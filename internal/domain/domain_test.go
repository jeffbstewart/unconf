package domain

import (
	"strings"
	"testing"
	"time"
)

func TestRoleOrdering(t *testing.T) {
	roles := []Role{RoleParticipant, RoleModerator, RoleOrganizer}
	for i, have := range roles {
		for j, need := range roles {
			if got, want := have.AtLeast(need), i >= j; got != want {
				t.Errorf("%s.AtLeast(%s) = %v, want %v", have, need, got, want)
			}
		}
	}
	if Role("admin").AtLeast(RoleParticipant) {
		t.Error("unknown role must not satisfy anything")
	}
	if RoleOrganizer.AtLeast(Role("")) {
		t.Error("nothing satisfies an invalid requirement")
	}
}

func TestParseRole(t *testing.T) {
	if r, err := ParseRole("moderator"); err != nil || r != RoleModerator {
		t.Fatalf("got %q, %v", r, err)
	}
	for _, bad := range []string{"", "Moderator", "admin"} {
		if _, err := ParseRole(bad); err == nil {
			t.Errorf("ParseRole(%q) succeeded", bad)
		}
	}
}

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		in, want string
		err      error
	}{
		{"  Ada Lovelace  ", "Ada Lovelace", nil},
		{"é", "é", nil},
		{strings.Repeat("ü", 40), strings.Repeat("ü", 40), nil}, // runes, not bytes
		{"", "", ErrNameEmpty},
		{"   \t ", "", ErrNameEmpty},
		{strings.Repeat("x", 41), "", ErrNameTooLong},
		{"bad\x00name", "", ErrNameInvalid},
		{"two\nlines", "", ErrNameInvalid},
	}
	for _, tt := range tests {
		got, err := NormalizeName(tt.in)
		if got != tt.want || err != tt.err {
			t.Errorf("NormalizeName(%q) = %q, %v; want %q, %v", tt.in, got, err, tt.want, tt.err)
		}
	}
}

func TestNameKeyCaseInsensitive(t *testing.T) {
	if NameKey("Ada") != NameKey("aDA") {
		t.Fatal("name keys should match case-insensitively")
	}
}

func TestNormalizeEmail(t *testing.T) {
	ok := map[string]string{"": "", " a@b.c ": "a@b.c", "x@y": "x@y"}
	for in, want := range ok {
		if got, err := NormalizeEmail(in); err != nil || got != want {
			t.Errorf("NormalizeEmail(%q) = %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"nope", "@b.c", "a@", "a b@c.d", strings.Repeat("a", 250) + "@b.cd"} {
		if _, err := NormalizeEmail(bad); err == nil {
			t.Errorf("NormalizeEmail(%q) succeeded", bad)
		}
	}
}

func TestNewID(t *testing.T) {
	a, b := NewID(), NewID()
	if a == b || len(a) != 26 || strings.ToLower(a) != a {
		t.Fatalf("bad ids %q %q", a, b)
	}
}

func TestTimestampSortsAsString(t *testing.T) {
	base := time.Date(2026, 9, 25, 12, 0, 5, 0, time.FixedZone("EDT", -4*3600))
	a := Timestamp(base.Add(100 * time.Millisecond))
	b := Timestamp(base.Add(120 * time.Millisecond))
	c := Timestamp(base.Add(time.Second))
	if a != "2026-09-25T16:00:05.100Z" {
		t.Fatalf("format: %s", a)
	}
	if !(a < b && b < c) {
		t.Fatalf("not chronological as strings: %s %s %s", a, b, c)
	}
	if _, err := time.Parse(time.RFC3339, a); err != nil {
		t.Fatalf("not RFC 3339: %v", err)
	}
}
