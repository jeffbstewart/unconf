-- Virtual event: waves are time slots × numbered parallel tracks, and each
-- scheduled session gets its own meeting (SPEC §2, §4). Replaces the
-- event-level rooms from 004. No deployed data exists yet.

DROP TABLE assignments;
DROP TABLE rooms;

CREATE TABLE assignments (
  id       TEXT PRIMARY KEY,
  note_id  TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  slot_id  TEXT NOT NULL REFERENCES slots(id) ON DELETE CASCADE,
  track    INTEGER NOT NULL,                        -- 1..wave.tracks
  meet_url TEXT,                                    -- set by CalendarService after lock
  UNIQUE (slot_id, track)
);          -- plus domain rule: at most one assignment per (note, wave)

CREATE INDEX assignments_note ON assignments(note_id);

ALTER TABLE waves ADD COLUMN tracks INTEGER NOT NULL DEFAULT 1;
ALTER TABLE events ADD COLUMN schedule_threshold INTEGER NOT NULL DEFAULT 2;
