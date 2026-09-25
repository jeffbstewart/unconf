package domain

import (
	"crypto/rand"
	"encoding/base32"
	"strings"
	"time"
)

var idEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewID returns an opaque identifier: 16 random bytes, base32, lower case.
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return strings.ToLower(idEncoding.EncodeToString(b[:]))
}

// timestampLayout is RFC 3339 with fixed-width milliseconds, so stored
// timestamps sort chronologically as plain strings (RFC3339Nano trims
// trailing zeros and would not).
const timestampLayout = "2006-01-02T15:04:05.000Z07:00"

// Timestamp formats t as the canonical stored form: UTC RFC 3339 with
// millisecond precision, e.g. 2026-09-25T12:00:00.000Z.
func Timestamp(t time.Time) string {
	return t.UTC().Format(timestampLayout)
}
