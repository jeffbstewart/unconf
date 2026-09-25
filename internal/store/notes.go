package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

	"github.com/jeffbstewart/unconf/internal/domain"
)

// Note is a sticky note row.
type Note struct {
	ID        string
	EventID   string
	AuthorID  string
	Title     string
	BodyMD    string
	X, Y      float64
	Color     string
	RegionID  string // "" = none
	HiddenBy  string // "" = visible
	HiddenAt  string
	CreatedAt string
	UpdatedAt string
}

func (n Note) Hidden() bool { return n.HiddenAt != "" }

const noteColumns = `id, event_id, author_id, title, body_md, x, y, color,
	COALESCE(region_id, ''), COALESCE(hidden_by, ''), COALESCE(hidden_at, ''), created_at, updated_at`

func scanNote(row interface{ Scan(...any) error }) (Note, error) {
	var n Note
	err := row.Scan(&n.ID, &n.EventID, &n.AuthorID, &n.Title, &n.BodyMD, &n.X, &n.Y, &n.Color,
		&n.RegionID, &n.HiddenBy, &n.HiddenAt, &n.CreatedAt, &n.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Note{}, ErrNotFound
	}
	return n, err
}

// NoteByID loads one note, hidden or not.
func (s Queries) NoteByID(ctx context.Context, id string) (Note, error) {
	return scanNote(s.db.QueryRowContext(ctx, "SELECT "+noteColumns+" FROM notes WHERE id = ?", id))
}

// Notes lists all of an event's notes, hidden included, oldest first.
func (s Queries) Notes(ctx context.Context, eventID string) ([]Note, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+noteColumns+" FROM notes WHERE event_id = ? ORDER BY created_at, rowid", eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Note
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// InsertNote stores a new note.
func (s Queries) InsertNote(ctx context.Context, n Note) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO notes (id, event_id, author_id, title, body_md, x, y, color, region_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.ID, n.EventID, n.AuthorID, n.Title, n.BodyMD, n.X, n.Y, n.Color, nullIfEmpty(n.RegionID), n.CreatedAt, n.UpdatedAt)
	return err
}

// UpdateNoteContent replaces a note's title, body, and color.
func (s Queries) UpdateNoteContent(ctx context.Context, id, title, body, color, updatedAt string) error {
	return s.execOne(ctx, "UPDATE notes SET title = ?, body_md = ?, color = ?, updated_at = ? WHERE id = ?",
		title, body, color, updatedAt, id)
}

// MoveNote sets a note's position.
func (s Queries) MoveNote(ctx context.Context, id string, x, y float64) error {
	return s.execOne(ctx, "UPDATE notes SET x = ?, y = ? WHERE id = ?", x, y, id)
}

// DeleteNote removes a note; links, votes, stars, assignments, and messages
// go with it (ON DELETE CASCADE).
func (s Queries) DeleteNote(ctx context.Context, id string) error {
	return s.execOne(ctx, "DELETE FROM notes WHERE id = ?", id)
}

// SetNoteRegion tags a note with a region ("" clears the tag).
func (s Queries) SetNoteRegion(ctx context.Context, id, regionID string) error {
	return s.execOne(ctx, "UPDATE notes SET region_id = ? WHERE id = ?", nullIfEmpty(regionID), id)
}

// StarNote bookmarks a note for a user; it reports whether anything changed.
func (s Queries) StarNote(ctx context.Context, userID, noteID string) (bool, error) {
	res, err := s.db.ExecContext(ctx, "INSERT INTO stars (user_id, note_id) VALUES (?, ?) ON CONFLICT DO NOTHING", userID, noteID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// UnstarNote removes a user's bookmark; it reports whether anything changed.
func (s Queries) UnstarNote(ctx context.Context, userID, noteID string) (bool, error) {
	res, err := s.db.ExecContext(ctx, "DELETE FROM stars WHERE user_id = ? AND note_id = ?", userID, noteID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// HideNote marks a note hidden by a moderator.
func (s Queries) HideNote(ctx context.Context, id, byUserID, at string) error {
	return s.execOne(ctx, "UPDATE notes SET hidden_by = ?, hidden_at = ? WHERE id = ?", byUserID, at, id)
}

// NoteFacts gathers what authorization needs about a note.
func (s Queries) NoteFacts(ctx context.Context, n Note) (domain.NoteFacts, error) {
	f := domain.NoteFacts{AuthorID: n.AuthorID, Hidden: n.Hidden()}
	err := s.db.QueryRowContext(ctx,
		`SELECT (SELECT COUNT(*) FROM votes WHERE note_id = ?), (SELECT COUNT(*) FROM assignments WHERE note_id = ?)`,
		n.ID, n.ID).Scan(&f.Votes, &f.Assignments)
	return f, err
}

// NoteLinks lists a note's links in their stored order.
func (s Queries) NoteLinks(ctx context.Context, noteID string) ([]domain.Link, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, title, url, kind FROM note_links WHERE note_id = ? ORDER BY rowid", noteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Link{}
	for rows.Next() {
		var l domain.Link
		if err := rows.Scan(&l.ID, &l.Title, &l.URL, &l.Kind); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ReplaceLinks swaps a note's links for links (whose IDs must be set).
func (s Queries) ReplaceLinks(ctx context.Context, noteID string, links []domain.Link) error {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM note_links WHERE note_id = ?", noteID); err != nil {
		return err
	}
	for _, l := range links {
		if _, err := s.db.ExecContext(ctx,
			"INSERT INTO note_links (id, note_id, title, url, kind) VALUES (?, ?, ?, ?, ?)",
			l.ID, noteID, l.Title, l.URL, l.Kind); err != nil {
			return err
		}
	}
	return nil
}

// SetLifecycle changes an event's lifecycle.
func (s Queries) SetLifecycle(ctx context.Context, eventID string, lc domain.Lifecycle) error {
	return s.execOne(ctx, "UPDATE events SET lifecycle = ? WHERE id = ?", string(lc), eventID)
}

// Users lists an event's users in join order.
func (s Queries) Users(ctx context.Context, eventID string) ([]User, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+userColumns+" FROM users WHERE event_id = ? ORDER BY created_at, rowid", eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

const metaSeq = "seq"

// Seq returns the last assigned event sequence number (0 if none).
func (s Queries) Seq(ctx context.Context) (int64, error) {
	v, err := s.Meta(ctx, metaSeq)
	if errors.Is(err, ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(v, 10, 64)
}

// SetSeq records the last assigned event sequence number.
func (s Queries) SetSeq(ctx context.Context, seq int64) error {
	return s.SetMeta(ctx, metaSeq, strconv.FormatInt(seq, 10))
}
