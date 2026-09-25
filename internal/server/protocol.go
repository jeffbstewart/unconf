package server

// WebSocket wire shapes (SPEC §8). web/src/api/protocol.ts mirrors these.

import (
	"encoding/json"

	"github.com/jeffbstewart/unconf/internal/domain"
	"github.com/jeffbstewart/unconf/internal/store"
)

// clientFrame is any client → server frame.
type clientFrame struct {
	Type    string          `json:"type"` // "cmd" | "ping"
	CmdID   string          `json:"cmdId"`
	Cmd     string          `json:"cmd"`
	Payload json.RawMessage `json:"payload"`
}

type userJSON struct {
	ID   string      `json:"id"`
	Name string      `json:"name"`
	Role domain.Role `json:"role"`
}

func toUserJSON(u store.User) userJSON { return userJSON{u.ID, u.Name, u.Role} }

type noteJSON struct {
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	BodyMD    string        `json:"bodyMd"`
	AuthorID  string        `json:"authorId"`
	X         float64       `json:"x"`
	Y         float64       `json:"y"`
	Color     string        `json:"color"`
	RegionID  *string       `json:"regionId"`
	VoteTotal int           `json:"voteTotal"`
	Voters    []string      `json:"voters"` // user ids; votes are public (SPEC §11)
	Voted     bool          `json:"voted"`  // this user voted for it
	Starred   bool          `json:"starred"`
	Hidden    bool          `json:"hidden,omitempty"`
	Links     []domain.Link `json:"links"`
	Scheduled bool          `json:"scheduled"`
	CreatedAt string        `json:"createdAt"`
	UpdatedAt string        `json:"updatedAt"`
}

func toNoteJSON(n store.Note, links []domain.Link) noteJSON {
	if links == nil {
		links = []domain.Link{}
	}
	return noteJSON{
		ID: n.ID, Title: n.Title, BodyMD: n.BodyMD, AuthorID: n.AuthorID,
		X: n.X, Y: n.Y, Color: n.Color, RegionID: optional(n.RegionID),
		Hidden: n.Hidden(), Links: links, Voters: []string{}, CreatedAt: n.CreatedAt, UpdatedAt: n.UpdatedAt,
	}
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

type eventJSON struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Lifecycle         string `json:"lifecycle"`
	VotingOpen        bool   `json:"votingOpen"`
	VotesPerUser      int    `json:"votesPerUser"`
	ScheduleThreshold int    `json:"scheduleThreshold"`
}

type regionJSON struct {
	ID    string  `json:"id"`
	Label string  `json:"label"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	W     float64 `json:"w"`
	H     float64 `json:"h"`
	Color string  `json:"color"`
	Z     int     `json:"z"`
}

type slotJSON struct {
	ID      string `json:"id"`
	StartAt string `json:"startAt"`
	EndAt   string `json:"endAt"`
}

type waveJSON struct {
	ID      string     `json:"id"`
	Name    string     `json:"name"`
	Status  string     `json:"status"`
	OpensAt *string    `json:"opensAt"`
	Tracks  int        `json:"tracks"`
	Slots   []slotJSON `json:"slots"`
}

func toWaveJSON(w store.Wave) waveJSON {
	j := waveJSON{ID: w.ID, Name: w.Name, Status: w.Status, OpensAt: optional(w.OpensAt), Tracks: w.Tracks, Slots: []slotJSON{}}
	for _, sl := range w.Slots {
		j.Slots = append(j.Slots, slotJSON{sl.ID, sl.StartAt, sl.EndAt})
	}
	return j
}

type assignmentJSON struct {
	ID      string  `json:"id"`
	NoteID  string  `json:"noteId"`
	SlotID  string  `json:"slotId"`
	Track   int     `json:"track"`
	MeetURL *string `json:"meetUrl"`
}

func toAssignmentJSON(a store.Assignment) assignmentJSON {
	return assignmentJSON{a.ID, a.NoteID, a.SlotID, a.Track, optional(a.MeetURL)}
}

type messageJSON struct {
	ID        string  `json:"id"`
	AuthorID  string  `json:"authorId"`
	ThreadID  *string `json:"threadId"`
	Body      string  `json:"body"`
	Hidden    bool    `json:"hidden,omitempty"`
	CreatedAt string  `json:"createdAt"`
}

type snapshotJSON struct {
	Event       eventJSON                `json:"event"`
	Users       []userJSON               `json:"users"`
	Notes       []noteJSON               `json:"notes"`
	Regions     []regionJSON             `json:"regions"`
	Waves       []waveJSON               `json:"waves"`
	Assignments []assignmentJSON         `json:"assignments"`
	Messages    map[string][]messageJSON `json:"messages"`
	Me          struct {
		VotesRemaining int `json:"votesRemaining"`
		VotesUsed      int `json:"votesUsed"`
	} `json:"me"`
}

// E is an event body: {"kind": ..., fields...}.
type E map[string]any

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // only our own plain data types are encoded
	}
	return b
}

func helloFrame(u store.User, seq int64) []byte {
	return mustJSON(map[string]any{"type": "hello", "you": toUserJSON(u), "eventSeq": seq})
}

func snapshotFrame(seq int64, s snapshotJSON) []byte {
	return mustJSON(map[string]any{"type": "snapshot", "seq": seq, "state": s})
}

func eventFrame(seq int64, ev E) []byte {
	return mustJSON(map[string]any{"type": "event", "seq": seq, "event": ev})
}

func ackFrame(cmdID string, seq int64) []byte {
	return mustJSON(map[string]any{"type": "ack", "cmdId": cmdID, "seq": seq})
}

func errorFrame(cmdID string, e *domain.CmdError) []byte {
	return mustJSON(map[string]any{"type": "error", "cmdId": cmdID, "code": e.Code, "message": e.Message})
}

var pongFrame = []byte(`{"type":"pong"}`)
