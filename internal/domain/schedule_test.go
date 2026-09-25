package domain

import (
	"testing"
	"time"
)

func TestAuthorizeSchedule(t *testing.T) {
	if code(AuthorizeSchedule(Actor{Role: RoleParticipant}, LifecycleActive)) != CodeForbidden {
		t.Error("participants may not schedule")
	}
	for _, r := range []Role{RoleModerator, RoleOrganizer} {
		for _, lc := range []Lifecycle{LifecycleSetup, LifecycleActive} {
			if AuthorizeSchedule(Actor{Role: r}, lc) != nil {
				t.Errorf("%s should schedule during %s", r, lc)
			}
		}
		if code(AuthorizeSchedule(Actor{Role: r}, LifecycleDone)) != CodeNotAllowedNow {
			t.Errorf("%s scheduling after done", r)
		}
	}
}

func TestWaveTransitions(t *testing.T) {
	cases := []struct {
		cur, next WaveStatus
		otherOpen bool
		want      ErrCode
	}{
		{WavePlanned, WaveOpen, false, ""},
		{WaveOpen, WaveLocked, false, ""},
		{WaveLocked, WaveDone, false, ""},
		{WavePlanned, WaveOpen, true, CodeNotAllowedNow},    // one open wave at a time
		{WaveOpen, WaveLocked, true, ""},                    // (the flag only matters for opening)
		{WavePlanned, WaveLocked, false, CodeNotAllowedNow}, // no skipping
		{WaveLocked, WaveOpen, false, CodeNotAllowedNow},    // no going back
		{WaveDone, WaveDone, false, CodeNotAllowedNow},
		{WavePlanned, "paused", false, CodeBadRequest},
	}
	for _, c := range cases {
		if got := code(CheckWaveTransition(c.cur, c.next, c.otherOpen)); got != c.want {
			t.Errorf("%s→%s (otherOpen=%v): %q, want %q", c.cur, c.next, c.otherOpen, got, c.want)
		}
	}
	if !WaveLocked.Closed() || !WaveDone.Closed() || WaveOpen.Closed() || WavePlanned.Closed() {
		t.Error("Closed()")
	}
}

func TestParseSlotAndOverlap(t *testing.T) {
	i, err := ParseSlot("2026-10-01T10:00:00Z", "2026-10-01T10:45:00.000Z")
	if err != nil || i.End.Sub(i.Start) != 45*time.Minute {
		t.Fatal(i, err)
	}
	for _, bad := range [][2]string{
		{"10:00", "2026-10-01T10:45:00Z"},
		{"2026-10-01T10:00:00Z", "2026-10-01T10:00:00Z"}, // zero length
		{"2026-10-01T11:00:00Z", "2026-10-01T10:00:00Z"}, // backwards
		{"2026-10-01T00:00:00Z", "2026-10-01T13:00:00Z"}, // > 12h
	} {
		if _, err := ParseSlot(bad[0], bad[1]); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
	at := func(h, m int) time.Time { return time.Date(2026, 10, 1, h, m, 0, 0, time.UTC) }
	a := Interval{at(10, 0), at(10, 45)}
	if !a.Overlaps(Interval{at(10, 30), at(11, 0)}) {
		t.Error("overlap missed")
	}
	if a.Overlaps(Interval{at(10, 45), at(11, 30)}) {
		t.Error("touching ends are not an overlap")
	}
}

func TestValidation(t *testing.T) {
	if n, err := NormalizeWaveName("  After lunch "); err != nil || n != "After lunch" {
		t.Fatal(n, err)
	}
	if _, err := NormalizeWaveName(""); err == nil {
		t.Error("empty name")
	}
	if ValidateTracks(0) == nil || ValidateTracks(21) == nil || ValidateTracks(6) != nil {
		t.Error("tracks bounds")
	}
	if ValidateScheduleThreshold(0) == nil || ValidateScheduleThreshold(2) != nil {
		t.Error("threshold bounds")
	}
}

func TestAuthorizeSetRole(t *testing.T) {
	org := Actor{Role: RoleOrganizer}
	cases := []struct {
		a            Actor
		target, next Role
		want         ErrCode
	}{
		{org, RoleParticipant, RoleModerator, ""},
		{org, RoleModerator, RoleParticipant, ""},
		{org, RoleParticipant, RoleOrganizer, CodeBadRequest},
		{org, RoleOrganizer, RoleParticipant, CodeForbidden},
		{Actor{Role: RoleModerator}, RoleParticipant, RoleModerator, CodeForbidden},
	}
	for _, c := range cases {
		if got := code(AuthorizeSetRole(c.a, c.target, c.next)); got != c.want {
			t.Errorf("%s sets %s→%s: %q, want %q", c.a.Role, c.target, c.next, got, c.want)
		}
	}
}
