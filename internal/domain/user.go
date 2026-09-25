package domain

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaxNameLen  = 40
	MaxEmailLen = 254
)

var (
	ErrNameEmpty   = errors.New("name is required")
	ErrNameTooLong = errors.New("name must be at most 40 characters")
	ErrNameInvalid = errors.New("name contains control characters")
	ErrEmail       = errors.New("email is not valid")
)

// NormalizeName trims a display name and validates it: 1–40 characters
// (counted as Unicode code points), no control characters.
func NormalizeName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	switch n := utf8.RuneCountInString(name); {
	case n == 0:
		return "", ErrNameEmpty
	case n > MaxNameLen:
		return "", ErrNameTooLong
	}
	if !utf8.ValidString(name) || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", ErrNameInvalid
	}
	return name, nil
}

// NameKey is the case-insensitive uniqueness key for a normalized name.
func NameKey(name string) string {
	return strings.ToLower(name)
}

// NormalizeEmail trims an optional email. Empty is allowed; otherwise it
// must look like local@domain. Real verification arrives with SSO.
func NormalizeEmail(raw string) (string, error) {
	email := strings.TrimSpace(raw)
	if email == "" {
		return "", nil
	}
	at := strings.LastIndexByte(email, '@')
	if len(email) > MaxEmailLen || at < 1 || at == len(email)-1 ||
		strings.IndexFunc(email, unicode.IsSpace) >= 0 {
		return "", ErrEmail
	}
	return email, nil
}
