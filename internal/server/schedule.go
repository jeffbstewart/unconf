package server

// Scheduling commands (SPEC §4, §8.2): waves of time slots × numbered
// tracks, assignments into cells, and locking — which hands the wave to the
// CalendarService and refunds its voters' votes (§4.1).

import (
	"context"
	"errors"
	"log"
	"sort"
	"time"

	"github.com/jeffbstewart/unconf/internal/domain"
	"github.com/jeffbstewart/unconf/internal/integrations"
	"github.com/jeffbstewart/unconf/internal/store"
)

func init() {
	for name, h := range map[string]commandHandler{
		"create_wave":            decoded((*Hub).createWave),
		"update_wave":            decoded((*Hub).updateWave),
		"delete_wave":            decoded((*Hub).deleteWave),
		"set_wave_status":        decoded((*Hub).setWaveStatus),
		"create_slot":            decoded((*Hub).createSlot),
		"delete_slot":            decoded((*Hub).deleteSlot),
		"assign_note":            decoded((*Hub).assignNote),
		"unassign_note":          decoded((*Hub).unassignNote),
		"clear_wave":             decoded((*Hub).clearWave),
		"set_schedule_threshold": decoded((*Hub).setScheduleThreshold),
		"set_role":               decoded((*Hub).setRole),
	} {
		commandHandlers[name] = h
	}
}

// loadWave loads a wave of this event.
func (h *Hub) loadWave(tx store.Tx, id string) (store.Wave, *domain.CmdError) {
	w, err := tx.WaveByID(h.ctx, id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && w.EventID != h.eventID) {
		return store.Wave{}, domain.BadRequest("no such wave")
	}
	if err != nil {
		return store.Wave{}, internal(err)
	}
	return w, nil
}

// requireWaveStatus fails unless the wave is in one of the given states.
func requireWaveStatus(w store.Wave, allowed ...domain.WaveStatus) *domain.CmdError {
	for _, s := range allowed {
		if domain.WaveStatus(w.Status) == s {
			return nil
		}
	}
	return domain.NotAllowedNow("wave %q is %s", w.Name, w.Status)
}

func waveEvent(kind string, w store.Wave) eventSpec {
	return toAll(E{"kind": kind, "wave": toWaveJSON(w)})
}

type createWavePayload struct {
	Name    string `json:"name"`
	Tracks  *int   `json:"tracks"`
	OpensAt string `json:"opensAt"`
}

func (h *Hub) createWave(tx store.Tx, a domain.Actor, p createWavePayload) (change, *domain.CmdError) {
	if err := domain.AuthorizeSchedule(a, h.lifecycle); err != nil {
		return change{}, err
	}
	name, err := domain.NormalizeWaveName(p.Name)
	if err != nil {
		return change{}, domain.BadRequest("%v", err)
	}
	if p.Tracks == nil {
		return change{}, domain.BadRequest("tracks is required")
	}
	if err := domain.ValidateTracks(*p.Tracks); err != nil {
		return change{}, domain.BadRequest("%v", err)
	}
	opensAt, cerr := normalizeOptionalTime(p.OpensAt)
	if cerr != nil {
		return change{}, cerr
	}
	w := store.Wave{ID: domain.NewID(), EventID: h.eventID, Name: name, Status: string(domain.WavePlanned),
		OpensAt: opensAt, Tracks: *p.Tracks, Slots: []store.Slot{}}
	if err := tx.InsertWave(h.ctx, w); err != nil {
		return change{}, internal(err)
	}
	return change{events: []eventSpec{waveEvent("wave_created", w)}}, nil
}

func normalizeOptionalTime(v string) (string, *domain.CmdError) {
	if v == "" {
		return "", nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return "", domain.BadRequest("opensAt must be an RFC 3339 time")
	}
	return domain.Timestamp(t), nil
}

type updateWavePayload struct {
	WaveID  string  `json:"waveId"`
	Name    *string `json:"name"`
	Tracks  *int    `json:"tracks"`
	OpensAt *string `json:"opensAt"`
}

func (h *Hub) updateWave(tx store.Tx, a domain.Actor, p updateWavePayload) (change, *domain.CmdError) {
	if err := domain.AuthorizeSchedule(a, h.lifecycle); err != nil {
		return change{}, err
	}
	w, cerr := h.loadWave(tx, p.WaveID)
	if cerr != nil {
		return change{}, cerr
	}
	if p.Name != nil {
		name, err := domain.NormalizeWaveName(*p.Name)
		if err != nil {
			return change{}, domain.BadRequest("%v", err)
		}
		w.Name = name
	}
	if p.OpensAt != nil {
		if w.OpensAt, cerr = normalizeOptionalTime(*p.OpensAt); cerr != nil {
			return change{}, cerr
		}
	}
	if p.Tracks != nil && *p.Tracks != w.Tracks {
		if err := requireWaveStatus(w, domain.WavePlanned, domain.WaveOpen); err != nil {
			return change{}, err
		}
		if err := domain.ValidateTracks(*p.Tracks); err != nil {
			return change{}, domain.BadRequest("%v", err)
		}
		assigned, err := tx.WaveAssignments(h.ctx, w.ID)
		if err != nil {
			return change{}, internal(err)
		}
		for _, x := range assigned {
			if x.Track > *p.Tracks {
				return change{}, domain.NotAllowedNow("track %d is in use; empty it first", x.Track)
			}
		}
		w.Tracks = *p.Tracks
	}
	if err := tx.UpdateWave(h.ctx, w); err != nil {
		return change{}, internal(err)
	}
	return change{events: []eventSpec{waveEvent("wave_updated", w)}}, nil
}

type waveIDPayload struct {
	WaveID string `json:"waveId"`
}

func (h *Hub) deleteWave(tx store.Tx, a domain.Actor, p waveIDPayload) (change, *domain.CmdError) {
	if err := domain.AuthorizeSchedule(a, h.lifecycle); err != nil {
		return change{}, err
	}
	w, cerr := h.loadWave(tx, p.WaveID)
	if cerr != nil {
		return change{}, cerr
	}
	if err := requireWaveStatus(w, domain.WavePlanned); err != nil {
		return change{}, err
	}
	if err := tx.DeleteWave(h.ctx, w.ID); err != nil {
		return change{}, internal(err)
	}
	return change{events: []eventSpec{toAll(E{"kind": "wave_deleted", "waveId": w.ID})}}, nil
}

type setWaveStatusPayload struct {
	WaveID string            `json:"waveId"`
	Status domain.WaveStatus `json:"status"`
}

func (h *Hub) setWaveStatus(tx store.Tx, a domain.Actor, p setWaveStatusPayload) (change, *domain.CmdError) {
	if err := domain.AuthorizeSchedule(a, h.lifecycle); err != nil {
		return change{}, err
	}
	w, cerr := h.loadWave(tx, p.WaveID)
	if cerr != nil {
		return change{}, cerr
	}
	otherOpen := false
	if p.Status == domain.WaveOpen {
		waves, err := tx.Waves(h.ctx, h.eventID)
		if err != nil {
			return change{}, internal(err)
		}
		for _, o := range waves {
			otherOpen = otherOpen || (o.ID != w.ID && domain.WaveStatus(o.Status) == domain.WaveOpen)
		}
	}
	if err := domain.CheckWaveTransition(domain.WaveStatus(w.Status), p.Status, otherOpen); err != nil {
		return change{}, err
	}
	w.Status = string(p.Status)
	if err := tx.UpdateWave(h.ctx, w); err != nil {
		return change{}, internal(err)
	}
	if err := h.audit(tx, a, "wave_status_set", map[string]any{"waveId": w.ID, "status": w.Status}); err != nil {
		return change{}, internal(err)
	}
	events := []eventSpec{toAll(E{"kind": "wave_status_set", "waveId": w.ID, "status": w.Status})}
	var after func()
	if p.Status == domain.WaveLocked {
		// The wave's sessions are now history: refund their voters (§4.1).
		refunds, err := h.budgetUpdates(tx, w.ID)
		if err != nil {
			return change{}, internal(err)
		}
		events = append(events, refunds...)
		breakouts, err := h.breakouts(tx, w)
		if err != nil {
			return change{}, internal(err)
		}
		after = func() { go h.createMeetings(w.Name, breakouts) }
	}
	return change{events: events, after: after}, nil
}

// budgetUpdates sends every voter of the wave's sessions their new
// votesUsed, privately (SPEC §4.1).
func (h *Hub) budgetUpdates(tx store.Tx, waveID string) ([]eventSpec, error) {
	assigned, err := tx.WaveAssignments(h.ctx, waveID)
	if err != nil {
		return nil, err
	}
	users := map[string]bool{}
	for _, x := range assigned {
		voters, err := tx.Voters(h.ctx, x.NoteID)
		if err != nil {
			return nil, err
		}
		for _, u := range voters {
			users[u] = true
		}
	}
	ids := make([]string, 0, len(users))
	for u := range users {
		ids = append(ids, u)
	}
	sort.Strings(ids)
	var out []eventSpec
	for _, u := range ids {
		used, err := tx.VotesCast(h.ctx, u)
		if err != nil {
			return nil, err
		}
		ev := E{"kind": "votes_used_set", "votesUsed": used}
		out = append(out, eventSpec{part: ev, mod: ev, onlyUser: u})
	}
	return out, nil
}

// breakouts describes a wave's sessions for the CalendarService.
func (h *Hub) breakouts(tx store.Tx, w store.Wave) ([]integrations.Breakout, error) {
	assigned, err := tx.WaveAssignments(h.ctx, w.ID)
	if err != nil {
		return nil, err
	}
	slots := map[string]store.Slot{}
	for _, sl := range w.Slots {
		slots[sl.ID] = sl
	}
	var out []integrations.Breakout
	for _, x := range assigned {
		n, err := tx.NoteByID(h.ctx, x.NoteID)
		if err != nil {
			return nil, err
		}
		iv, err := domain.ParseSlot(slots[x.SlotID].StartAt, slots[x.SlotID].EndAt)
		if err != nil {
			return nil, err
		}
		people, err := tx.Voters(h.ctx, n.ID)
		if err != nil {
			return nil, err
		}
		var emails []string
		for _, id := range append([]string{n.AuthorID}, people...) {
			if u, err := tx.UserByID(h.ctx, id); err == nil && u.Email != "" {
				emails = append(emails, u.Email)
			}
		}
		out = append(out, integrations.Breakout{
			AssignmentID: x.ID, NoteID: n.ID, Title: n.Title, Track: x.Track,
			Start: iv.Start, End: iv.End, Attendees: emails,
		})
	}
	return out, nil
}

// createMeetings runs off the hub goroutine (network I/O), then posts the
// links back to the hub as an assignment_links_set event.
func (h *Hub) createMeetings(waveName string, b []integrations.Breakout) {
	if len(b) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(h.ctx, 2*time.Minute)
	defer cancel()
	links, err := h.calendar.CreateBreakouts(ctx, waveName, b)
	if err != nil {
		log.Printf("calendar: wave %q: %v", waveName, err)
		return
	}
	_ = h.post(func() {
		_, cerr := h.commit(func(tx store.Tx) (change, *domain.CmdError) {
			saved := map[string]string{}
			for id, url := range links {
				if err := tx.SetAssignmentMeetURL(h.ctx, id, url); errors.Is(err, store.ErrNotFound) {
					continue // unassigned since (only possible if the note was deleted)
				} else if err != nil {
					return change{}, internal(err)
				}
				saved[id] = url
			}
			if len(saved) == 0 {
				return change{}, nil
			}
			return change{events: []eventSpec{toAll(E{"kind": "assignment_links_set", "links": saved})}}, nil
		})
		if cerr != nil {
			log.Printf("calendar: storing links for wave %q: %v", waveName, cerr)
		}
	})
}

type createSlotPayload struct {
	WaveID  string `json:"waveId"`
	StartAt string `json:"startAt"`
	EndAt   string `json:"endAt"`
}

func (h *Hub) createSlot(tx store.Tx, a domain.Actor, p createSlotPayload) (change, *domain.CmdError) {
	if err := domain.AuthorizeSchedule(a, h.lifecycle); err != nil {
		return change{}, err
	}
	w, cerr := h.loadWave(tx, p.WaveID)
	if cerr != nil {
		return change{}, cerr
	}
	if err := requireWaveStatus(w, domain.WavePlanned, domain.WaveOpen); err != nil {
		return change{}, err
	}
	iv, err := domain.ParseSlot(p.StartAt, p.EndAt)
	if err != nil {
		return change{}, domain.BadRequest("%v", err)
	}
	for _, o := range w.Slots {
		other, err := domain.ParseSlot(o.StartAt, o.EndAt)
		if err == nil && iv.Overlaps(other) {
			return change{}, domain.BadRequest("overlaps the %s–%s slot", other.Start.Format("15:04"), other.End.Format("15:04"))
		}
	}
	sl := store.Slot{ID: domain.NewID(), WaveID: w.ID, StartAt: domain.Timestamp(iv.Start), EndAt: domain.Timestamp(iv.End)}
	if err := tx.InsertSlot(h.ctx, sl); err != nil {
		return change{}, internal(err)
	}
	return change{events: []eventSpec{toAll(E{"kind": "slot_created", "waveId": w.ID,
		"slot": slotJSON{sl.ID, sl.StartAt, sl.EndAt}})}}, nil
}

type slotIDPayload struct {
	SlotID string `json:"slotId"`
}

func (h *Hub) deleteSlot(tx store.Tx, a domain.Actor, p slotIDPayload) (change, *domain.CmdError) {
	if err := domain.AuthorizeSchedule(a, h.lifecycle); err != nil {
		return change{}, err
	}
	sl, err := tx.SlotByID(h.ctx, p.SlotID)
	if errors.Is(err, store.ErrNotFound) {
		return change{}, domain.BadRequest("no such slot")
	} else if err != nil {
		return change{}, internal(err)
	}
	w, cerr := h.loadWave(tx, sl.WaveID)
	if cerr != nil {
		return change{}, cerr
	}
	if err := requireWaveStatus(w, domain.WavePlanned, domain.WaveOpen); err != nil {
		return change{}, err
	}
	assigned, err := tx.WaveAssignments(h.ctx, w.ID)
	if err != nil {
		return change{}, internal(err)
	}
	for _, x := range assigned {
		if x.SlotID == sl.ID {
			return change{}, domain.NotAllowedNow("the slot has sessions in it; empty it first")
		}
	}
	if err := tx.DeleteSlot(h.ctx, sl.ID); err != nil {
		return change{}, internal(err)
	}
	return change{events: []eventSpec{toAll(E{"kind": "slot_deleted", "waveId": w.ID, "slotId": sl.ID})}}, nil
}

type assignNotePayload struct {
	NoteID string `json:"noteId"`
	SlotID string `json:"slotId"`
	Track  *int   `json:"track"`
}

func (h *Hub) assignNote(tx store.Tx, a domain.Actor, p assignNotePayload) (change, *domain.CmdError) {
	if err := domain.AuthorizeSchedule(a, h.lifecycle); err != nil {
		return change{}, err
	}
	n, facts, cerr := h.visibleNote(tx, a, p.NoteID)
	if cerr != nil {
		return change{}, cerr
	}
	if facts.Hidden {
		return change{}, domain.NotAllowedNow("hidden notes can't be scheduled")
	}
	sl, err := tx.SlotByID(h.ctx, p.SlotID)
	if errors.Is(err, store.ErrNotFound) {
		return change{}, domain.BadRequest("no such slot")
	} else if err != nil {
		return change{}, internal(err)
	}
	w, cerr := h.loadWave(tx, sl.WaveID)
	if cerr != nil {
		return change{}, cerr
	}
	if err := requireWaveStatus(w, domain.WaveOpen); err != nil {
		return change{}, err
	}
	if p.Track == nil || *p.Track < 1 || *p.Track > w.Tracks {
		return change{}, domain.BadRequest("track must be 1–%d", w.Tracks)
	}
	// At most one assignment per note per wave: a new cell replaces the old.
	assigned, err := tx.WaveAssignments(h.ctx, w.ID)
	if err != nil {
		return change{}, internal(err)
	}
	var events []eventSpec
	for _, x := range assigned {
		if x.SlotID == sl.ID && x.Track == *p.Track {
			if x.NoteID == n.ID {
				return change{}, nil // already there
			}
			return change{}, domain.NotAllowedNow("that cell is taken")
		}
	}
	for _, x := range assigned {
		if x.NoteID == n.ID {
			if err := tx.DeleteAssignment(h.ctx, x.ID); err != nil {
				return change{}, internal(err)
			}
			events = append(events, toAll(E{"kind": "note_unassigned", "assignmentId": x.ID}))
		}
	}
	x := store.Assignment{ID: domain.NewID(), NoteID: n.ID, SlotID: sl.ID, Track: *p.Track}
	if err := tx.InsertAssignment(h.ctx, x); err != nil {
		return change{}, internal(err)
	}
	events = append(events, toAll(E{"kind": "note_assigned", "assignment": toAssignmentJSON(x)}))
	return change{events: events}, nil
}

type assignmentIDPayload struct {
	AssignmentID string `json:"assignmentId"`
}

func (h *Hub) unassignNote(tx store.Tx, a domain.Actor, p assignmentIDPayload) (change, *domain.CmdError) {
	if err := domain.AuthorizeSchedule(a, h.lifecycle); err != nil {
		return change{}, err
	}
	x, err := tx.AssignmentByID(h.ctx, p.AssignmentID)
	if errors.Is(err, store.ErrNotFound) {
		return change{}, domain.BadRequest("no such assignment")
	} else if err != nil {
		return change{}, internal(err)
	}
	sl, err := tx.SlotByID(h.ctx, x.SlotID)
	if err != nil {
		return change{}, internal(err)
	}
	w, cerr := h.loadWave(tx, sl.WaveID)
	if cerr != nil {
		return change{}, cerr
	}
	if err := requireWaveStatus(w, domain.WaveOpen); err != nil {
		return change{}, err
	}
	if err := tx.DeleteAssignment(h.ctx, x.ID); err != nil {
		return change{}, internal(err)
	}
	return change{events: []eventSpec{toAll(E{"kind": "note_unassigned", "assignmentId": x.ID})}}, nil
}

func (h *Hub) clearWave(tx store.Tx, a domain.Actor, p waveIDPayload) (change, *domain.CmdError) {
	if err := domain.AuthorizeSchedule(a, h.lifecycle); err != nil {
		return change{}, err
	}
	w, cerr := h.loadWave(tx, p.WaveID)
	if cerr != nil {
		return change{}, cerr
	}
	if err := requireWaveStatus(w, domain.WaveOpen); err != nil {
		return change{}, err
	}
	assigned, err := tx.WaveAssignments(h.ctx, w.ID)
	if err != nil {
		return change{}, internal(err)
	}
	var events []eventSpec
	for _, x := range assigned {
		if err := tx.DeleteAssignment(h.ctx, x.ID); err != nil {
			return change{}, internal(err)
		}
		events = append(events, toAll(E{"kind": "note_unassigned", "assignmentId": x.ID}))
	}
	return change{events: events}, nil
}

type setThresholdPayload struct {
	N *int `json:"n"`
}

func (h *Hub) setScheduleThreshold(tx store.Tx, a domain.Actor, p setThresholdPayload) (change, *domain.CmdError) {
	if err := domain.AuthorizeSchedule(a, h.lifecycle); err != nil {
		return change{}, err
	}
	if p.N == nil {
		return change{}, domain.BadRequest("n is required")
	}
	if err := domain.ValidateScheduleThreshold(*p.N); err != nil {
		return change{}, err
	}
	if err := tx.SetScheduleThreshold(h.ctx, h.eventID, *p.N); err != nil {
		return change{}, internal(err)
	}
	return change{events: []eventSpec{toAll(E{"kind": "schedule_threshold_set", "n": *p.N})}}, nil
}

type setRolePayload struct {
	UserID string      `json:"userId"`
	Role   domain.Role `json:"role"`
}

func (h *Hub) setRole(tx store.Tx, a domain.Actor, p setRolePayload) (change, *domain.CmdError) {
	u, err := tx.UserByID(h.ctx, p.UserID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && u.EventID != h.eventID) {
		return change{}, domain.BadRequest("no such user")
	} else if err != nil {
		return change{}, internal(err)
	}
	if err := domain.AuthorizeSetRole(a, u.Role, p.Role); err != nil {
		return change{}, err
	}
	if u.Role == p.Role {
		return change{}, nil
	}
	if err := tx.SetUserRole(h.ctx, u.ID, p.Role); err != nil {
		return change{}, internal(err)
	}
	if err := h.audit(tx, a, "role_changed", map[string]any{"userId": u.ID, "from": u.Role, "to": p.Role}); err != nil {
		return change{}, internal(err)
	}
	u.Role = p.Role
	return change{
		events: []eventSpec{toAll(E{"kind": "role_set", "userId": u.ID, "role": u.Role})},
		after:  func() { h.refreshRole(u) },
	}, nil
}
