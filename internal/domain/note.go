package domain

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

const (
	MaxTitleLen     = 120
	MaxBodyLen      = 20000
	MaxLinks        = 20
	MaxLinkTitleLen = 200
	MaxURLLen       = 2048
	DefaultColor    = "yellow"
)

// NoteColors are the sticky colors a note may have.
var NoteColors = []string{"yellow", "pink", "blue", "green", "orange", "purple"}

// LinkKinds classify a note's links for display.
var LinkKinds = []string{"doc", "slides", "other"}

// Link is an attachment on a note.
type Link struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
	Kind  string `json:"kind"`
}

// NormalizeTitle trims and validates a note title (1–120 characters).
func NormalizeTitle(raw string) (string, error) {
	t := strings.TrimSpace(raw)
	n := utf8.RuneCountInString(t)
	if n == 0 {
		return "", errors.New("title is required")
	}
	if n > MaxTitleLen {
		return "", fmt.Errorf("title must be at most %d characters", MaxTitleLen)
	}
	return t, nil
}

// ValidateBody checks a markdown body's length (≤ 20000 characters).
func ValidateBody(body string) error {
	if !utf8.ValidString(body) {
		return errors.New("body is not valid UTF-8")
	}
	if utf8.RuneCountInString(body) > MaxBodyLen {
		return fmt.Errorf("body must be at most %d characters", MaxBodyLen)
	}
	return nil
}

// NormalizeColor returns the color, DefaultColor for "", or an error.
func NormalizeColor(c string) (string, error) {
	if c == "" {
		return DefaultColor, nil
	}
	for _, ok := range NoteColors {
		if c == ok {
			return c, nil
		}
	}
	return "", fmt.Errorf("unknown color %q", c)
}

// NormalizeLinks validates a full replacement list of links: at most 20,
// each with a 1–200 character title, an absolute http(s) URL, and a known
// kind (default "other"). IDs are left for the caller to assign.
func NormalizeLinks(in []Link) ([]Link, error) {
	if len(in) > MaxLinks {
		return nil, fmt.Errorf("at most %d links", MaxLinks)
	}
	out := make([]Link, 0, len(in))
	for i, l := range in {
		title := strings.TrimSpace(l.Title)
		if n := utf8.RuneCountInString(title); n == 0 || n > MaxLinkTitleLen {
			return nil, fmt.Errorf("link %d: title must be 1–%d characters", i+1, MaxLinkTitleLen)
		}
		raw := strings.TrimSpace(l.URL)
		u, err := url.Parse(raw)
		if err != nil || len(raw) > MaxURLLen || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("link %d: URL must be an absolute http(s) URL", i+1)
		}
		kind := l.Kind
		if kind == "" {
			kind = "other"
		}
		if !contains(LinkKinds, kind) {
			return nil, fmt.Errorf("link %d: unknown kind %q", i+1, kind)
		}
		out = append(out, Link{Title: title, URL: u.String(), Kind: kind})
	}
	return out, nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
