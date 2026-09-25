package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jeffbstewart/unconf/internal/domain"
)

// Region is a moderator-drawn board area.
type Region struct {
	ID, EventID, Label string
	X, Y, W, H         float64
	Color              string
	Z                  int
	Order              int64 // creation rank (rowid); breaks containment z ties
}

// Message is a chat message on a note.
type Message struct {
	ID, NoteID, AuthorID string
	ThreadID             string // "" = root message
	Body                 string
	HiddenBy, HiddenAt   string
	CreatedAt            string
}

func (m Message) Hidden() bool { return m.HiddenAt != "" }

// collect runs query and scans each row with scan.
func collect[T any](ctx context.Context, q querier, scan func(*sql.Rows) (T, error), query string, args ...any) ([]T, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// countsByNote runs a (note_id, count) query into a map.
func countsByNote(ctx context.Context, q querier, query string, args ...any) (map[string]int, error) {
	type kv struct {
		k string
		v int
	}
	rows, err := collect(ctx, q, func(r *sql.Rows) (kv, error) {
		var x kv
		return x, r.Scan(&x.k, &x.v)
	}, query, args...)
	m := make(map[string]int, len(rows))
	for _, r := range rows {
		m[r.k] = r.v
	}
	return m, err
}

// EventLinks returns every link of an event's notes, grouped by note.
func (s Queries) EventLinks(ctx context.Context, eventID string) (map[string][]domain.Link, error) {
	type row struct {
		noteID string
		l      domain.Link
	}
	rows, err := collect(ctx, s.db, func(r *sql.Rows) (row, error) {
		var x row
		return x, r.Scan(&x.noteID, &x.l.ID, &x.l.Title, &x.l.URL, &x.l.Kind)
	}, `SELECT l.note_id, l.id, l.title, l.url, l.kind FROM note_links l
	    JOIN notes n ON n.id = l.note_id WHERE n.event_id = ? ORDER BY l.rowid`, eventID)
	m := map[string][]domain.Link{}
	for _, r := range rows {
		m[r.noteID] = append(m[r.noteID], r.l)
	}
	return m, err
}

// VoteTotals counts vote dots per note across the event.
func (s Queries) VoteTotals(ctx context.Context, eventID string) (map[string]int, error) {
	return countsByNote(ctx, s.db, `SELECT v.note_id, COUNT(*) FROM votes v
		JOIN notes n ON n.id = v.note_id WHERE n.event_id = ? GROUP BY v.note_id`, eventID)
}

// UserVoted returns the notes a user has voted for.
func (s Queries) UserVoted(ctx context.Context, userID string) (map[string]bool, error) {
	counts, err := countsByNote(ctx, s.db, "SELECT note_id, 1 FROM votes WHERE user_id = ?", userID)
	m := make(map[string]bool, len(counts))
	for k := range counts {
		m[k] = true
	}
	return m, err
}

// UserStars returns the notes a user has starred.
func (s Queries) UserStars(ctx context.Context, userID string) (map[string]bool, error) {
	counts, err := countsByNote(ctx, s.db, "SELECT note_id, 1 FROM stars WHERE user_id = ?", userID)
	m := make(map[string]bool, len(counts))
	for k := range counts {
		m[k] = true
	}
	return m, err
}

const regionColumns = "id, event_id, label, x, y, w, h, color, z, rowid"

func scanRegion(r interface{ Scan(...any) error }) (Region, error) {
	var x Region
	err := r.Scan(&x.ID, &x.EventID, &x.Label, &x.X, &x.Y, &x.W, &x.H, &x.Color, &x.Z, &x.Order)
	if errors.Is(err, sql.ErrNoRows) {
		return Region{}, ErrNotFound
	}
	return x, err
}

// Regions lists an event's regions, oldest first.
func (s Queries) Regions(ctx context.Context, eventID string) ([]Region, error) {
	return collect(ctx, s.db, func(r *sql.Rows) (Region, error) { return scanRegion(r) },
		"SELECT "+regionColumns+" FROM regions WHERE event_id = ? ORDER BY rowid", eventID)
}

// RegionByID loads one region.
func (s Queries) RegionByID(ctx context.Context, id string) (Region, error) {
	return scanRegion(s.db.QueryRowContext(ctx, "SELECT "+regionColumns+" FROM regions WHERE id = ?", id))
}

// InsertRegion stores a new region and returns it with its Order set.
func (s Queries) InsertRegion(ctx context.Context, r Region) (Region, error) {
	res, err := s.db.ExecContext(ctx,
		"INSERT INTO regions (id, event_id, label, x, y, w, h, color, z) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		r.ID, r.EventID, r.Label, r.X, r.Y, r.W, r.H, r.Color, r.Z)
	if err != nil {
		return Region{}, err
	}
	r.Order, err = res.LastInsertId()
	return r, err
}

// UpdateRegion rewrites a region's label, geometry, color, and z.
func (s Queries) UpdateRegion(ctx context.Context, r Region) error {
	return s.execOne(ctx, "UPDATE regions SET label = ?, x = ?, y = ?, w = ?, h = ?, color = ?, z = ? WHERE id = ?",
		r.Label, r.X, r.Y, r.W, r.H, r.Color, r.Z, r.ID)
}

// DeleteRegion removes a region. Notes tagged with it must be retagged first.
func (s Queries) DeleteRegion(ctx context.Context, id string) error {
	return s.execOne(ctx, "DELETE FROM regions WHERE id = ?", id)
}

// Messages lists an event's chat messages, oldest first.
func (s Queries) Messages(ctx context.Context, eventID string) ([]Message, error) {
	return collect(ctx, s.db, func(r *sql.Rows) (Message, error) {
		var x Message
		return x, r.Scan(&x.ID, &x.NoteID, &x.AuthorID, &x.ThreadID, &x.Body, &x.HiddenBy, &x.HiddenAt, &x.CreatedAt)
	}, `SELECT m.id, m.note_id, m.author_id, COALESCE(m.thread_id, ''), m.body,
	           COALESCE(m.hidden_by, ''), COALESCE(m.hidden_at, ''), m.created_at
	    FROM messages m JOIN notes n ON n.id = m.note_id
	    WHERE n.event_id = ? ORDER BY m.created_at, m.rowid`, eventID)
}
