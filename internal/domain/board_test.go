package domain

import (
	"math"
	"strings"
	"testing"
)

func TestIntersects(t *testing.T) {
	a := NoteRect(0, 0)
	cases := []struct {
		b    Rect
		want bool
	}{
		{NoteRect(0, 0), true},
		{NoteRect(179, 119), true},
		{NoteRect(180, 0), false}, // shares an edge only
		{NoteRect(0, 120), false},
		{NoteRect(-180, -120), false},
		{NoteRect(-179, 0), true},
	}
	for _, c := range cases {
		if got := a.Intersects(c.b); got != c.want || c.b.Intersects(a) != c.want {
			t.Errorf("%v ∩ %v = %v, want %v", a, c.b, got, c.want)
		}
	}
}

func TestResolveFreeSpotUnchanged(t *testing.T) {
	x, y := ResolvePosition(1000.5, -3, []Rect{NoteRect(0, 0)})
	if x != 1000.5 || y != -3 {
		t.Fatalf("free position moved to %v,%v", x, y)
	}
}

func TestResolveNudgesToNearestFree(t *testing.T) {
	others := []Rect{NoteRect(0, 0)}
	x, y := ResolvePosition(0, 0, others)
	if NoteRect(x, y).Intersects(others[0]) {
		t.Fatalf("resolved %v,%v still overlaps", x, y)
	}
	// Directly on top of a note: the nearest free spots are 120 above/below
	// (6 steps) — closer than 180 left/right (9 steps).
	if x != 0 || math.Abs(y) != 120 {
		t.Fatalf("got %v,%v, want 0,±120", x, y)
	}
}

func TestResolveIsDeterministicAndOnGrid(t *testing.T) {
	others := []Rect{NoteRect(0, 0), NoteRect(0, 120), NoteRect(0, -120), NoteRect(180, 0)}
	x1, y1 := ResolvePosition(20, 20, others)
	for i := 0; i < 10; i++ {
		x, y := ResolvePosition(20, 20, others)
		if x != x1 || y != y1 {
			t.Fatal("not deterministic")
		}
	}
	for _, o := range others {
		if NoteRect(x1, y1).Intersects(o) {
			t.Fatalf("overlaps %v", o)
		}
	}
	// Starting on the 20-unit grid keeps the result on it.
	if math.Mod(x1, 20) != 0 || math.Mod(y1, 20) != 0 {
		t.Fatalf("off grid: %v,%v", x1, y1)
	}
}

func TestResolveDenseCluster(t *testing.T) {
	var others []Rect
	for i := 0; i < 20; i++ {
		for j := 0; j < 20; j++ {
			others = append(others, NoteRect(float64(i)*180, float64(j)*120))
		}
	}
	x, y := ResolvePosition(1800, 1200, others) // middle of a solid 20×20 block
	for _, o := range others {
		if NoteRect(x, y).Intersects(o) {
			t.Fatalf("resolved %v,%v overlaps %v", x, y, o)
		}
	}
}

func TestSpiralRingOrder(t *testing.T) {
	r1 := ring(1)
	if len(r1) != 8 || r1[0] != (offset{1, 0}) {
		t.Fatalf("ring 1 starts %v (len %d), want east first", r1[0], len(r1))
	}
	for k := 1; k < 5; k++ {
		if got := len(ring(k)); got != 8*k {
			t.Errorf("ring %d has %d offsets", k, got)
		}
	}
	if len(ring(100)) != 800 { // beyond the precomputed cache
		t.Error("uncached ring wrong size")
	}
}

func TestValidCoord(t *testing.T) {
	for _, v := range []float64{0, -5.5, MaxCoord, -MaxCoord} {
		if !ValidCoord(v) {
			t.Errorf("%v should be valid", v)
		}
	}
	for _, v := range []float64{math.NaN(), math.Inf(1), MaxCoord + 1} {
		if ValidCoord(v) {
			t.Errorf("%v should be invalid", v)
		}
	}
}

func TestNoteValidation(t *testing.T) {
	if got, err := NormalizeTitle("  Hello  "); err != nil || got != "Hello" {
		t.Fatalf("%q %v", got, err)
	}
	for _, bad := range []string{"", "  ", strings.Repeat("x", 121)} {
		if _, err := NormalizeTitle(bad); err == nil {
			t.Errorf("title %q accepted", bad)
		}
	}
	if err := ValidateBody(strings.Repeat("é", MaxBodyLen)); err != nil {
		t.Error(err)
	}
	if err := ValidateBody(strings.Repeat("x", MaxBodyLen+1)); err == nil {
		t.Error("long body accepted")
	}
	if c, _ := NormalizeColor(""); c != "yellow" {
		t.Error("default color")
	}
	if _, err := NormalizeColor("red"); err == nil {
		t.Error("unknown color accepted")
	}
}

func TestNormalizeLinks(t *testing.T) {
	got, err := NormalizeLinks([]Link{
		{Title: " Deck ", URL: " https://example.com/s ", Kind: "slides"},
		{Title: "Notes", URL: "http://example.com/d"},
	})
	if err != nil || len(got) != 2 || got[0].Title != "Deck" || got[0].URL != "https://example.com/s" || got[1].Kind != "other" {
		t.Fatalf("%+v %v", got, err)
	}
	bad := [][]Link{
		{{Title: "x", URL: "javascript:alert(1)"}},
		{{Title: "x", URL: "ftp://example.com"}},
		{{Title: "x", URL: "/relative"}},
		{{Title: "x", URL: "https://"}},
		{{Title: "", URL: "https://example.com"}},
		{{Title: "x", URL: "https://example.com", Kind: "video"}},
		make([]Link, MaxLinks+1),
	}
	for _, links := range bad {
		if _, err := NormalizeLinks(links); err == nil {
			t.Errorf("accepted %+v", links)
		}
	}
}

// TestPermissionMatrix checks every M3 note command for every role, on the
// actor's own note and on someone else's, in each lifecycle.
func TestPermissionMatrix(t *testing.T) {
	const self, other = "u-self", "u-other"
	type check func(Actor, Lifecycle, NoteFacts) *CmdError
	commands := map[string]check{
		"create_note": func(a Actor, lc Lifecycle, _ NoteFacts) *CmdError { return AuthorizeCreateNote(a, lc) },
		"update_note": AuthorizeEditNote,
		"set_links":   AuthorizeEditNote,
		"move_note":   AuthorizeMoveNote,
		"delete_note": AuthorizeDeleteNote,
	}
	// want[cmd][role][own?] while the event is active. "" = allowed.
	want := map[string]map[Role][2]ErrCode{
		"create_note": {RoleParticipant: {"", ""}, RoleModerator: {"", ""}, RoleOrganizer: {"", ""}},
		"update_note": {RoleParticipant: {CodeForbidden, ""}, RoleModerator: {"", ""}, RoleOrganizer: {"", ""}},
		"set_links":   {RoleParticipant: {CodeForbidden, ""}, RoleModerator: {"", ""}, RoleOrganizer: {"", ""}},
		"move_note":   {RoleParticipant: {"", ""}, RoleModerator: {"", ""}, RoleOrganizer: {"", ""}},
		"delete_note": {RoleParticipant: {CodeForbidden, ""}, RoleModerator: {"", ""}, RoleOrganizer: {"", ""}},
	}
	code := func(e *CmdError) ErrCode {
		if e == nil {
			return ""
		}
		return e.Code
	}
	for name, fn := range commands {
		for _, role := range []Role{RoleParticipant, RoleModerator, RoleOrganizer} {
			a := Actor{UserID: self, Role: role}
			for i, author := range []string{other, self} {
				n := NoteFacts{AuthorID: author}
				if got := code(fn(a, LifecycleActive, n)); got != want[name][role][i] {
					t.Errorf("active %s by %s (own=%v): %q, want %q", name, role, i == 1, got, want[name][role][i])
				}
				// setup: participants locked out, moderators+ as in active.
				wantSetup := want[name][role][i]
				if role == RoleParticipant {
					wantSetup = CodeNotAllowedNow
				}
				if got := code(fn(a, LifecycleSetup, n)); got != wantSetup {
					t.Errorf("setup %s by %s (own=%v): %q, want %q", name, role, i == 1, got, wantSetup)
				}
				// done: read-only for everyone.
				if got := code(fn(a, LifecycleDone, n)); got != CodeNotAllowedNow {
					t.Errorf("done %s by %s: %q, want not_allowed_now", name, role, got)
				}
			}
		}
	}
}

func TestDeleteWithVotesOrAssignments(t *testing.T) {
	p := Actor{UserID: "me", Role: RoleParticipant}
	m := Actor{UserID: "mod", Role: RoleModerator}
	for _, n := range []NoteFacts{{AuthorID: "me", Votes: 1}, {AuthorID: "me", Assignments: 1}} {
		if err := AuthorizeDeleteNote(p, LifecycleActive, n); err == nil || err.Code != CodeNotAllowedNow {
			t.Errorf("author delete of %+v: %v", n, err)
		}
		if err := AuthorizeDeleteNote(m, LifecycleActive, n); err != nil {
			t.Errorf("moderator delete of %+v: %v", n, err)
		}
	}
}

func TestMoveHiddenNote(t *testing.T) {
	m := Actor{UserID: "mod", Role: RoleModerator}
	if err := AuthorizeMoveNote(m, LifecycleActive, NoteFacts{Hidden: true}); err == nil || err.Code != CodeNotAllowedNow {
		t.Fatalf("got %v", err)
	}
	p := Actor{UserID: "p", Role: RoleParticipant}
	if p.CanSee(NoteFacts{Hidden: true}) || !m.CanSee(NoteFacts{Hidden: true}) {
		t.Fatal("hidden visibility")
	}
}

func TestSetLifecycle(t *testing.T) {
	org := Actor{Role: RoleOrganizer}
	cases := []struct {
		a         Actor
		cur, next Lifecycle
		want      ErrCode
	}{
		{org, LifecycleSetup, LifecycleActive, ""},
		{org, LifecycleActive, LifecycleDone, ""},
		{org, LifecycleSetup, LifecycleDone, ""},
		{org, LifecycleActive, LifecycleSetup, CodeNotAllowedNow},
		{org, LifecycleActive, LifecycleActive, CodeNotAllowedNow},
		{org, LifecycleDone, LifecycleDone, CodeNotAllowedNow},
		{org, LifecycleSetup, "paused", CodeBadRequest},
		{Actor{Role: RoleModerator}, LifecycleSetup, LifecycleActive, CodeForbidden},
		{Actor{Role: RoleParticipant}, LifecycleSetup, LifecycleActive, CodeForbidden},
	}
	for _, c := range cases {
		err := AuthorizeSetLifecycle(c.a, c.cur, c.next)
		got := ErrCode("")
		if err != nil {
			got = err.Code
		}
		if got != c.want {
			t.Errorf("%s: %s→%s = %q, want %q", c.a.Role, c.cur, c.next, got, c.want)
		}
	}
}
