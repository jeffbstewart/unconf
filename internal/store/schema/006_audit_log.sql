-- Privileged actions, visible to moderators and organizers.

CREATE TABLE audit_log (
  id       TEXT PRIMARY KEY,
  event_id TEXT NOT NULL,
  actor_id TEXT NOT NULL,
  action   TEXT NOT NULL,     -- e.g. hide_note, unhide_message, wave_locked, role_changed
  target   TEXT NOT NULL,     -- id of the affected entity
  detail   TEXT,              -- optional JSON
  at       TEXT NOT NULL
);

CREATE INDEX audit_log_event ON audit_log(event_id, at);
