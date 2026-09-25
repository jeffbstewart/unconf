package store

import (
	"context"
	"database/sql"
	"errors"
)

// Wave is one scheduling round: time slots × numbered tracks.
type Wave struct {
	ID, EventID, Name, Status string
	OpensAt                   string // "" = unset
	Tracks                    int
	Slots                     []Slot
}

// Slot is a time interval within a wave.
type Slot struct {
	ID, WaveID, StartAt, EndAt string
}

// Assignment places a note in a (slot, track) cell.
type Assignment struct {
	ID, NoteID, SlotID string
	Track              int
	MeetURL            string // "" until the wave's meetings are created
}

// Waves lists an event's waves with their slots (by start time), in
// creation order.
func (s Queries) Waves(ctx context.Context, eventID string) ([]Wave, error) {
	waves, err := collect(ctx, s.db, scanWave,
		"SELECT "+waveColumns+" FROM waves WHERE event_id = ? ORDER BY rowid", eventID)
	if err != nil {
		return nil, err
	}
	slots, err := collect(ctx, s.db, scanSlot,
		`SELECT s.id, s.wave_id, s.start_at, s.end_at FROM slots s JOIN waves w ON w.id = s.wave_id
		 WHERE w.event_id = ? ORDER BY s.start_at, s.rowid`, eventID)
	if err != nil {
		return nil, err
	}
	idx := map[string]int{}
	for i := range waves {
		waves[i].Slots = []Slot{}
		idx[waves[i].ID] = i
	}
	for _, sl := range slots {
		waves[idx[sl.WaveID]].Slots = append(waves[idx[sl.WaveID]].Slots, sl)
	}
	return waves, nil
}

const waveColumns = "id, event_id, name, status, COALESCE(opens_at, ''), tracks"

func scanWave(r *sql.Rows) (Wave, error) {
	var w Wave
	return w, r.Scan(&w.ID, &w.EventID, &w.Name, &w.Status, &w.OpensAt, &w.Tracks)
}

func scanSlot(r *sql.Rows) (Slot, error) {
	var x Slot
	return x, r.Scan(&x.ID, &x.WaveID, &x.StartAt, &x.EndAt)
}

// WaveByID loads one wave with its slots.
func (s Queries) WaveByID(ctx context.Context, id string) (Wave, error) {
	var w Wave
	err := s.db.QueryRowContext(ctx, "SELECT "+waveColumns+" FROM waves WHERE id = ?", id).
		Scan(&w.ID, &w.EventID, &w.Name, &w.Status, &w.OpensAt, &w.Tracks)
	if errors.Is(err, sql.ErrNoRows) {
		return Wave{}, ErrNotFound
	}
	if err != nil {
		return Wave{}, err
	}
	w.Slots, err = collect(ctx, s.db, scanSlot,
		"SELECT id, wave_id, start_at, end_at FROM slots WHERE wave_id = ? ORDER BY start_at, rowid", id)
	if w.Slots == nil {
		w.Slots = []Slot{}
	}
	return w, err
}

// InsertWave stores a new wave (without slots).
func (s Queries) InsertWave(ctx context.Context, w Wave) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO waves (id, event_id, name, status, opens_at, tracks) VALUES (?, ?, ?, ?, ?, ?)",
		w.ID, w.EventID, w.Name, w.Status, nullIfEmpty(w.OpensAt), w.Tracks)
	return err
}

// UpdateWave rewrites a wave's name, opens-at, tracks, and status.
func (s Queries) UpdateWave(ctx context.Context, w Wave) error {
	return s.execOne(ctx, "UPDATE waves SET name = ?, opens_at = ?, tracks = ?, status = ? WHERE id = ?",
		w.Name, nullIfEmpty(w.OpensAt), w.Tracks, w.Status, w.ID)
}

// DeleteWave removes a wave; its slots and their assignments go with it.
func (s Queries) DeleteWave(ctx context.Context, id string) error {
	return s.execOne(ctx, "DELETE FROM waves WHERE id = ?", id)
}

// SlotByID loads one slot.
func (s Queries) SlotByID(ctx context.Context, id string) (Slot, error) {
	var x Slot
	err := s.db.QueryRowContext(ctx, "SELECT id, wave_id, start_at, end_at FROM slots WHERE id = ?", id).
		Scan(&x.ID, &x.WaveID, &x.StartAt, &x.EndAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Slot{}, ErrNotFound
	}
	return x, err
}

// InsertSlot stores a new slot.
func (s Queries) InsertSlot(ctx context.Context, x Slot) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO slots (id, wave_id, start_at, end_at) VALUES (?, ?, ?, ?)",
		x.ID, x.WaveID, x.StartAt, x.EndAt)
	return err
}

// DeleteSlot removes a slot (and, by cascade, its assignments).
func (s Queries) DeleteSlot(ctx context.Context, id string) error {
	return s.execOne(ctx, "DELETE FROM slots WHERE id = ?", id)
}

const assignmentColumns = "a.id, a.note_id, a.slot_id, a.track, COALESCE(a.meet_url, '')"

func scanAssignment(r interface{ Scan(...any) error }) (Assignment, error) {
	var x Assignment
	err := r.Scan(&x.ID, &x.NoteID, &x.SlotID, &x.Track, &x.MeetURL)
	if errors.Is(err, sql.ErrNoRows) {
		return Assignment{}, ErrNotFound
	}
	return x, err
}

// Assignments lists an event's assignments.
func (s Queries) Assignments(ctx context.Context, eventID string) ([]Assignment, error) {
	return collect(ctx, s.db, func(r *sql.Rows) (Assignment, error) { return scanAssignment(r) },
		`SELECT `+assignmentColumns+` FROM assignments a
		 JOIN slots s ON s.id = a.slot_id JOIN waves w ON w.id = s.wave_id
		 WHERE w.event_id = ? ORDER BY a.rowid`, eventID)
}

// WaveAssignments lists one wave's assignments.
func (s Queries) WaveAssignments(ctx context.Context, waveID string) ([]Assignment, error) {
	return collect(ctx, s.db, func(r *sql.Rows) (Assignment, error) { return scanAssignment(r) },
		`SELECT `+assignmentColumns+` FROM assignments a JOIN slots s ON s.id = a.slot_id
		 WHERE s.wave_id = ? ORDER BY a.rowid`, waveID)
}

// AssignmentByID loads one assignment.
func (s Queries) AssignmentByID(ctx context.Context, id string) (Assignment, error) {
	return scanAssignment(s.db.QueryRowContext(ctx, "SELECT "+assignmentColumns+" FROM assignments a WHERE a.id = ?", id))
}

// InsertAssignment places a note in a cell. It returns ErrConflict if the
// cell is taken.
func (s Queries) InsertAssignment(ctx context.Context, a Assignment) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO assignments (id, note_id, slot_id, track) VALUES (?, ?, ?, ?)",
		a.ID, a.NoteID, a.SlotID, a.Track)
	if isUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// DeleteAssignment removes one assignment.
func (s Queries) DeleteAssignment(ctx context.Context, id string) error {
	return s.execOne(ctx, "DELETE FROM assignments WHERE id = ?", id)
}

// SetAssignmentMeetURL stores the meeting link for an assignment.
func (s Queries) SetAssignmentMeetURL(ctx context.Context, id, url string) error {
	return s.execOne(ctx, "UPDATE assignments SET meet_url = ? WHERE id = ?", url, id)
}

// ScheduledInClosedWave reports whether a note is assigned in a locked or
// done wave — its votes are history (SPEC §4.1).
func (s Queries) ScheduledInClosedWave(ctx context.Context, noteID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM assignments a
		JOIN slots s ON s.id = a.slot_id JOIN waves w ON w.id = s.wave_id
		WHERE a.note_id = ? AND w.status IN ('locked', 'done')`, noteID).Scan(&n)
	return n > 0, err
}

// SetScheduleThreshold sets the suggester's minimum voter count.
func (s Queries) SetScheduleThreshold(ctx context.Context, eventID string, n int) error {
	return s.execOne(ctx, "UPDATE events SET schedule_threshold = ? WHERE id = ?", n, eventID)
}

// Voters lists the ids of a note's voters.
func (s Queries) Voters(ctx context.Context, noteID string) ([]string, error) {
	return collect(ctx, s.db, func(r *sql.Rows) (string, error) {
		var id string
		return id, r.Scan(&id)
	}, "SELECT user_id FROM votes WHERE note_id = ? ORDER BY rowid", noteID)
}

// EventVoters maps each note of the event to its voters' ids (votes are
// public, SPEC §11).
func (s Queries) EventVoters(ctx context.Context, eventID string) (map[string][]string, error) {
	type row struct{ note, user string }
	rows, err := collect(ctx, s.db, func(r *sql.Rows) (row, error) {
		var x row
		return x, r.Scan(&x.note, &x.user)
	}, `SELECT v.note_id, v.user_id FROM votes v JOIN notes n ON n.id = v.note_id
	    WHERE n.event_id = ? ORDER BY v.rowid`, eventID)
	m := map[string][]string{}
	for _, x := range rows {
		m[x.note] = append(m[x.note], x.user)
	}
	return m, err
}
