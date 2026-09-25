package store

import (
	"context"
	"errors"
	"io/fs"
	"testing"
	"time"

	"github.com/jeffbstewart/unconf/internal/domain"
	"github.com/jeffbstewart/unconf/internal/store/schema"
)

func seedUser(t *testing.T, s *Store, eventID, name string) User {
	t.Helper()
	u := User{ID: domain.NewID(), EventID: eventID, Name: name, Role: domain.RoleParticipant, CreatedAt: domain.Timestamp(time.Now())}
	if err := s.CreateUser(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	return u
}

func TestNoteCRUD(t *testing.T) {
	ctx := context.Background()
	s, _ := openTemp(t)
	ev, _ := s.EnsureDefaultEvent(ctx, "E")
	u := seedUser(t, s, ev.ID, "Ada")
	now := domain.Timestamp(time.Now())
	n := Note{ID: domain.NewID(), EventID: ev.ID, AuthorID: u.ID, Title: "T", X: 1.5, Y: -2, Color: "blue", CreatedAt: now, UpdatedAt: now}
	if err := s.InsertNote(ctx, n); err != nil {
		t.Fatal(err)
	}
	got, err := s.NoteByID(ctx, n.ID)
	if err != nil || got != n {
		t.Fatalf("NoteByID: %+v, %v", got, err)
	}

	if err := s.UpdateNoteContent(ctx, n.ID, "T2", "body", "pink", "later"); err != nil {
		t.Fatal(err)
	}
	if err := s.MoveNote(ctx, n.ID, 100, 200); err != nil {
		t.Fatal(err)
	}
	got, _ = s.NoteByID(ctx, n.ID)
	if got.Title != "T2" || got.BodyMD != "body" || got.Color != "pink" || got.UpdatedAt != "later" || got.X != 100 || got.Y != 200 {
		t.Fatalf("after update/move: %+v", got)
	}

	links := []domain.Link{{ID: "l1", Title: "A", URL: "https://a", Kind: "doc"}, {ID: "l2", Title: "B", URL: "https://b", Kind: "other"}}
	if err := s.ReplaceLinks(ctx, n.ID, links); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceLinks(ctx, n.ID, links[1:]); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.NoteLinks(ctx, n.ID); len(got) != 1 || got[0].ID != "l2" {
		t.Fatalf("links after replace: %+v", got)
	}
	all, _ := s.EventLinks(ctx, ev.ID)
	if len(all[n.ID]) != 1 {
		t.Fatalf("EventLinks: %+v", all)
	}

	// Votes and stars feed facts and snapshot aggregates.
	u2 := seedUser(t, s, ev.ID, "Bob")
	if _, err := s.db.ExecContext(ctx, "INSERT INTO votes (id, user_id, note_id, cast_at) VALUES ('v1', ?, ?, ?), ('v2', ?, ?, ?)", u.ID, n.ID, now, u2.ID, n.ID, now); err != nil {
		t.Fatal(err)
	}
	s.db.ExecContext(ctx, "INSERT INTO stars (user_id, note_id) VALUES (?, ?)", u.ID, n.ID)
	if f, _ := s.NoteFacts(ctx, got); f.Votes != 2 || f.Assignments != 0 || f.AuthorID != u.ID || f.Hidden {
		t.Fatalf("facts: %+v", f)
	}
	if tot, _ := s.VoteTotals(ctx, ev.ID); tot[n.ID] != 2 {
		t.Fatalf("totals: %v", tot)
	}
	if mine, _ := s.UserVoted(ctx, u.ID); !mine[n.ID] {
		t.Fatalf("user voted: %v", mine)
	}
	if stars, _ := s.UserStars(ctx, u.ID); !stars[n.ID] {
		t.Fatalf("stars: %v", stars)
	}

	if err := s.HideNote(ctx, n.ID, u.ID, now); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.NoteByID(ctx, n.ID); !got.Hidden() || got.HiddenBy != u.ID {
		t.Fatalf("hide: %+v", got)
	}

	// Deleting cascades to links, votes, and stars.
	if err := s.DeleteNote(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.NoteByID(ctx, n.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
	for _, table := range []string{"note_links", "votes", "stars"} {
		var c int
		s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&c)
		if c != 0 {
			t.Errorf("%s not cascaded: %d rows", table, c)
		}
	}
	if err := s.DeleteNote(ctx, n.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}

func TestSeqAndLifecycle(t *testing.T) {
	ctx := context.Background()
	s, _ := openTemp(t)
	if seq, err := s.Seq(ctx); err != nil || seq != 0 {
		t.Fatalf("initial seq %d %v", seq, err)
	}
	if err := s.InTx(ctx, func(tx Tx) error { return tx.SetSeq(ctx, 42) }); err != nil {
		t.Fatal(err)
	}
	if seq, _ := s.Seq(ctx); seq != 42 {
		t.Fatalf("seq %d", seq)
	}
	// A failed transaction leaves nothing behind.
	boom := errors.New("boom")
	if err := s.InTx(ctx, func(tx Tx) error { tx.SetSeq(ctx, 99); return boom }); !errors.Is(err, boom) {
		t.Fatal(err)
	}
	if seq, _ := s.Seq(ctx); seq != 42 {
		t.Fatalf("rolled-back seq leaked: %d", seq)
	}

	ev, _ := s.EnsureDefaultEvent(ctx, "E")
	if err := s.SetLifecycle(ctx, ev.ID, domain.LifecycleActive); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Event(ctx, ev.ID); got.Lifecycle != "active" {
		t.Fatalf("lifecycle %q", got.Lifecycle)
	}
}

func TestSnapshotQueriesOnEmptyEvent(t *testing.T) {
	ctx := context.Background()
	s, _ := openTemp(t)
	ev, _ := s.EnsureDefaultEvent(ctx, "E")
	if _, err := s.Regions(ctx, ev.ID); err != nil {
		t.Error(err)
	}
	if w, err := s.Waves(ctx, ev.ID); err != nil || len(w) != 0 {
		t.Error(w, err)
	}
	if _, err := s.Assignments(ctx, ev.ID); err != nil {
		t.Error(err)
	}
	if _, err := s.Messages(ctx, ev.ID); err != nil {
		t.Error(err)
	}
	if u, err := s.Users(ctx, ev.ID); err != nil || len(u) != 0 {
		t.Error(u, err)
	}
}

func TestRegionsAndStars(t *testing.T) {
	ctx := context.Background()
	s, _ := openTemp(t)
	ev, _ := s.EnsureDefaultEvent(ctx, "E")
	a, err := s.InsertRegion(ctx, Region{ID: "ra", EventID: ev.ID, Label: "A", X: 0, Y: 0, W: 400, H: 300, Color: "#aabbcc"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := s.InsertRegion(ctx, Region{ID: "rb", EventID: ev.ID, Label: "B", X: 0, Y: 0, W: 400, H: 300, Color: "#aabbcc", Z: 2})
	if !(b.Order > a.Order) {
		t.Fatalf("creation order not increasing: %d, %d", a.Order, b.Order)
	}
	a.Label, a.W = "A2", 500
	if err := s.UpdateRegion(ctx, a); err != nil {
		t.Fatal(err)
	}
	got, _ := s.RegionByID(ctx, "ra")
	if got != a {
		t.Fatalf("round trip: %+v vs %+v", got, a)
	}
	if all, _ := s.Regions(ctx, ev.ID); len(all) != 2 || all[0].ID != "ra" {
		t.Fatalf("Regions: %+v", all)
	}

	u := seedUser(t, s, ev.ID, "Ada")
	n := Note{ID: "n", EventID: ev.ID, AuthorID: u.ID, Title: "T", Color: "yellow", CreatedAt: "t", UpdatedAt: "t"}
	s.InsertNote(ctx, n)
	if err := s.SetNoteRegion(ctx, "n", "ra"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.NoteByID(ctx, "n"); got.RegionID != "ra" {
		t.Fatalf("tag: %q", got.RegionID)
	}
	// A tagged note blocks region deletion until retagged (foreign key).
	if err := s.DeleteRegion(ctx, "ra"); err == nil {
		t.Fatal("deleting a region still referenced by a note should fail")
	}
	s.SetNoteRegion(ctx, "n", "")
	if err := s.DeleteRegion(ctx, "ra"); err != nil {
		t.Fatal(err)
	}

	for i, want := range []bool{true, false} {
		if changed, err := s.StarNote(ctx, u.ID, "n"); err != nil || changed != want {
			t.Fatalf("star #%d: %v %v", i+1, changed, err)
		}
	}
	for i, want := range []bool{true, false} {
		if changed, err := s.UnstarNote(ctx, u.ID, "n"); err != nil || changed != want {
			t.Fatalf("unstar #%d: %v %v", i+1, changed, err)
		}
	}
}

func TestVotes(t *testing.T) {
	ctx := context.Background()
	s, _ := openTemp(t)
	ev, _ := s.EnsureDefaultEvent(ctx, "E")
	ada := seedUser(t, s, ev.ID, "Ada")
	bob := seedUser(t, s, ev.ID, "Bob")
	s.InsertNote(ctx, Note{ID: "n", EventID: ev.ID, AuthorID: ada.ID, Title: "T", Color: "yellow", CreatedAt: "t", UpdatedAt: "t"})

	if cast, err := s.CastVote(ctx, "v1", ada.ID, "n", "t"); !cast || err != nil {
		t.Fatal(cast, err)
	}
	// One vote per person per note: a second one changes nothing.
	if cast, err := s.CastVote(ctx, "v2", ada.ID, "n", "t"); cast || err != nil {
		t.Fatalf("second vote by the same user: cast=%v err=%v", cast, err)
	}
	s.CastVote(ctx, "v3", bob.ID, "n", "t")
	if n, _ := s.NoteVoteTotal(ctx, "n"); n != 2 {
		t.Fatalf("total %d, want 2 voters", n)
	}
	if voted, _ := s.HasVoted(ctx, ada.ID, "n"); !voted {
		t.Fatal("HasVoted")
	}
	if m, _ := s.UserVoted(ctx, ada.ID); !m["n"] {
		t.Fatal("UserVoted")
	}
	if used, _ := s.VotesCast(ctx, ada.ID); used != 1 {
		t.Fatalf("used %d", used)
	}
	if ok, err := s.RetractVote(ctx, ada.ID, "n"); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if ok, _ := s.RetractVote(ctx, ada.ID, "n"); ok {
		t.Fatal("retracting twice should report false")
	}
	if n, _ := s.NoteVoteTotal(ctx, "n"); n != 1 {
		t.Fatalf("total %d after retract", n)
	}

	if err := s.SetVotingOpen(ctx, ev.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetVotesPerUser(ctx, ev.ID, 9); err != nil {
		t.Fatal(err)
	}
	if e, _ := s.Event(ctx, ev.ID); !e.VotingOpen || e.VotesPerUser != 9 {
		t.Fatalf("event: %+v", e)
	}
}

func TestSchemaCollapsesStackedVotes(t *testing.T) {
	// Fragment 007 must dedupe votes left by earlier (stacking) builds.
	ctx := context.Background()
	s, _ := openTemp(t)
	if _, err := s.db.ExecContext(ctx, "DROP INDEX votes_user_note"); err != nil {
		t.Fatal(err)
	}
	ev, _ := s.EnsureDefaultEvent(ctx, "E")
	u := seedUser(t, s, ev.ID, "Ada")
	s.InsertNote(ctx, Note{ID: "n", EventID: ev.ID, AuthorID: u.ID, Title: "T", Color: "yellow", CreatedAt: "t", UpdatedAt: "t"})
	for _, id := range []string{"a", "b", "c"} {
		s.db.ExecContext(ctx, "INSERT INTO votes (id, user_id, note_id, cast_at) VALUES (?, ?, 'n', 't')", id, u.ID)
	}
	frag, err := fs.ReadFile(schema.Fragments, "007_one_vote_per_session.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, string(frag)); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.NoteVoteTotal(ctx, "n"); n != 1 {
		t.Fatalf("stacked votes not collapsed: %d", n)
	}
}
