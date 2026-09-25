package store

import (
	"context"
	"errors"
	"testing"
)

func TestWavesSlotsAssignments(t *testing.T) {
	ctx := context.Background()
	s, _ := openTemp(t)
	ev, _ := s.EnsureDefaultEvent(ctx, "E")
	if ev.ScheduleThreshold != 2 {
		t.Fatalf("default threshold %d", ev.ScheduleThreshold)
	}
	u := seedUser(t, s, ev.ID, "Ada")
	for _, id := range []string{"n1", "n2"} {
		s.InsertNote(ctx, Note{ID: id, EventID: ev.ID, AuthorID: u.ID, Title: id, Color: "yellow", CreatedAt: "t", UpdatedAt: "t"})
	}

	w := Wave{ID: "w", EventID: ev.ID, Name: "Morning", Status: "planned", Tracks: 6}
	if err := s.InsertWave(ctx, w); err != nil {
		t.Fatal(err)
	}
	s.InsertSlot(ctx, Slot{ID: "s2", WaveID: "w", StartAt: "2026-10-01T11:00:00.000Z", EndAt: "2026-10-01T11:45:00.000Z"})
	s.InsertSlot(ctx, Slot{ID: "s1", WaveID: "w", StartAt: "2026-10-01T10:00:00.000Z", EndAt: "2026-10-01T10:45:00.000Z"})
	got, err := s.WaveByID(ctx, "w")
	if err != nil || got.Tracks != 6 || len(got.Slots) != 2 || got.Slots[0].ID != "s1" {
		t.Fatalf("WaveByID (slots by start): %+v %v", got, err)
	}
	got.Name, got.Tracks, got.Status = "AM", 4, "open"
	if err := s.UpdateWave(ctx, got); err != nil {
		t.Fatal(err)
	}
	if ws, _ := s.Waves(ctx, ev.ID); len(ws) != 1 || ws[0].Name != "AM" || ws[0].Tracks != 4 || ws[0].Status != "open" || len(ws[0].Slots) != 2 {
		t.Fatalf("Waves: %+v", ws)
	}

	if err := s.InsertAssignment(ctx, Assignment{ID: "a1", NoteID: "n1", SlotID: "s1", Track: 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertAssignment(ctx, Assignment{ID: "a2", NoteID: "n2", SlotID: "s1", Track: 1}); !errors.Is(err, ErrConflict) {
		t.Fatalf("occupied cell: want ErrConflict, got %v", err)
	}
	if err := s.SetAssignmentMeetURL(ctx, "a1", "https://meet.example/a1"); err != nil {
		t.Fatal(err)
	}
	if a, _ := s.AssignmentByID(ctx, "a1"); a.MeetURL != "https://meet.example/a1" || a.Track != 1 {
		t.Fatalf("assignment: %+v", a)
	}
	if as, _ := s.WaveAssignments(ctx, "w"); len(as) != 1 {
		t.Fatalf("WaveAssignments: %+v", as)
	}
	// Deleting a slot cascades to its assignments.
	if err := s.DeleteSlot(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	if as, _ := s.Assignments(ctx, ev.ID); len(as) != 0 {
		t.Fatalf("cascade: %+v", as)
	}
	if err := s.DeleteWave(ctx, "w"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WaveByID(ctx, "w"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err := s.SetScheduleThreshold(ctx, ev.ID, 3); err != nil {
		t.Fatal(err)
	}
	if e, _ := s.Event(ctx, ev.ID); e.ScheduleThreshold != 3 {
		t.Fatal("threshold not saved")
	}
}

// TestVotesCastRefunds checks SPEC §4.1: votes on hidden notes and on notes
// scheduled in a locked or done wave stop counting against the budget.
func TestVotesCastRefunds(t *testing.T) {
	ctx := context.Background()
	s, _ := openTemp(t)
	ev, _ := s.EnsureDefaultEvent(ctx, "E")
	u := seedUser(t, s, ev.ID, "Ada")
	for _, id := range []string{"plain", "hidden", "draft", "locked", "done"} {
		s.InsertNote(ctx, Note{ID: id, EventID: ev.ID, AuthorID: u.ID, Title: id, Color: "yellow", CreatedAt: "t", UpdatedAt: "t"})
		s.CastVote(ctx, "v-"+id, u.ID, id, "t")
	}
	used := func() int {
		n, err := s.VotesCast(ctx, u.ID)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if used() != 5 {
		t.Fatalf("all votes count at first: %d", used())
	}
	s.HideNote(ctx, "hidden", u.ID, "t")
	for _, w := range []struct{ wave, status, note string }{{"wo", "open", "draft"}, {"wl", "locked", "locked"}, {"wd", "done", "done"}} {
		s.InsertWave(ctx, Wave{ID: w.wave, EventID: ev.ID, Name: w.wave, Status: w.status, Tracks: 1})
		s.InsertSlot(ctx, Slot{ID: "s-" + w.wave, WaveID: w.wave, StartAt: "a", EndAt: "b"})
		s.InsertAssignment(ctx, Assignment{ID: "a-" + w.wave, NoteID: w.note, SlotID: "s-" + w.wave, Track: 1})
	}
	// Counting: plain and the open wave's draft. Refunded: hidden, locked, done.
	if used() != 2 {
		t.Fatalf("used = %d, want 2", used())
	}
	for note, want := range map[string]bool{"plain": false, "draft": false, "locked": true, "done": true} {
		if got, _ := s.ScheduledInClosedWave(ctx, note); got != want {
			t.Errorf("ScheduledInClosedWave(%s) = %v", note, got)
		}
	}
	if voters, _ := s.Voters(ctx, "plain"); len(voters) != 1 || voters[0] != u.ID {
		t.Fatalf("Voters: %v", voters)
	}
	if m, _ := s.EventVoters(ctx, ev.ID); len(m) != 5 {
		t.Fatalf("EventVoters: %v", m)
	}
}
