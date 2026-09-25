package store

import (
	"context"
	"time"

	"github.com/jeffbstewart/unconf/internal/domain"
)

// AuditEntry records a privileged action (SPEC §7 audit_log).
type AuditEntry struct {
	ID      string // assigned by AppendAudit when empty
	EventID string
	ActorID string
	Action  string // e.g. hide_note, wave_locked, role_changed
	Target  string // id of the affected entity
	Detail  string // optional JSON; "" stores NULL
	At      string // assigned by AppendAudit when empty
}

// AppendAudit inserts an audit log entry.
func (s *Store) AppendAudit(ctx context.Context, e AuditEntry) error {
	if e.ID == "" {
		e.ID = domain.NewID()
	}
	if e.At == "" {
		e.At = domain.Timestamp(time.Now())
	}
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO audit_log (id, event_id, actor_id, action, target, detail, at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		e.ID, e.EventID, e.ActorID, e.Action, e.Target, nullIfEmpty(e.Detail), e.At)
	return err
}

// AuditLog returns an event's audit entries, oldest first.
func (s *Store) AuditLog(ctx context.Context, eventID string) ([]AuditEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, event_id, actor_id, action, target, COALESCE(detail, ''), at FROM audit_log WHERE event_id = ? ORDER BY at, rowid",
		eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.EventID, &e.ActorID, &e.Action, &e.Target, &e.Detail, &e.At); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
