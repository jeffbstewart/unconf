package server

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jeffbstewart/unconf/internal/domain"
)

// schedFixture: an active event with an organizer, a moderator, a
// participant, and n notes.
type schedFixture struct {
	s          *Server
	ts         *httptest.Server
	org, mod   *wsClient
	p          *wsClient
	notes      []string
	modID, pID string
}

func newSchedFixture(t *testing.T, n int) *schedFixture {
	t.Helper()
	s, ts := setup(t)
	f := &schedFixture{s: s, ts: ts}
	f.org = startEvent(t, ts)
	modClient := loginWS(t, ts, "Mo", false)
	pClient := loginWS(t, ts, "Pat", false)
	f.org.expectEvent("user_joined")
	f.org.expectEvent("user_joined")
	ctx := context.Background()
	mo, _ := s.cfg.Store.UserByName(ctx, s.cfg.EventID, "Mo")
	pat, _ := s.cfg.Store.UserByName(ctx, s.cfg.EventID, "Pat")
	f.modID, f.pID = mo.ID, pat.ID
	// Promote Mo through the real command.
	f.org.do("set_role", map[string]any{"userId": f.modID, "role": "moderator"}, "role_set")
	f.mod, _ = connectFresh(t, ts, modClient)
	f.p, _ = connectFresh(t, ts, pClient)
	for i := 0; i < n; i++ {
		ev := f.org.do("create_note", map[string]any{"title": fmt.Sprintf("S%d", i), "x": float64(i * 400), "y": 0}, "note_created")
		f.notes = append(f.notes, ev["note"].(map[string]any)["id"].(string))
		f.mod.expectEvent("note_created")
		f.p.expectEvent("note_created")
	}
	return f
}

// wave creates a wave with the given slots (as hour offsets from now) and
// returns its id and slot ids.
func (f *schedFixture) wave(t *testing.T, w *wsClient, tracks int, hours ...int) (string, []string) {
	t.Helper()
	ev := w.do("create_wave", map[string]any{"name": "Morning", "tracks": tracks}, "wave_created")["wave"].(map[string]any)
	id := ev["id"].(string)
	base := time.Now().UTC().Truncate(time.Hour)
	var slots []string
	for _, h := range hours {
		start := base.Add(time.Duration(h) * time.Hour)
		sl := w.do("create_slot", map[string]any{"waveId": id,
			"startAt": start.Format(time.RFC3339), "endAt": start.Add(45 * time.Minute).Format(time.RFC3339)}, "slot_created")
		slots = append(slots, sl["slot"].(map[string]any)["id"].(string))
	}
	return id, slots
}

func TestSchedulingPermissions(t *testing.T) {
	f := newSchedFixture(t, 1)
	f.p.fail("create_wave", map[string]any{"name": "Mine", "tracks": 2}, "forbidden")
	// Moderators are schedulers (SPEC §3).
	waveID, slots := f.wave(t, f.mod, 2, 1)
	f.mod.do("set_wave_status", map[string]any{"waveId": waveID, "status": "open"}, "wave_status_set")
	f.p.sync()
	f.p.fail("assign_note", map[string]any{"noteId": f.notes[0], "slotId": slots[0], "track": 1}, "forbidden")
	f.mod.do("assign_note", map[string]any{"noteId": f.notes[0], "slotId": slots[0], "track": 1}, "note_assigned")
	// Roles: only organizers set them, and only participant↔moderator.
	f.mod.fail("set_role", map[string]any{"userId": f.pID, "role": "moderator"}, "forbidden")
	f.org.sync()
	f.org.fail("set_role", map[string]any{"userId": f.pID, "role": "organizer"}, "bad_request")
}

func TestWaveLifecycleAndOneOpenRule(t *testing.T) {
	f := newSchedFixture(t, 0)
	a, _ := f.wave(t, f.org, 4, 1)
	b, _ := f.wave(t, f.org, 4, 3)
	f.org.fail("set_wave_status", map[string]any{"waveId": a, "status": "locked"}, "not_allowed_now") // no skipping
	f.org.do("set_wave_status", map[string]any{"waveId": a, "status": "open"}, "wave_status_set")
	f.org.fail("set_wave_status", map[string]any{"waveId": b, "status": "open"}, "not_allowed_now") // one open
	f.org.fail("delete_wave", map[string]any{"waveId": a}, "not_allowed_now")                       // only planned
	f.org.do("set_wave_status", map[string]any{"waveId": a, "status": "locked"}, "wave_status_set")
	f.org.do("set_wave_status", map[string]any{"waveId": b, "status": "open"}, "wave_status_set")
	f.org.fail("set_wave_status", map[string]any{"waveId": a, "status": "open"}, "not_allowed_now") // no going back
	f.org.do("set_wave_status", map[string]any{"waveId": a, "status": "done"}, "wave_status_set")
	c, _ := f.wave(t, f.org, 1)
	f.org.do("delete_wave", map[string]any{"waveId": c}, "wave_deleted")
}

func TestSlotsAndTracks(t *testing.T) {
	f := newSchedFixture(t, 1)
	waveID, slots := f.wave(t, f.org, 2, 1, 2)
	base := time.Now().UTC().Truncate(time.Hour)
	overlap := map[string]any{"waveId": waveID,
		"startAt": base.Add(90 * time.Minute).Format(time.RFC3339), "endAt": base.Add(150 * time.Minute).Format(time.RFC3339)}
	if msg := f.org.fail("create_slot", overlap, "bad_request"); !strings.Contains(msg, "overlaps") {
		t.Fatalf("overlap message: %q", msg)
	}
	f.org.fail("create_slot", map[string]any{"waveId": waveID, "startAt": "soon", "endAt": "later"}, "bad_request")
	f.org.do("set_wave_status", map[string]any{"waveId": waveID, "status": "open"}, "wave_status_set")
	f.org.fail("assign_note", map[string]any{"noteId": f.notes[0], "slotId": slots[0], "track": 3}, "bad_request")
	f.org.do("assign_note", map[string]any{"noteId": f.notes[0], "slotId": slots[0], "track": 2}, "note_assigned")
	// Can't shrink below a track in use, or delete a non-empty slot.
	f.org.fail("update_wave", map[string]any{"waveId": waveID, "tracks": 1}, "not_allowed_now")
	f.org.fail("delete_slot", map[string]any{"slotId": slots[0]}, "not_allowed_now")
	w := f.org.do("update_wave", map[string]any{"waveId": waveID, "tracks": 6, "name": "AM"}, "wave_updated")["wave"].(map[string]any)
	if w["tracks"] != 6.0 || w["name"] != "AM" || len(w["slots"].([]any)) != 2 {
		t.Fatalf("wave_updated: %v", w)
	}
	f.org.do("delete_slot", map[string]any{"slotId": slots[1]}, "slot_deleted")
}

func TestAssignReplaceUnassignClear(t *testing.T) {
	f := newSchedFixture(t, 2)
	waveID, slots := f.wave(t, f.mod, 2, 1, 2)
	f.org.sync()
	f.org.fail("assign_note", map[string]any{"noteId": f.notes[0], "slotId": slots[0], "track": 1}, "not_allowed_now") // planned
	f.mod.do("set_wave_status", map[string]any{"waveId": waveID, "status": "open"}, "wave_status_set")

	a := f.mod.do("assign_note", map[string]any{"noteId": f.notes[0], "slotId": slots[0], "track": 1}, "note_assigned")["assignment"].(map[string]any)
	if a["track"] != 1.0 || a["slotId"] != slots[0] || a["meetUrl"] != nil {
		t.Fatalf("assignment: %v", a)
	}
	f.mod.fail("assign_note", map[string]any{"noteId": f.notes[1], "slotId": slots[0], "track": 1}, "not_allowed_now") // taken

	// Moving a note within the wave replaces its assignment: unassigned, then assigned.
	id := f.mod.send("assign_note", map[string]any{"noteId": f.notes[0], "slotId": slots[1], "track": 2})
	if ev := f.mod.expectEvent("note_unassigned").event(); ev["assignmentId"] != a["id"] {
		t.Fatalf("replace: %v", ev)
	}
	moved := f.mod.expectEvent("note_assigned").event()["assignment"].(map[string]any)
	if f.mod.expect("ack").str("cmdId") != id || moved["slotId"] != slots[1] {
		t.Fatalf("moved: %v", moved)
	}
	// The participant watches the draft live.
	for _, k := range []string{"wave_created", "slot_created", "slot_created", "wave_status_set", "note_assigned", "note_unassigned", "note_assigned"} {
		f.p.expectEvent(k)
	}

	f.mod.do("unassign_note", map[string]any{"assignmentId": moved["id"]}, "note_unassigned")
	f.mod.do("assign_note", map[string]any{"noteId": f.notes[0], "slotId": slots[0], "track": 1}, "note_assigned")
	f.mod.do("assign_note", map[string]any{"noteId": f.notes[1], "slotId": slots[0], "track": 2}, "note_assigned")
	f.mod.send("clear_wave", map[string]any{"waveId": waveID})
	f.mod.expectEvent("note_unassigned")
	f.mod.expectEvent("note_unassigned")
	f.mod.expect("ack")
}

func TestLockCreatesMeetingsAndRefundsVotes(t *testing.T) {
	f := newSchedFixture(t, 3)
	f.org.do("set_voting", map[string]any{"open": true}, "voting_set")
	f.p.sync()
	for _, n := range f.notes {
		f.p.do("cast_vote", map[string]any{"noteId": n}, "vote_cast")
	}
	f.mod.sync()
	waveID, slots := f.wave(t, f.mod, 2, 1)
	f.mod.do("set_wave_status", map[string]any{"waveId": waveID, "status": "open"}, "wave_status_set")
	f.mod.do("assign_note", map[string]any{"noteId": f.notes[0], "slotId": slots[0], "track": 1}, "note_assigned")
	f.mod.do("assign_note", map[string]any{"noteId": f.notes[1], "slotId": slots[0], "track": 2}, "note_assigned")

	f.p.sync()
	f.org.sync()

	f.mod.send("set_wave_status", map[string]any{"waveId": waveID, "status": "locked"})
	f.mod.expectEvent("wave_status_set")
	f.mod.expect("ack") // the moderator cast no votes: no refund for them

	// The voter hears the lock, then their private refund: 3 votes, 2 now history.
	f.p.expectEvent("wave_status_set")
	if ev := f.p.expectEvent("votes_used_set").event(); ev["votesUsed"] != 1.0 {
		t.Fatalf("votes_used_set: %v", ev)
	}
	// Then the calendar stub's links arrive as a follow-up event, for everyone.
	links := f.p.expectEvent("assignment_links_set").event()["links"].(map[string]any)
	if len(links) != 2 {
		t.Fatalf("links: %v", links)
	}
	for id, url := range links {
		if url != "https://meet.example/"+id {
			t.Fatalf("link %s = %v", id, url)
		}
	}
	f.mod.expectEvent("assignment_links_set")
	// The organizer (no votes) gets the public events but no refund.
	f.org.expectEvent("wave_status_set")
	f.org.expectEvent("assignment_links_set")
	f.org.quiet()

	// Locked sessions' votes are history.
	f.p.fail("retract_vote", map[string]any{"noteId": f.notes[0]}, "not_allowed_now")
	f.p.fail("cast_vote", map[string]any{"noteId": f.notes[1]}, "not_allowed_now")
	// The wave is frozen.
	f.mod.fail("assign_note", map[string]any{"noteId": f.notes[2], "slotId": slots[0], "track": 1}, "not_allowed_now")

	// A fresh snapshot agrees: budget refunded, meet links stored.
	_, st := connectFresh(t, f.ts, loginWS(t, f.ts, "Pat", false))
	if me := st["me"].(map[string]any); me["votesUsed"] != 1.0 {
		t.Fatalf("snapshot me: %v", me)
	}
	for _, a := range st["assignments"].([]any) {
		if u, _ := a.(map[string]any)["meetUrl"].(string); !strings.HasPrefix(u, "https://meet.example/") {
			t.Fatalf("assignment without link: %v", a)
		}
	}
	notes := st["notes"].([]any)
	if v := notes[0].(map[string]any)["voters"].([]any); len(v) != 1 || v[0] != f.pID {
		t.Fatalf("voters: %v", notes[0])
	}
	w := st["waves"].([]any)[0].(map[string]any)
	if w["status"] != string(domain.WaveLocked) || w["tracks"] != 2.0 {
		t.Fatalf("wave: %v", w)
	}
}
