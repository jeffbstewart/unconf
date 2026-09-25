package server

import (
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
