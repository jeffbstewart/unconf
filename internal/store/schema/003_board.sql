-- Sticky notes, links, regions, votes, and personal stars (SPEC §9).

CREATE TABLE regions (
  id       TEXT PRIMARY KEY,
  event_id TEXT NOT NULL REFERENCES events(id),
  label    TEXT NOT NULL,
  x REAL NOT NULL, y REAL NOT NULL, w REAL NOT NULL, h REAL NOT NULL,
  color    TEXT NOT NULL,                          -- muted background tint
  z        INTEGER NOT NULL DEFAULT 0              -- higher wins containment
);

CREATE INDEX regions_event ON regions(event_id);

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

CREATE INDEX notes_event ON notes(event_id);

CREATE TABLE note_links (
  id      TEXT PRIMARY KEY,
  note_id TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  title   TEXT NOT NULL,
  url     TEXT NOT NULL,                           -- http(s) only, validated
  kind    TEXT NOT NULL DEFAULT 'other'            -- doc|slides|other
);

CREATE TABLE votes (
  id       TEXT PRIMARY KEY,
  user_id  TEXT NOT NULL REFERENCES users(id),
  note_id  TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  cast_at  TEXT NOT NULL
);          -- one row per dot; stacking allowed; budget enforced in domain code

CREATE INDEX votes_user ON votes(user_id);
CREATE INDEX votes_note ON votes(note_id);

CREATE TABLE stars (
  user_id TEXT NOT NULL REFERENCES users(id),
  note_id TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  PRIMARY KEY (user_id, note_id)
);          -- personal bookmarks; never exposed to any other user
