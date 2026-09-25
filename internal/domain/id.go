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

// Timestamp formats t as the canonical stored form: UTC RFC 3339.
func Timestamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}
