// Package integrations defines the Google Workspace seams (SPEC §10) and
// their phase-1 stubs. Real implementations are swapped in by main.go.
package integrations

import (
	"context"
	"log"
	"time"
)

// Breakout is one scheduled session to create a meeting for.
type Breakout struct {
	AssignmentID, NoteID, Title string
	Track                       int
	Start, End                  time.Time
	Attendees                   []string // emails of the proposer and voters who gave one
}

// CalendarService creates the breakout meetings for a locked wave.
type CalendarService interface {
	// CreateBreakouts returns a meeting link per assignment id. It is called
	// outside the hub's transaction (it may be slow network I/O).
	CreateBreakouts(ctx context.Context, waveName string, b []Breakout) (map[string]string, error)
}

// StubCalendar logs the breakouts and returns fake meeting links.
type StubCalendar struct{}

func (StubCalendar) CreateBreakouts(_ context.Context, waveName string, b []Breakout) (map[string]string, error) {
	links := make(map[string]string, len(b))
	for _, x := range b {
		links[x.AssignmentID] = "https://meet.example/" + x.AssignmentID
		log.Printf("calendar stub: wave %q, track %d, %s–%s: %q (%d invitees)",
			waveName, x.Track, x.Start.Format("15:04"), x.End.Format("15:04"), x.Title, len(x.Attendees))
	}
	return links, nil
}
