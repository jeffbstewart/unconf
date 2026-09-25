package server

import (
	"context"
	"testing"
)

func TestRegionTagging(t *testing.T) {
	_, ts := setup(t)
	org := startEvent(t, ts)
	p, _ := connectFresh(t, ts, loginWS(t, ts, "Pat", false))
	org.expectEvent("user_joined")

	inside := p.do("create_note", map[string]any{"title": "early", "x": 50, "y": 50}, "note_created")["note"].(map[string]any)
	org.expectEvent("note_created")
	if inside["regionId"] != nil {
		t.Fatalf("no regions yet: %v", inside)
	}

	// Participants may not draw regions.
	p.fail("create_region", map[string]any{"label": "Mine", "x": 0, "y": 0, "w": 400, "h": 300, "color": "#ffeeaa"}, "forbidden")

	// Drawing a region over an existing note tags it, in every window.
	id := org.send("create_region", map[string]any{"label": "Track A", "x": 0, "y": 0, "w": 600, "h": 400, "color": "#ffeeaa"})
	created := org.expectEvent("region_created").event()["region"].(map[string]any)
	regionID := created["id"].(string)
	if created["label"] != "Track A" || created["w"] != 600.0 || created["z"] != 0.0 {
		t.Fatalf("region_created: %v", created)
	}
	retag := org.expectEvent("note_retagged").event()
	if retag["noteId"] != inside["id"] || retag["regionId"] != regionID {
		t.Fatalf("note_retagged: %v", retag)
	}
	if ack := org.expect("ack"); ack.str("cmdId") != id {
		t.Fatalf("ack: %v", ack)
	}
	p.expectEvent("region_created")
	p.expectEvent("note_retagged")

	// A new note created inside is tagged at birth (no separate event).
	born := p.do("create_note", map[string]any{"title": "inside", "x": 300, "y": 200}, "note_created")["note"].(map[string]any)
	org.expectEvent("note_created")
	if born["regionId"] != regionID {
		t.Fatalf("born untagged: %v", born)
	}

	// Dragging a note out, then back in, retags it.
	moved := p.send("move_note", map[string]any{"noteId": born["id"], "x": 2000, "y": 2000})
	p.expectEvent("note_moved")
	if ev := p.expectEvent("note_retagged").event(); ev["regionId"] != nil {
		t.Fatalf("moved out: %v", ev)
	}
	if p.expect("ack").str("cmdId") != moved {
		t.Fatal("ack after both events")
	}
	org.expectEvent("note_moved")
	org.expectEvent("note_retagged")
	p.send("move_note", map[string]any{"noteId": born["id"], "x": 300, "y": 200})
	p.expectEvent("note_moved")
	if ev := p.expectEvent("note_retagged").event(); ev["regionId"] != regionID {
		t.Fatalf("moved in: %v", ev)
	}
	p.expect("ack")
	org.expectEvent("note_moved")
	org.expectEvent("note_retagged")

	// A moving note that stays inside emits no retag.
	p.send("move_note", map[string]any{"noteId": born["id"], "x": 320, "y": 200})
	p.expectEvent("note_moved")
	p.expect("ack")
	org.expectEvent("note_moved")

	// An overlapping region with a higher z takes over the notes it covers.
	org.send("create_region", map[string]any{"label": "Hot", "x": 250, "y": 150, "w": 400, "h": 300, "color": "#ffcccc", "z": 5})
	hot := org.expectEvent("region_created").event()["region"].(map[string]any)["id"]
	if ev := org.expectEvent("note_retagged").event(); ev["noteId"] != born["id"] || ev["regionId"] != hot {
		t.Fatalf("z-order retag: %v", ev)
	}
	org.expect("ack")
	p.expectEvent("region_created")
	p.expectEvent("note_retagged")

	// Lowering its z hands the note back to Track A.
	org.send("update_region", map[string]any{"regionId": hot, "z": -1})
	if ev := org.expectEvent("region_updated").event()["region"].(map[string]any); ev["z"] != -1.0 || ev["label"] != "Hot" {
		t.Fatalf("region_updated: %v", ev)
	}
	if ev := org.expectEvent("note_retagged").event(); ev["regionId"] != regionID {
		t.Fatalf("after z change: %v", ev)
	}
	org.expect("ack")
	p.expectEvent("region_updated")
	p.expectEvent("note_retagged")

	// Deleting Track A retags its notes: "inside" falls through to Hot,
	// "early" to nothing. Events come in note-id order.
	org.send("delete_region", map[string]any{"regionId": regionID})
	if ev := org.expectEvent("region_deleted").event(); ev["regionId"] != regionID {
		t.Fatalf("region_deleted: %v", ev)
	}
	got := map[string]any{}
	for i := 0; i < 2; i++ {
		ev := org.expectEvent("note_retagged").event()
		got[ev["noteId"].(string)] = ev["regionId"]
	}
	org.expect("ack")
	if got[inside["id"].(string)] != nil || got[born["id"].(string)] != hot {
		t.Fatalf("retags after delete: %v", got)
	}

	// The snapshot agrees.
	_, state := connectFresh(t, ts, loginWS(t, ts, "Late", false))
	tags := map[string]any{}
	for _, n := range state["notes"].([]any) {
		n := n.(map[string]any)
		tags[n["id"].(string)] = n["regionId"]
	}
	if tags[inside["id"].(string)] != nil || tags[born["id"].(string)] != hot || len(state["regions"].([]any)) != 1 {
		t.Fatalf("snapshot tags %v regions %v", tags, state["regions"])
	}

	org.expectEvent("user_joined") // Late
	org.fail("update_region", map[string]any{"regionId": "nope", "label": "x"}, "bad_request")
	org.fail("create_region", map[string]any{"label": "Tiny", "x": 0, "y": 0, "w": 10, "h": 10, "color": "#ffffff"}, "bad_request")
	org.fail("create_region", map[string]any{"label": "Red", "x": 0, "y": 0, "w": 400, "h": 400, "color": "red"}, "bad_request")
}

func TestStarsArePrivate(t *testing.T) {
	_, ts := setup(t)
	org := startEvent(t, ts)
	aClient := loginWS(t, ts, "Ada", false)
	a, _ := connectFresh(t, ts, aClient)
	b, _ := connectFresh(t, ts, loginWS(t, ts, "Bob", false))
	org.expectEvent("user_joined")
	org.expectEvent("user_joined")
	a.expectEvent("user_joined")

	note := org.do("create_note", map[string]any{"title": "Star me", "x": 0, "y": 0}, "note_created")["note"].(map[string]any)
	a.expectEvent("note_created")
	b.expectEvent("note_created")

	starred := a.do("star_note", map[string]any{"noteId": note["id"]}, "note_starred")
	if starred["noteId"] != note["id"] {
		t.Fatalf("note_starred: %v", starred)
	}
	b.quiet()
	org.quiet()

	// Starring again changes nothing: an ack, no event.
	id := a.send("star_note", map[string]any{"noteId": note["id"]})
	if f := a.expect("ack"); f.str("cmdId") != id {
		t.Fatalf("%v", f)
	}

	// A second window of the same user hears about it; nobody else does.
	a2, aState := connectFresh(t, ts, aClient)
	_, bState := connectFresh(t, ts, loginWS(t, ts, "Bob", false))
	if aState["notes"].([]any)[0].(map[string]any)["starred"] != true {
		t.Fatal("Ada's snapshot lost her star")
	}
	if bState["notes"].([]any)[0].(map[string]any)["starred"] != false {
		t.Fatal("Bob's snapshot shows Ada's star")
	}
	a.do("unstar_note", map[string]any{"noteId": note["id"]}, "note_unstarred")
	a2.expectEvent("note_unstarred")
	b.quiet()
	org.quiet()
}

// TestDraggingNoteOutOfRegionClearsTag simulates a drag the way the client
// sends it (a stream of move_note commands) that carries a tagged note's
// center across the region's edge. The tag must be cleared for everyone
// and in storage.
func TestDraggingNoteOutOfRegionClearsTag(t *testing.T) {
	s, ts := setup(t)
	org := startEvent(t, ts)
	dragger, _ := connectFresh(t, ts, loginWS(t, ts, "Dee", false))
	org.expectEvent("user_joined")

	org.do("create_region", map[string]any{"label": "Track A", "x": 0, "y": 0, "w": 600, "h": 400, "color": "#ffeeaa"}, "region_created")
	dragger.expectEvent("region_created")
	regionID := lastRegionID(t, s)

	note := dragger.do("create_note", map[string]any{"title": "Drag me out", "x": 100, "y": 100}, "note_created")["note"].(map[string]any)
	org.expectEvent("note_created")
	noteID := note["id"].(string)
	if note["regionId"] != regionID {
		t.Fatalf("note should start inside Track A: %v", note)
	}

	// Drag right in 60-unit steps from x=100 to x=700. The note's center
	// (x+90) leaves the region (right edge 600) once x > 510.
	var cmdIDs []string
	for x := 160; x <= 700; x += 60 {
		cmdIDs = append(cmdIDs, dragger.send("move_note", map[string]any{"noteId": noteID, "x": x, "y": 100}))
	}

	// Collect the dragger's frames until every move is acked.
	acked := map[string]bool{}
	var retags []frame
	var lastMove frame
	for len(acked) < len(cmdIDs) {
		f := dragger.next()
		switch {
		case f.str("type") == "ack":
			acked[f.str("cmdId")] = true
		case f.kind() == "note_moved":
			lastMove = f.event()
		case f.kind() == "note_retagged":
			retags = append(retags, f.event())
		default:
			t.Fatalf("unexpected frame during drag: %v", f)
		}
	}
	if lastMove["x"] != 700.0 {
		t.Fatalf("drag should end at x=700: %v", lastMove)
	}
	// Exactly one retag: the moment the center crossed the edge.
	if len(retags) != 1 || retags[0]["noteId"] != noteID || retags[0]["regionId"] != nil {
		t.Fatalf("want one note_retagged {regionId: null}, got %v", retags)
	}

	// The other window sees the same retag.
	var seen frame
	for seen == nil {
		f := org.next()
		if f.kind() == "note_retagged" {
			seen = f.event()
		}
	}
	if seen["noteId"] != noteID || seen["regionId"] != nil {
		t.Fatalf("other window: %v", seen)
	}

	// A fresh snapshot and the stored row agree: no region.
	_, state := connectFresh(t, ts, loginWS(t, ts, "Late", false))
	for _, n := range state["notes"].([]any) {
		if n := n.(map[string]any); n["id"] == noteID && n["regionId"] != nil {
			t.Fatalf("snapshot still tags the note: %v", n)
		}
	}
	stored, err := s.cfg.Store.NoteByID(context.Background(), noteID)
	if err != nil || stored.RegionID != "" {
		t.Fatalf("stored region_id = %q, %v; want NULL", stored.RegionID, err)
	}
}

// lastRegionID returns the id of the most recently created region.
func lastRegionID(t *testing.T, s *Server) string {
	t.Helper()
	regions, err := s.cfg.Store.Regions(context.Background(), s.cfg.EventID)
	if err != nil || len(regions) == 0 {
		t.Fatalf("regions: %v %v", regions, err)
	}
	return regions[len(regions)-1].ID
}
