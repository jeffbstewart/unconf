-- Initial schema (SPEC §7).

CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);

CREATE TABLE events (
  id             TEXT PRIMARY KEY,
  name           TEXT NOT NULL,
  lifecycle      TEXT NOT NULL DEFAULT 'setup',   -- setup|active|done
  voting_open    INTEGER NOT NULL DEFAULT 0,
  votes_per_user INTEGER NOT NULL DEFAULT 5,
  created_at     TEXT NOT NULL
);

CREATE TABLE users (
  id         TEXT PRIMARY KEY,
  event_id   TEXT NOT NULL REFERENCES events(id),
  name       TEXT NOT NULL,
  name_key   TEXT NOT NULL,                        -- lower(name), for uniqueness
  email      TEXT,
  role       TEXT NOT NULL DEFAULT 'participant',  -- participant|moderator|organizer
  created_at TEXT NOT NULL,
  UNIQUE (event_id, name_key)
);

CREATE TABLE notes (
  id         TEXT PRIMARY KEY,
  event_id   TEXT NOT NULL REFERENCES events(id),
  author_id  TEXT NOT NULL REFERENCES users(id),
  title      TEXT NOT NULL,                        -- 1..120 chars
  body_md    TEXT NOT NULL DEFAULT '',             -- markdown, ≤ 20000 chars
  x REAL NOT NULL, y REAL NOT NULL,                -- board coords, unbounded
  color      TEXT NOT NULL DEFAULT 'yellow',       -- yellow|pink|blue|green|orange|purple
  region_id  TEXT REFERENCES regions(id),          -- derived (§9), nullable
  hidden_by  TEXT REFERENCES users(id),            -- moderation
  hidden_at  TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE note_links (
  id      TEXT PRIMARY KEY,
  note_id TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  title   TEXT NOT NULL,
  url     TEXT NOT NULL,                           -- http(s) only, validated
  kind    TEXT NOT NULL DEFAULT 'other'            -- doc|slides|other
);

CREATE TABLE regions (
  id       TEXT PRIMARY KEY,
  event_id TEXT NOT NULL REFERENCES events(id),
  label    TEXT NOT NULL,
  x REAL NOT NULL, y REAL NOT NULL, w REAL NOT NULL, h REAL NOT NULL,
  color    TEXT NOT NULL,                          -- muted background tint
  z        INTEGER NOT NULL DEFAULT 0              -- higher wins containment
);

CREATE TABLE votes (
  id       TEXT PRIMARY KEY,
  user_id  TEXT NOT NULL REFERENCES users(id),
  note_id  TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  cast_at  TEXT NOT NULL
);          -- one row per dot; stacking allowed; budget enforced in domain code

CREATE TABLE stars (
  user_id TEXT NOT NULL REFERENCES users(id),
  note_id TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  PRIMARY KEY (user_id, note_id)
);          -- personal bookmarks; never exposed to any other user

CREATE TABLE waves (
  id       TEXT PRIMARY KEY,
  event_id TEXT NOT NULL REFERENCES events(id),
  name     TEXT NOT NULL,
  status   TEXT NOT NULL DEFAULT 'planned',        -- planned|open|locked|done
  opens_at TEXT                                    -- informational display only
);

CREATE TABLE slots (
  id       TEXT PRIMARY KEY,
  wave_id  TEXT NOT NULL REFERENCES waves(id) ON DELETE CASCADE,
  start_at TEXT NOT NULL,
  end_at   TEXT NOT NULL
);

CREATE TABLE rooms (
  id       TEXT PRIMARY KEY,
  event_id TEXT NOT NULL REFERENCES events(id),
  name     TEXT NOT NULL,
  meet_url TEXT                                    -- filled by CalendarService later
);

CREATE TABLE assignments (
  id      TEXT PRIMARY KEY,
  note_id TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  slot_id TEXT NOT NULL REFERENCES slots(id) ON DELETE CASCADE,
  room_id TEXT NOT NULL REFERENCES rooms(id),
  UNIQUE (slot_id, room_id)
);          -- plus domain rule: at most one assignment per (note, wave)

CREATE TABLE messages (
  id         TEXT PRIMARY KEY,
  note_id    TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  author_id  TEXT NOT NULL REFERENCES users(id),
  thread_id  TEXT,            -- null = root message; else id of the root message
  body       TEXT NOT NULL,   -- plain text, ≤ 2000 chars
  hidden_by  TEXT REFERENCES users(id),
  hidden_at  TEXT,
  created_at TEXT NOT NULL
);

CREATE TABLE audit_log (
  id       TEXT PRIMARY KEY,
  event_id TEXT NOT NULL,
  actor_id TEXT NOT NULL,
  action   TEXT NOT NULL,     -- e.g. hide_note, unhide_message, wave_locked, role_changed
  target   TEXT NOT NULL,     -- id of the affected entity
  detail   TEXT,              -- optional JSON
  at       TEXT NOT NULL
);

-- Lookup indexes (not part of the logical schema).
CREATE INDEX notes_event       ON notes(event_id);
CREATE INDEX regions_event     ON regions(event_id);
CREATE INDEX votes_user        ON votes(user_id);
CREATE INDEX votes_note        ON votes(note_id);
CREATE INDEX waves_event       ON waves(event_id);
CREATE INDEX slots_wave        ON slots(wave_id);
CREATE INDEX assignments_note  ON assignments(note_id);
CREATE INDEX messages_note     ON messages(note_id);
CREATE INDEX audit_log_event   ON audit_log(event_id, at);
