package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jeffbstewart/unconf/internal/domain"
)

// Event is an unconference instance.
type Event struct {
	ID           string
	Name         string
	Lifecycle    string
	VotingOpen   bool
	VotesPerUser int
	CreatedAt    string
}

// User is a participant identity within one event.
type User struct {
	ID        string
	EventID   string
	Name      string
	Email     string // "" when not given
	Role      domain.Role
	CreatedAt string
}

const metaDefaultEvent = "default_event_id"

// EnsureDefaultEvent returns the event recorded in meta as the default,
// creating it with the given name on first run.
func (s Queries) EnsureDefaultEvent(ctx context.Context, name string) (Event, error) {
	id, err := s.MetaOrInit(ctx, metaDefaultEvent, domain.NewID)
	if err != nil {
		return Event{}, err
	}
	if _, err := s.db.ExecContext(ctx,
		"INSERT INTO events (id, name, created_at) VALUES (?, ?, ?) ON CONFLICT (id) DO NOTHING",
		id, name, domain.Timestamp(time.Now())); err != nil {
		return Event{}, err
	}
	return s.Event(ctx, id)
}

// Event loads an event by id.
func (s Queries) Event(ctx context.Context, id string) (Event, error) {
	var e Event
	err := s.db.QueryRowContext(ctx,
		"SELECT id, name, lifecycle, voting_open, votes_per_user, created_at FROM events WHERE id = ?", id).
		Scan(&e.ID, &e.Name, &e.Lifecycle, &e.VotingOpen, &e.VotesPerUser, &e.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Event{}, ErrNotFound
	}
	return e, err
}

const userColumns = "id, event_id, name, COALESCE(email, ''), role, created_at"

func scanUser(row interface{ Scan(...any) error }) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.EventID, &u.Name, &u.Email, &u.Role, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

// UserByID loads a user by id.
func (s Queries) UserByID(ctx context.Context, id string) (User, error) {
	return scanUser(s.db.QueryRowContext(ctx, "SELECT "+userColumns+" FROM users WHERE id = ?", id))
}

// UserByName loads a user by case-insensitive name within an event.
func (s Queries) UserByName(ctx context.Context, eventID, name string) (User, error) {
	return scanUser(s.db.QueryRowContext(ctx,
		"SELECT "+userColumns+" FROM users WHERE event_id = ? AND name_key = ?",
		eventID, domain.NameKey(name)))
}

// CreateUser inserts u. It returns ErrConflict if the name is already
// taken (case-insensitively) in the event.
func (s Queries) CreateUser(ctx context.Context, u User) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO users (id, event_id, name, name_key, email, role, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		u.ID, u.EventID, u.Name, domain.NameKey(u.Name), nullIfEmpty(u.Email), string(u.Role), u.CreatedAt)
	if isUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// SetUserRole changes a user's role.
func (s Queries) SetUserRole(ctx context.Context, id string, role domain.Role) error {
	return s.execOne(ctx, "UPDATE users SET role = ? WHERE id = ?", string(role), id)
}

// SetUserEmail changes a user's email ("" clears it).
func (s Queries) SetUserEmail(ctx context.Context, id, email string) error {
	return s.execOne(ctx, "UPDATE users SET email = ? WHERE id = ?", nullIfEmpty(email), id)
}

// VotesCast counts the votes a user has cast.
func (s Queries) VotesCast(ctx context.Context, userID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM votes WHERE user_id = ?", userID).Scan(&n)
	return n, err
}

// execOne runs an UPDATE/DELETE expected to touch exactly one row.
func (s Queries) execOne(ctx context.Context, query string, args ...any) error {
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	return nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
