package store

import "context"

// CastVote records one vote dot. Stacking several dots on a note is allowed.
func (s Queries) CastVote(ctx context.Context, id, userID, noteID, at string) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO votes (id, user_id, note_id, cast_at) VALUES (?, ?, ?, ?)", id, userID, noteID, at)
	return err
}

// RetractVote removes the user's most recent dot on a note. It reports
// false if the user had none there.
func (s Queries) RetractVote(ctx context.Context, userID, noteID string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM votes WHERE rowid = (
		SELECT rowid FROM votes WHERE user_id = ? AND note_id = ? ORDER BY rowid DESC LIMIT 1)`, userID, noteID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// NoteVoteTotal counts every dot on a note.
func (s Queries) NoteVoteTotal(ctx context.Context, noteID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM votes WHERE note_id = ?", noteID).Scan(&n)
	return n, err
}

// SetVotingOpen opens or closes voting for an event.
func (s Queries) SetVotingOpen(ctx context.Context, eventID string, open bool) error {
	return s.execOne(ctx, "UPDATE events SET voting_open = ? WHERE id = ?", open, eventID)
}

// SetVotesPerUser changes an event's per-user vote budget.
func (s Queries) SetVotesPerUser(ctx context.Context, eventID string, n int) error {
	return s.execOne(ctx, "UPDATE events SET votes_per_user = ? WHERE id = ?", n, eventID)
}
