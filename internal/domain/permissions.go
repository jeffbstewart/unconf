package domain

import "fmt"

// Lifecycle is the event's one-way state: setup → active → done (SPEC §4).
type Lifecycle string

const (
	LifecycleSetup  Lifecycle = "setup"
	LifecycleActive Lifecycle = "active"
	LifecycleDone   Lifecycle = "done"
)

func (l Lifecycle) rank() int {
	switch l {
	case LifecycleSetup:
		return 1
	case LifecycleActive:
		return 2
	case LifecycleDone:
		return 3
	}
	return 0
}

func (l Lifecycle) Valid() bool { return l.rank() > 0 }

// ErrCode classifies a rejected command (SPEC §8.2).
type ErrCode string

const (
	CodeBadRequest    ErrCode = "bad_request"
	CodeForbidden     ErrCode = "forbidden"
	CodeNotAllowedNow ErrCode = "not_allowed_now"
	CodeRateLimited   ErrCode = "rate_limited"
	CodeInternal      ErrCode = "internal"
)

// CmdError is a command rejection sent back to the client. Cause, if set,
// is logged server-side and never sent.
type CmdError struct {
	Code    ErrCode
	Message string
	Cause   error
}

func (e *CmdError) Error() string { return string(e.Code) + ": " + e.Message }
func (e *CmdError) Unwrap() error { return e.Cause }

func BadRequest(format string, args ...any) *CmdError {
	return &CmdError{Code: CodeBadRequest, Message: fmt.Sprintf(format, args...)}
}
func Forbidden(format string, args ...any) *CmdError {
	return &CmdError{Code: CodeForbidden, Message: fmt.Sprintf(format, args...)}
}
func NotAllowedNow(format string, args ...any) *CmdError {
	return &CmdError{Code: CodeNotAllowedNow, Message: fmt.Sprintf(format, args...)}
}

// Actor is the user issuing a command.
type Actor struct {
	UserID string
	Role   Role
}

func (a Actor) IsMod() bool { return a.Role.AtLeast(RoleModerator) }

// NoteFacts is what authorization needs to know about an existing note.
type NoteFacts struct {
	AuthorID    string
	Hidden      bool
	Votes       int
	Assignments int
}

// CanSee reports whether the actor may see a note at all. Hidden notes do
// not exist as far as participants are concerned.
func (a Actor) CanSee(n NoteFacts) bool { return !n.Hidden || a.IsMod() }

// CheckInteract gates every board mutation on the lifecycle: during setup
// only moderators and organizers may act; once done, nobody may.
func CheckInteract(a Actor, lc Lifecycle) *CmdError {
	switch lc {
	case LifecycleDone:
		return NotAllowedNow("the event is over; the board is read-only")
	case LifecycleSetup:
		if !a.IsMod() {
			return NotAllowedNow("the event has not started yet")
		}
	}
	return nil
}

// AuthorizeCreateNote: anyone while active; moderators+ also during setup.
func AuthorizeCreateNote(a Actor, lc Lifecycle) *CmdError {
	return CheckInteract(a, lc)
}

// AuthorizeEditNote covers update_note and set_links: the author or
// moderators+.
func AuthorizeEditNote(a Actor, lc Lifecycle, n NoteFacts) *CmdError {
	if err := CheckInteract(a, lc); err != nil {
		return err
	}
	if n.AuthorID != a.UserID && !a.IsMod() {
		return Forbidden("only the author or a moderator can edit this note")
	}
	return nil
}

// AuthorizeMoveNote: anyone may move any unhidden note.
func AuthorizeMoveNote(a Actor, lc Lifecycle, n NoteFacts) *CmdError {
	if err := CheckInteract(a, lc); err != nil {
		return err
	}
	if n.Hidden {
		return NotAllowedNow("hidden notes cannot be moved")
	}
	return nil
}

// AuthorizeDeleteNote: the author while the note has no votes and no
// assignments; moderators+ always.
func AuthorizeDeleteNote(a Actor, lc Lifecycle, n NoteFacts) *CmdError {
	if err := CheckInteract(a, lc); err != nil {
		return err
	}
	if a.IsMod() {
		return nil
	}
	if n.AuthorID != a.UserID {
		return Forbidden("only the author or a moderator can delete this note")
	}
	if n.Votes > 0 || n.Assignments > 0 {
		return NotAllowedNow("a note with votes or a schedule slot can only be deleted by a moderator")
	}
	return nil
}

// AuthorizeSetLifecycle: organizers only, strictly forward.
func AuthorizeSetLifecycle(a Actor, current, next Lifecycle) *CmdError {
	if !a.Role.AtLeast(RoleOrganizer) {
		return Forbidden("only organizers can change the event lifecycle")
	}
	if !next.Valid() {
		return BadRequest("unknown lifecycle %q", next)
	}
	if next.rank() <= current.rank() {
		return NotAllowedNow("lifecycle can only move forward (currently %s)", current)
	}
	return nil
}
