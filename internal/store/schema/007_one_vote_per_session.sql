-- One vote per person per session (decided 2026-09-25): a vote means "I want
-- to attend", which is what conflict-aware scheduling needs. Collapse any
-- stacked dots from earlier builds, then enforce uniqueness.

DELETE FROM votes
WHERE rowid NOT IN (SELECT MIN(rowid) FROM votes GROUP BY user_id, note_id);

CREATE UNIQUE INDEX votes_user_note ON votes(user_id, note_id);
