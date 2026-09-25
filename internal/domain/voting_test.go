package domain

import "testing"

func code(e *CmdError) ErrCode {
	if e == nil {
		return ""
	}
	return e.Code
}

func TestAuthorizeVote(t *testing.T) {
	visible, hidden := NoteFacts{AuthorID: "x"}, NoteFacts{AuthorID: "x", Hidden: true}
	cases := []struct {
		role Role
		lc   Lifecycle
		open bool
		n    NoteFacts
		want ErrCode
	}{
		{RoleParticipant, LifecycleActive, true, visible, ""},
		{RoleModerator, LifecycleActive, true, visible, ""},
		{RoleOrganizer, LifecycleActive, true, visible, ""},
		{RoleParticipant, LifecycleActive, false, visible, CodeNotAllowedNow}, // voting closed
		{RoleOrganizer, LifecycleActive, false, visible, CodeNotAllowedNow},
		{RoleParticipant, LifecycleSetup, true, visible, CodeNotAllowedNow}, // not started
		{RoleOrganizer, LifecycleSetup, true, visible, ""},                  // mods+ may try it out in setup
		{RoleParticipant, LifecycleDone, true, visible, CodeNotAllowedNow},
		{RoleOrganizer, LifecycleDone, true, visible, CodeNotAllowedNow},
		{RoleModerator, LifecycleActive, true, hidden, CodeNotAllowedNow},
	}
	for _, c := range cases {
		got := code(AuthorizeVote(Actor{UserID: "u", Role: c.role}, c.lc, c.open, c.n))
		if got != c.want {
			t.Errorf("%s/%s/open=%v/hidden=%v: %q, want %q", c.role, c.lc, c.open, c.n.Hidden, got, c.want)
		}
	}
}

func TestVoteBudget(t *testing.T) {
	if CheckVoteBudget(4, 5) != nil {
		t.Error("4 of 5 used should allow one more")
	}
	if code(CheckVoteBudget(5, 5)) != CodeNotAllowedNow || code(CheckVoteBudget(7, 5)) != CodeNotAllowedNow {
		t.Error("exhausted or over budget should be not_allowed_now")
	}
}

func TestVotingSettings(t *testing.T) {
	for _, r := range []Role{RoleParticipant, RoleModerator} {
		if code(AuthorizeVotingSettings(Actor{Role: r}, LifecycleActive)) != CodeForbidden {
			t.Errorf("%s may not change voting", r)
		}
	}
	o := Actor{Role: RoleOrganizer}
	for _, lc := range []Lifecycle{LifecycleSetup, LifecycleActive} {
		if AuthorizeVotingSettings(o, lc) != nil {
			t.Errorf("organizer blocked in %s", lc)
		}
	}
	if code(AuthorizeVotingSettings(o, LifecycleDone)) != CodeNotAllowedNow {
		t.Error("done should block voting changes")
	}
	for n, ok := range map[int]bool{0: false, 1: true, 5: true, 20: true, 21: false, -3: false} {
		if (ValidateVotesPerUser(n) == nil) != ok {
			t.Errorf("votes per user %d: ok=%v expected", n, ok)
		}
	}
}
