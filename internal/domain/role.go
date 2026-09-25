// Package domain holds the core types and rules of the unconference:
// roles, identity validation, lifecycle and wave state machines, command
// validation, and board geometry. It has no I/O dependencies.
package domain

import "fmt"

// Role is a user's permission level. Roles are strictly ordered:
// participant < moderator < organizer (SPEC §3).
type Role string

const (
	RoleParticipant Role = "participant"
	RoleModerator   Role = "moderator"
	RoleOrganizer   Role = "organizer"
)

func (r Role) rank() int {
	switch r {
	case RoleParticipant:
		return 1
	case RoleModerator:
		return 2
	case RoleOrganizer:
		return 3
	}
	return 0
}

// Valid reports whether r is one of the three known roles.
func (r Role) Valid() bool { return r.rank() > 0 }

// AtLeast reports whether r grants everything min grants. It fails closed:
// false if either role is invalid.
func (r Role) AtLeast(min Role) bool {
	return r.Valid() && min.Valid() && r.rank() >= min.rank()
}

// ParseRole converts a string to a Role, rejecting unknown values.
func ParseRole(s string) (Role, error) {
	r := Role(s)
	if !r.Valid() {
		return "", fmt.Errorf("unknown role %q", s)
	}
	return r, nil
}
