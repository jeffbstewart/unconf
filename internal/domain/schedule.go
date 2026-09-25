package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxWaveNameLen       = 60
	MaxTracks            = 20
	MaxSlotLength        = 12 * time.Hour
	MinScheduleThreshold = 1
	MaxScheduleThreshold = 20
)

// WaveStatus is a wave's one-way state: planned → open → locked → done.
type WaveStatus string

const (
	WavePlanned WaveStatus = "planned"
	WaveOpen    WaveStatus = "open"
	WaveLocked  WaveStatus = "locked"
	WaveDone    WaveStatus = "done"
)

var waveOrder = []WaveStatus{WavePlanned, WaveOpen, WaveLocked, WaveDone}

func (w WaveStatus) rank() int {
	for i, s := range waveOrder {
		if s == w {
			return i + 1
		}
	}
	return 0
}

func (w WaveStatus) Valid() bool { return w.rank() > 0 }

// Closed reports whether the wave's schedule is final (locked or done):
// its sessions' votes are history (SPEC §4.1).
func (w WaveStatus) Closed() bool { return w == WaveLocked || w == WaveDone }

// AuthorizeSchedule: schedulers are moderators and organizers (SPEC §3);
// nothing changes once the event is done.
func AuthorizeSchedule(a Actor, lc Lifecycle) *CmdError {
	if !a.IsMod() {
		return Forbidden("only moderators and organizers can schedule")
	}
	if lc == LifecycleDone {
		return NotAllowedNow("the event is over")
	}
	return nil
}

// CheckWaveTransition allows exactly the next step of the state machine.
// otherOpen reports whether a different wave is currently open.
func CheckWaveTransition(cur, next WaveStatus, otherOpen bool) *CmdError {
	if !next.Valid() {
		return BadRequest("unknown wave status %q", next)
	}
	if next.rank() != cur.rank()+1 {
		return NotAllowedNow("a %s wave can only move to %s", cur, waveOrder[min(cur.rank(), len(waveOrder)-1)])
	}
	if next == WaveOpen && otherOpen {
		return NotAllowedNow("another wave is already open; lock it first")
	}
	return nil
}

// NormalizeWaveName trims and validates a wave name (1–60 characters).
func NormalizeWaveName(raw string) (string, error) {
	n := strings.TrimSpace(raw)
	if c := utf8.RuneCountInString(n); c == 0 || c > MaxWaveNameLen {
		return "", fmt.Errorf("wave name must be 1–%d characters", MaxWaveNameLen)
	}
	return n, nil
}

// ValidateTracks checks a wave's parallel track count (1–20).
func ValidateTracks(n int) error {
	if n < 1 || n > MaxTracks {
		return fmt.Errorf("tracks must be 1–%d", MaxTracks)
	}
	return nil
}

// Interval is a slot's time range.
type Interval struct{ Start, End time.Time }

// ParseSlot parses RFC 3339 start/end times; the slot must end after it
// starts and last at most 12 hours.
func ParseSlot(start, end string) (Interval, error) {
	s, err := time.Parse(time.RFC3339, start)
	if err != nil {
		return Interval{}, fmt.Errorf("startAt must be an RFC 3339 time")
	}
	e, err := time.Parse(time.RFC3339, end)
	if err != nil {
		return Interval{}, fmt.Errorf("endAt must be an RFC 3339 time")
	}
	if !e.After(s) {
		return Interval{}, fmt.Errorf("a slot must end after it starts")
	}
	if e.Sub(s) > MaxSlotLength {
		return Interval{}, fmt.Errorf("a slot may last at most %v", MaxSlotLength)
	}
	return Interval{s, e}, nil
}

// Overlaps reports whether two intervals share any time (touching ends
// don't count).
func (i Interval) Overlaps(o Interval) bool {
	return i.Start.Before(o.End) && o.Start.Before(i.End)
}

// ValidateScheduleThreshold checks the suggester's minimum voter count.
func ValidateScheduleThreshold(n int) *CmdError {
	if n < MinScheduleThreshold || n > MaxScheduleThreshold {
		return BadRequest("threshold must be %d–%d", MinScheduleThreshold, MaxScheduleThreshold)
	}
	return nil
}

// AuthorizeSetRole: organizers switch users between participant and
// moderator. Organizers themselves can only be changed via the admin key
// (SPEC §3), which avoids lockout games.
func AuthorizeSetRole(a Actor, target, next Role) *CmdError {
	if !a.Role.AtLeast(RoleOrganizer) {
		return Forbidden("only organizers can change roles")
	}
	if next != RoleParticipant && next != RoleModerator {
		return BadRequest("role must be participant or moderator")
	}
	if target == RoleOrganizer {
		return Forbidden("organizers can't be demoted here")
	}
	return nil
}
