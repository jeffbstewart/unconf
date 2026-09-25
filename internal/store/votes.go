package store

import "context"

// CastVote records a user's vote for a note (one per person per note).
// It reports false if the user had already voted for it.
func (s Queries) CastVote(ctx context.Context, id, userID, noteID, at string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		"INSERT INTO votes (id, user_id, note_id, cast_at) VALUES (?, ?, ?, ?) ON CONFLICT (user_id, note_id) DO NOTHING",
		id, userID, noteID, at)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// HasVoted reports whether a user has voted for a note.
func (s Queries) HasVoted(ctx context.Context, userID, noteID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM votes WHERE user_id = ? AND note_id = ?", userID, noteID).Scan(&n)
	return n > 0, err
}

// RetractVote removes a user's vote for a note. It reports false if there
// was none.
func (s Queries) RetractVote(ctx context.Context, userID, noteID string) (bool, error) {
	res, err := s.db.ExecContext(ctx, "DELETE FROM votes WHERE user_id = ? AND note_id = ?", userID, noteID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// NoteVoteTotal counts a note's voters.
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
