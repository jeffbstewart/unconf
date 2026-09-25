package server

import (
	"testing"
)

// votingSetup starts an event with voting open, two participants, and one note.
func votingSetup(t *testing.T) (org, a, b *wsClient, noteID string) {
	t.Helper()
	_, ts := setup(t)
	org = startEvent(t, ts)
	a, _ = connectFresh(t, ts, loginWS(t, ts, "Ada", false))
	b, _ = connectFresh(t, ts, loginWS(t, ts, "Bob", false))
	org.expectEvent("user_joined")
	org.expectEvent("user_joined")
	a.expectEvent("user_joined")
	noteID = org.do("create_note", map[string]any{"title": "Vote me", "x": 0, "y": 0}, "note_created")["note"].(map[string]any)["id"].(string)
	a.expectEvent("note_created")
	b.expectEvent("note_created")
	org.do("set_votes_per_user", map[string]any{"n": 3}, "votes_per_user_set")
	a.expectEvent("votes_per_user_set")
	b.expectEvent("votes_per_user_set")
	org.do("set_voting", map[string]any{"open": true}, "voting_set")
	a.expectEvent("voting_set")
	b.expectEvent("voting_set")
	return org, a, b, noteID
}

func TestVotesTallyLiveInAllWindows(t *testing.T) {
	org, a, b, id := votingSetup(t)
	cast := func(w *wsClient, others []*wsClient, want float64) {
		t.Helper()
		ev := w.do("cast_vote", map[string]any{"noteId": id}, "vote_cast")
		if ev["total"] != want || ev["noteId"] != id {
			t.Fatalf("vote_cast: %v, want total %v", ev, want)
		}
		for _, o := range others {
			if got := o.expectEvent("vote_cast").event(); got["total"] != want || got["byUserId"] != ev["byUserId"] {
				t.Fatalf("other window: %v", got)
			}
		}
	}
	cast(a, []*wsClient{b, org}, 1)
	cast(b, []*wsClient{a, org}, 2)
	cast(a, []*wsClient{b, org}, 3) // stacking: a second dot from Ada

	ev := a.do("retract_vote", map[string]any{"noteId": id}, "vote_retracted")
	if ev["total"] != 2.0 {
		t.Fatalf("vote_retracted: %v", ev)
	}
	b.expectEvent("vote_retracted")
	org.expectEvent("vote_retracted")
}

func TestVoteBudgetEnforcedServerSide(t *testing.T) {
	_, a, _, id := votingSetup(t)
	for i := 0; i < 3; i++ {
		a.do("cast_vote", map[string]any{"noteId": id}, "vote_cast")
	}
	msg := a.fail("cast_vote", map[string]any{"noteId": id}, "not_allowed_now")
	if msg == "" {
		t.Fatal("expected an explanation")
	}
	// Retracting frees a vote.
	a.do("retract_vote", map[string]any{"noteId": id}, "vote_retracted")
	a.do("cast_vote", map[string]any{"noteId": id}, "vote_cast")
}

func TestClosedVotingRejects(t *testing.T) {
	org, a, b, id := votingSetup(t)
	a.do("cast_vote", map[string]any{"noteId": id}, "vote_cast")
	b.expectEvent("vote_cast")
	org.expectEvent("vote_cast")

	org.do("set_voting", map[string]any{"open": false}, "voting_set")
	if ev := a.expectEvent("voting_set").event(); ev["open"] != false {
		t.Fatalf("voting_set: %v", ev)
	}
	a.fail("cast_vote", map[string]any{"noteId": id}, "not_allowed_now")
	a.fail("retract_vote", map[string]any{"noteId": id}, "not_allowed_now")
	org.fail("cast_vote", map[string]any{"noteId": id}, "not_allowed_now")

	// Re-opening lets the existing dot be retracted.
	org.do("set_voting", map[string]any{"open": true}, "voting_set")
	a.expectEvent("voting_set")
	a.do("retract_vote", map[string]any{"noteId": id}, "vote_retracted")
	a.fail("retract_vote", map[string]any{"noteId": id}, "bad_request") // no dot left
}

func TestVotingSettingsPermissions(t *testing.T) {
	org, a, _, _ := votingSetup(t)
	a.fail("set_voting", map[string]any{"open": false}, "forbidden")
	a.fail("set_votes_per_user", map[string]any{"n": 10}, "forbidden")
	org.fail("set_votes_per_user", map[string]any{"n": 0}, "bad_request")
	org.fail("set_votes_per_user", map[string]any{"n": 21}, "bad_request")
	org.fail("set_voting", map[string]any{}, "bad_request")
	// Setting voting to its current state is a no-op: ack, no event.
	id := org.send("set_voting", map[string]any{"open": true})
	if f := org.expect("ack"); f.str("cmdId") != id {
		t.Fatalf("%v", f)
	}
	a.quiet()
}

func TestSnapshotCarriesVotes(t *testing.T) {
	_, ts := setup(t)
	org := startEvent(t, ts)
	adaClient := loginWS(t, ts, "Ada", false)
	a, _ := connectFresh(t, ts, adaClient)
	org.expectEvent("user_joined")
	id := org.do("create_note", map[string]any{"title": "N", "x": 0, "y": 0}, "note_created")["note"].(map[string]any)["id"]
	a.expectEvent("note_created")
	org.do("set_voting", map[string]any{"open": true}, "voting_set")
	a.expectEvent("voting_set")
	a.do("cast_vote", map[string]any{"noteId": id}, "vote_cast")
	a.do("cast_vote", map[string]any{"noteId": id}, "vote_cast")
	org.expectEvent("vote_cast")
	org.expectEvent("vote_cast")
	org.do("cast_vote", map[string]any{"noteId": id}, "vote_cast")

	_, st := connectFresh(t, ts, adaClient)
	note := st["notes"].([]any)[0].(map[string]any)
	me := st["me"].(map[string]any)
	if note["voteTotal"] != 3.0 || note["myVotes"] != 2.0 || me["votesUsed"] != 2.0 || me["votesRemaining"] != 3.0 {
		t.Fatalf("snapshot votes: note %v, me %v", note, me)
	}
	ev := st["event"].(map[string]any)
	if ev["votingOpen"] != true || ev["votesPerUser"] != 5.0 {
		t.Fatalf("snapshot event: %v", ev)
	}

	// Deleting the note (a moderator may, despite votes) refunds the dots.
	org.do("delete_note", map[string]any{"noteId": id}, "note_deleted")
	_, st = connectFresh(t, ts, adaClient)
	if me := st["me"].(map[string]any); me["votesUsed"] != 0.0 || me["votesRemaining"] != 5.0 {
		t.Fatalf("after delete: %v", me)
	}
}
