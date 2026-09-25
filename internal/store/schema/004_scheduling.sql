-- Waves, time slots, rooms, and note assignments (SPEC §4).

CREATE TABLE waves (
  id       TEXT PRIMARY KEY,
  event_id TEXT NOT NULL REFERENCES events(id),
  name     TEXT NOT NULL,
  status   TEXT NOT NULL DEFAULT 'planned',        -- planned|open|locked|done
  opens_at TEXT                                    -- informational display only
);

CREATE INDEX waves_event ON waves(event_id);

CREATE TABLE slots (
  id       TEXT PRIMARY KEY,
  wave_id  TEXT NOT NULL REFERENCES waves(id) ON DELETE CASCADE,
  start_at TEXT NOT NULL,
  end_at   TEXT NOT NULL
);

CREATE INDEX slots_wave ON slots(wave_id);

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

CREATE INDEX assignments_note ON assignments(note_id);
