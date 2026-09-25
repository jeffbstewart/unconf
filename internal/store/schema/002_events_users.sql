-- Events and their participants (SPEC §3, §6).

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
