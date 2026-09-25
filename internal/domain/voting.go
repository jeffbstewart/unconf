package domain

const (
	MinVotesPerUser = 1
	MaxVotesPerUser = 20
)

// AuthorizeVote covers cast_vote and retract_vote (SPEC §8.2): anyone who
// can interact with the board, while voting is open, on a visible note.
// Each person has at most one vote per note; the budget and "has voted"
// are checked by the caller.
func AuthorizeVote(a Actor, lc Lifecycle, votingOpen bool, n NoteFacts) *CmdError {
	if err := CheckInteract(a, lc); err != nil {
		return err
	}
	if !votingOpen {
		return NotAllowedNow("voting is closed")
	}
	if n.Hidden {
		return NotAllowedNow("hidden notes cannot be voted on")
	}
	return nil
}

// CheckVoteBudget: a new vote needs a remaining one. Users over budget
// (after an organizer lowers it) keep their votes but cannot add more.
func CheckVoteBudget(used, perUser int) *CmdError {
	if used >= perUser {
		return NotAllowedNow("you have used all %d of your votes", perUser)
	}
	return nil
}

// AuthorizeVotingSettings covers set_voting and set_votes_per_user:
// organizers only, until the event is done.
func AuthorizeVotingSettings(a Actor, lc Lifecycle) *CmdError {
	if !a.Role.AtLeast(RoleOrganizer) {
		return Forbidden("only organizers can change voting")
	}
	if lc == LifecycleDone {
		return NotAllowedNow("the event is over")
	}
	return nil
}

// ValidateVotesPerUser checks a vote budget (1–20).
func ValidateVotesPerUser(n int) *CmdError {
	if n < MinVotesPerUser || n > MaxVotesPerUser {
		return BadRequest("votes per user must be %d–%d", MinVotesPerUser, MaxVotesPerUser)
	}
	return nil
}
