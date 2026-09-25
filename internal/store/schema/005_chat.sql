-- Per-note chat, thread-shaped for later Google Chat mapping.

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

CREATE INDEX messages_note ON messages(note_id);
