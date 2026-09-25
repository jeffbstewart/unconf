package server

// Command handlers (SPEC §8.2). Each runs on the hub goroutine inside a
// transaction and returns the events to publish, or a CmdError.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jeffbstewart/unconf/internal/domain"
	"github.com/jeffbstewart/unconf/internal/store"
)

type commandHandler func(h *Hub, tx store.Tx, a domain.Actor, payload json.RawMessage) (change, *domain.CmdError)

// commandHandlers lists the commands implemented so far; move_note is
// handled separately because moves are coalesced.
var commandHandlers = map[string]commandHandler{
	"create_note":   decoded((*Hub).createNote),
	"update_note":   decoded((*Hub).updateNote),
	"delete_note":   decoded((*Hub).deleteNote),
	"set_links":     decoded((*Hub).setLinks),
	"set_lifecycle": decoded((*Hub).setLifecycle),
}

// decoded adapts a handler taking a typed payload.
func decoded[P any](fn func(*Hub, store.Tx, domain.Actor, P) (change, *domain.CmdError)) commandHandler {
	return func(h *Hub, tx store.Tx, a domain.Actor, raw json.RawMessage) (change, *domain.CmdError) {
		var p P
		if err := decodePayload(raw, &p); err != nil {
			return change{}, err
		}
		return fn(h, tx, a, p)
	}
}

// decodePayload strictly decodes a command payload.
func decodePayload(raw json.RawMessage, v any) *domain.CmdError {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return domain.BadRequest("invalid payload: %v", err)
	}
	return nil
}

func internal(err error) *domain.CmdError {
	if err == nil {
		return nil
	}
	return &domain.CmdError{Code: domain.CodeInternal, Message: "internal error", Cause: err}
}

// audience makes an event for a note visible to participants only while
// the note is not hidden.
func audience(hidden bool, ev E) eventSpec {
	if hidden {
		return eventSpec{mod: ev}
	}
	return toAll(ev)
}

// visibleNote loads a note the actor is allowed to know exists.
func (h *Hub) visibleNote(tx store.Tx, a domain.Actor, id string) (store.Note, domain.NoteFacts, *domain.CmdError) {
	n, err := tx.NoteByID(h.ctx, id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && n.EventID != h.eventID) {
		return store.Note{}, domain.NoteFacts{}, domain.BadRequest("no such note")
	}
	if err != nil {
		return store.Note{}, domain.NoteFacts{}, internal(err)
	}
	facts, err := tx.NoteFacts(h.ctx, n)
	if err != nil {
		return store.Note{}, domain.NoteFacts{}, internal(err)
	}
	if !a.CanSee(facts) {
		return store.Note{}, domain.NoteFacts{}, domain.BadRequest("no such note")
	}
	return n, facts, nil
}

// obstacles lists the rectangles of visible notes other than exclude.
// Hidden notes never block placement (SPEC §9).
func (h *Hub) obstacles(exclude string) []domain.Rect {
	out := make([]domain.Rect, 0, len(h.geo))
	for id, g := range h.geo {
		if id != exclude && !g.hidden {
			out = append(out, domain.NoteRect(g.x, g.y))
		}
	}
	return out
}

func coords(x, y *float64) (float64, float64, *domain.CmdError) {
	if x == nil || y == nil {
		return 0, 0, domain.BadRequest("x and y are required")
	}
	if !domain.ValidCoord(*x) || !domain.ValidCoord(*y) {
		return 0, 0, domain.BadRequest("coordinates out of range")
	}
	return *x, *y, nil
}

type createNotePayload struct {
	Title  string   `json:"title"`
	BodyMD string   `json:"bodyMd"`
	X      *float64 `json:"x"`
	Y      *float64 `json:"y"`
	Color  string   `json:"color"`
}

func (h *Hub) createNote(tx store.Tx, a domain.Actor, p createNotePayload) (change, *domain.CmdError) {
	if err := domain.AuthorizeCreateNote(a, h.lifecycle); err != nil {
		return change{}, err
	}
	title, err := domain.NormalizeTitle(p.Title)
	if err != nil {
		return change{}, domain.BadRequest("%v", err)
	}
	if err := domain.ValidateBody(p.BodyMD); err != nil {
		return change{}, domain.BadRequest("%v", err)
	}
	color, err := domain.NormalizeColor(p.Color)
	if err != nil {
		return change{}, domain.BadRequest("%v", err)
	}
	x, y, cerr := coords(p.X, p.Y)
	if cerr != nil {
		return change{}, cerr
	}
	x, y = domain.ResolvePosition(x, y, h.obstacles(""))
	now := domain.Timestamp(time.Now())
	n := store.Note{
		ID: domain.NewID(), EventID: h.eventID, AuthorID: a.UserID,
		Title: title, BodyMD: p.BodyMD, X: x, Y: y, Color: color,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := tx.InsertNote(h.ctx, n); err != nil {
		return change{}, internal(err)
	}
	return change{
		events: []eventSpec{toAll(E{"kind": "note_created", "note": toNoteJSON(n, nil)})},
		after:  func() { h.geo[n.ID] = &noteGeo{x: x, y: y} },
	}, nil
}

type updateNotePayload struct {
	NoteID string  `json:"noteId"`
	Title  *string `json:"title"`
	BodyMD *string `json:"bodyMd"`
	Color  *string `json:"color"`
}

func (h *Hub) updateNote(tx store.Tx, a domain.Actor, p updateNotePayload) (change, *domain.CmdError) {
	n, facts, cerr := h.visibleNote(tx, a, p.NoteID)
	if cerr != nil {
		return change{}, cerr
	}
	if err := domain.AuthorizeEditNote(a, h.lifecycle, facts); err != nil {
		return change{}, err
	}
	if p.Title == nil && p.BodyMD == nil && p.Color == nil {
		return change{}, domain.BadRequest("nothing to update")
	}
	var err error
	if p.Title != nil {
		if n.Title, err = domain.NormalizeTitle(*p.Title); err != nil {
			return change{}, domain.BadRequest("%v", err)
		}
	}
	if p.BodyMD != nil {
		if err := domain.ValidateBody(*p.BodyMD); err != nil {
			return change{}, domain.BadRequest("%v", err)
		}
		n.BodyMD = *p.BodyMD
	}
	if p.Color != nil {
		if n.Color, err = domain.NormalizeColor(*p.Color); err != nil {
			return change{}, domain.BadRequest("%v", err)
		}
	}
	n.UpdatedAt = domain.Timestamp(time.Now())
	if err := tx.UpdateNoteContent(h.ctx, n.ID, n.Title, n.BodyMD, n.Color, n.UpdatedAt); err != nil {
		return change{}, internal(err)
	}
	return change{events: []eventSpec{audience(n.Hidden(), E{
		"kind": "note_updated", "noteId": n.ID, "title": n.Title, "bodyMd": n.BodyMD,
		"color": n.Color, "updatedAt": n.UpdatedAt,
	})}}, nil
}

type movePayload struct {
	NoteID string   `json:"noteId"`
	X      *float64 `json:"x"`
	Y      *float64 `json:"y"`
}

func (h *Hub) moveNote(tx store.Tx, a domain.Actor, p movePayload) (change, *domain.CmdError) {
	n, facts, cerr := h.visibleNote(tx, a, p.NoteID)
	if cerr != nil {
		return change{}, cerr
	}
	if err := domain.AuthorizeMoveNote(a, h.lifecycle, facts); err != nil {
		return change{}, err
	}
	x, y, cerr := coords(p.X, p.Y)
	if cerr != nil {
		return change{}, cerr
	}
	x, y = domain.ResolvePosition(x, y, h.obstacles(n.ID))
	if err := tx.MoveNote(h.ctx, n.ID, x, y); err != nil {
		return change{}, internal(err)
	}
	return change{
		events: []eventSpec{toAll(E{"kind": "note_moved", "noteId": n.ID, "x": x, "y": y, "byUserId": a.UserID})},
		after:  func() { h.geo[n.ID].x, h.geo[n.ID].y = x, y },
	}, nil
}

type noteIDPayload struct {
	NoteID string `json:"noteId"`
}

func (h *Hub) deleteNote(tx store.Tx, a domain.Actor, p noteIDPayload) (change, *domain.CmdError) {
	n, facts, cerr := h.visibleNote(tx, a, p.NoteID)
	if cerr != nil {
		return change{}, cerr
	}
	if err := domain.AuthorizeDeleteNote(a, h.lifecycle, facts); err != nil {
		return change{}, err
	}
	if err := tx.DeleteNote(h.ctx, n.ID); err != nil {
		return change{}, internal(err)
	}
	return change{
		events: []eventSpec{audience(n.Hidden(), E{"kind": "note_deleted", "noteId": n.ID})},
		after: func() {
			delete(h.geo, n.ID)
			delete(h.lastMove, n.ID)
		},
	}, nil
}

type setLinksPayload struct {
	NoteID string        `json:"noteId"`
	Links  []domain.Link `json:"links"`
}

func (h *Hub) setLinks(tx store.Tx, a domain.Actor, p setLinksPayload) (change, *domain.CmdError) {
	n, facts, cerr := h.visibleNote(tx, a, p.NoteID)
	if cerr != nil {
		return change{}, cerr
	}
	if err := domain.AuthorizeEditNote(a, h.lifecycle, facts); err != nil {
		return change{}, err
	}
	links, err := domain.NormalizeLinks(p.Links)
	if err != nil {
		return change{}, domain.BadRequest("%v", err)
	}
	for i := range links {
		links[i].ID = domain.NewID()
	}
	if err := tx.ReplaceLinks(h.ctx, n.ID, links); err != nil {
		return change{}, internal(err)
	}
	return change{events: []eventSpec{audience(n.Hidden(), E{"kind": "links_set", "noteId": n.ID, "links": links})}}, nil
}

type setLifecyclePayload struct {
	Lifecycle domain.Lifecycle `json:"lifecycle"`
}

func (h *Hub) setLifecycle(tx store.Tx, a domain.Actor, p setLifecyclePayload) (change, *domain.CmdError) {
	if err := domain.AuthorizeSetLifecycle(a, h.lifecycle, p.Lifecycle); err != nil {
		return change{}, err
	}
	if err := tx.SetLifecycle(h.ctx, h.eventID, p.Lifecycle); err != nil {
		return change{}, internal(err)
	}
	detail, _ := json.Marshal(map[string]string{"from": string(h.lifecycle), "to": string(p.Lifecycle)})
	if err := tx.AppendAudit(h.ctx, store.AuditEntry{
		EventID: h.eventID, ActorID: a.UserID, Action: "lifecycle_set", Target: h.eventID, Detail: string(detail),
	}); err != nil {
		return change{}, internal(err)
	}
	return change{
		events: []eventSpec{toAll(E{"kind": "lifecycle_set", "lifecycle": p.Lifecycle})},
		after:  func() { h.lifecycle = p.Lifecycle },
	}, nil
}

// auditRoleChange records a role change made through the admin key.
func auditRoleChange(ctx context.Context, tx store.Tx, eventID, userID string, from, to domain.Role) error {
	detail, _ := json.Marshal(map[string]string{"from": string(from), "to": string(to), "via": "admin_key"})
	return tx.AppendAudit(ctx, store.AuditEntry{
		EventID: eventID, ActorID: userID, Action: "role_changed", Target: userID, Detail: string(detail),
	})
}

// buildSnapshot assembles the full state visible to u (SPEC §8.4).
// Participants never receive hidden notes or messages, or anything attached
// to a hidden note.
func (h *Hub) buildSnapshot(u store.User) (snapshotJSON, error) {
	ctx, st := h.ctx, h.st
	isMod := u.Role.AtLeast(domain.RoleModerator)
	var s snapshotJSON
	ev, err := st.Event(ctx, h.eventID)
	if err != nil {
		return s, err
	}
	s.Event = eventJSON{ev.ID, ev.Name, ev.Lifecycle, ev.VotingOpen, ev.VotesPerUser}

	users, err := st.Users(ctx, h.eventID)
	if err != nil {
		return s, err
	}
	s.Users = make([]userJSON, 0, len(users))
	for _, x := range users {
		s.Users = append(s.Users, toUserJSON(x))
	}

	notes, err := st.Notes(ctx, h.eventID)
	if err != nil {
		return s, err
	}
	links, err := st.EventLinks(ctx, h.eventID)
	if err != nil {
		return s, err
	}
	totals, err := st.VoteTotals(ctx, h.eventID)
	if err != nil {
		return s, err
	}
	mine, err := st.UserVotes(ctx, u.ID)
	if err != nil {
		return s, err
	}
	stars, err := st.UserStars(ctx, u.ID)
	if err != nil {
		return s, err
	}
	assignments, err := st.Assignments(ctx, h.eventID)
	if err != nil {
		return s, err
	}
	scheduled := map[string]bool{}
	for _, a := range assignments {
		scheduled[a.NoteID] = true
	}
	visible := map[string]bool{}
	s.Notes = make([]noteJSON, 0, len(notes))
	for _, n := range notes {
		if n.Hidden() && !isMod {
			continue
		}
		visible[n.ID] = true
		j := toNoteJSON(n, links[n.ID])
		j.VoteTotal, j.MyVotes, j.Starred, j.Scheduled = totals[n.ID], mine[n.ID], stars[n.ID], scheduled[n.ID]
		s.Notes = append(s.Notes, j)
	}
	used := 0
	for _, v := range mine {
		used += v
	}
	s.Me.VotesRemaining = max(0, ev.VotesPerUser-used)

	regions, err := st.Regions(ctx, h.eventID)
	if err != nil {
		return s, err
	}
	s.Regions = make([]regionJSON, 0, len(regions))
	for _, r := range regions {
		s.Regions = append(s.Regions, regionJSON{r.ID, r.Label, r.X, r.Y, r.W, r.H, r.Color, r.Z})
	}

	waves, err := st.Waves(ctx, h.eventID)
	if err != nil {
		return s, err
	}
	s.Waves = make([]waveJSON, 0, len(waves))
	for _, w := range waves {
		wj := waveJSON{ID: w.ID, Name: w.Name, Status: w.Status, OpensAt: optional(w.OpensAt), Slots: []slotJSON{}}
		for _, sl := range w.Slots {
			wj.Slots = append(wj.Slots, slotJSON{sl.ID, sl.StartAt, sl.EndAt})
		}
		s.Waves = append(s.Waves, wj)
	}

	rooms, err := st.Rooms(ctx, h.eventID)
	if err != nil {
		return s, err
	}
	s.Rooms = make([]roomJSON, 0, len(rooms))
	for _, r := range rooms {
		s.Rooms = append(s.Rooms, roomJSON{r.ID, r.Name, optional(r.MeetURL)})
	}

	s.Assignments = make([]assignmentJSON, 0, len(assignments))
	for _, a := range assignments {
		if visible[a.NoteID] {
			s.Assignments = append(s.Assignments, assignmentJSON{a.ID, a.NoteID, a.SlotID, a.RoomID})
		}
	}

	msgs, err := st.Messages(ctx, h.eventID)
	if err != nil {
		return s, err
	}
	s.Messages = map[string][]messageJSON{}
	for _, m := range msgs {
		if !visible[m.NoteID] || (m.Hidden() && !isMod) {
			continue
		}
		s.Messages[m.NoteID] = append(s.Messages[m.NoteID], messageJSON{
			ID: m.ID, AuthorID: m.AuthorID, ThreadID: optional(m.ThreadID), Body: m.Body,
			Hidden: m.Hidden(), CreatedAt: m.CreatedAt,
		})
	}
	return s, nil
}
