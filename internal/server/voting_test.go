package server

import (
	"fmt"
	"testing"
)

// votingSetup starts an event with a budget of 3, voting open, two
// participants, and four notes (so budgets can run out).
func votingSetup(t *testing.T) (org, a, b *wsClient, notes []string) {
	t.Helper()
	_, ts := setup(t)
	org = startEvent(t, ts)
	a, _ = connectFresh(t, ts, loginWS(t, ts, "Ada", false))
	b, _ = connectFresh(t, ts, loginWS(t, ts, "Bob", false))
	org.expectEvent("user_joined")
	org.expectEvent("user_joined")
	a.expectEvent("user_joined")
	for i := 0; i < 4; i++ {
		ev := org.do("create_note", map[string]any{"title": fmt.Sprintf("N%d", i), "x": float64(i * 400), "y": 0}, "note_created")
		notes = append(notes, ev["note"].(map[string]any)["id"].(string))
		a.expectEvent("note_created")
		b.expectEvent("note_created")
	}
	org.do("set_votes_per_user", map[string]any{"n": 3}, "votes_per_user_set")
	a.expectEvent("votes_per_user_set")
	b.expectEvent("votes_per_user_set")
	org.do("set_voting", map[string]any{"open": true}, "voting_set")
	a.expectEvent("voting_set")
	b.expectEvent("voting_set")
	return org, a, b, notes
}

func TestVotesTallyLiveInAllWindows(t *testing.T) {
	org, a, b, notes := votingSetup(t)
	id := notes[0]
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

	ev := a.do("retract_vote", map[string]any{"noteId": id}, "vote_retracted")
	if ev["total"] != 1.0 {
		t.Fatalf("vote_retracted: %v", ev)
	}
	b.expectEvent("vote_retracted")
	org.expectEvent("vote_retracted")
}

func TestOneVotePerPersonPerSession(t *testing.T) {
	org, a, b, notes := votingSetup(t)
	a.do("cast_vote", map[string]any{"noteId": notes[0]}, "vote_cast")
	b.expectEvent("vote_cast")
	org.expectEvent("vote_cast")

	// Voting again for the same session is a no-op: ack, no event, no budget.
	id := a.send("cast_vote", map[string]any{"noteId": notes[0]})
	if f := a.expect("ack"); f.str("cmdId") != id {
		t.Fatalf("%v", f)
	}
	b.quiet()
	// So the other two votes still fit in the budget of 3.
	a.do("cast_vote", map[string]any{"noteId": notes[1]}, "vote_cast")
	a.do("cast_vote", map[string]any{"noteId": notes[2]}, "vote_cast")

	// At budget, re-voting an already-backed session is still just a no-op.
	id = a.send("cast_vote", map[string]any{"noteId": notes[0]})
	if f := a.expect("ack"); f.str("cmdId") != id {
		t.Fatalf("re-vote at budget: %v", f)
	}
}

func TestVoteBudgetEnforcedServerSide(t *testing.T) {
	_, a, _, notes := votingSetup(t)
	for _, id := range notes[:3] {
		a.do("cast_vote", map[string]any{"noteId": id}, "vote_cast")
	}
	if msg := a.fail("cast_vote", map[string]any{"noteId": notes[3]}, "not_allowed_now"); msg == "" {
		t.Fatal("expected an explanation")
	}
	// Retracting frees a vote for another session.
	a.do("retract_vote", map[string]any{"noteId": notes[0]}, "vote_retracted")
	a.do("cast_vote", map[string]any{"noteId": notes[3]}, "vote_cast")
}

func TestClosedVotingRejects(t *testing.T) {
	org, a, b, notes := votingSetup(t)
	id := notes[0]
	a.do("cast_vote", map[string]any{"noteId": id}, "vote_cast")
	b.expectEvent("vote_cast")
	org.expectEvent("vote_cast")

	org.do("set_voting", map[string]any{"open": false}, "voting_set")
	if ev := a.expectEvent("voting_set").event(); ev["open"] != false {
		t.Fatalf("voting_set: %v", ev)
	}
	a.fail("cast_vote", map[string]any{"noteId": notes[1]}, "not_allowed_now")
	a.fail("retract_vote", map[string]any{"noteId": id}, "not_allowed_now")
	org.fail("cast_vote", map[string]any{"noteId": id}, "not_allowed_now")

	// Re-opening lets the existing vote be retracted.
	org.do("set_voting", map[string]any{"open": true}, "voting_set")
	a.expectEvent("voting_set")
	a.do("retract_vote", map[string]any{"noteId": id}, "vote_retracted")
	a.fail("retract_vote", map[string]any{"noteId": id}, "bad_request") // no vote left there
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
	var ids []any
	for i := 0; i < 2; i++ {
		ids = append(ids, org.do("create_note", map[string]any{"title": "N", "x": float64(i * 400), "y": 0}, "note_created")["note"].(map[string]any)["id"])
		a.expectEvent("note_created")
	}
	org.do("set_voting", map[string]any{"open": true}, "voting_set")
	a.expectEvent("voting_set")
	a.do("cast_vote", map[string]any{"noteId": ids[0]}, "vote_cast")
	a.do("cast_vote", map[string]any{"noteId": ids[1]}, "vote_cast")
	org.expectEvent("vote_cast")
	org.expectEvent("vote_cast")
	org.do("cast_vote", map[string]any{"noteId": ids[0]}, "vote_cast")

	_, st := connectFresh(t, ts, adaClient)
	byID := map[any]map[string]any{}
	for _, n := range st["notes"].([]any) {
		byID[n.(map[string]any)["id"]] = n.(map[string]any)
	}
	me := st["me"].(map[string]any)
	if n := byID[ids[0]]; n["voteTotal"] != 2.0 || n["voted"] != true {
		t.Fatalf("note 0: %v", n)
	}
	if _, has := byID[ids[0]]["myVotes"]; has {
		t.Fatal("myVotes was replaced by voted")
	}
	if me["votesUsed"] != 2.0 || me["votesRemaining"] != 3.0 {
		t.Fatalf("me: %v", me)
	}

	// Deleting a note (a moderator may, despite votes) refunds its voters.
	org.do("delete_note", map[string]any{"noteId": ids[0]}, "note_deleted")
	_, st = connectFresh(t, ts, adaClient)
	if me := st["me"].(map[string]any); me["votesUsed"] != 1.0 || me["votesRemaining"] != 4.0 {
		t.Fatalf("after delete: %v", me)
	}
}
