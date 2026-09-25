import type { Note } from '../api/protocol';
import { votesRemaining } from '../store/reducer';
import { canInteract, useStore } from '../store/store';
import { inClosedWave } from '../schedule/logic';
import { hasAcknowledged, useVoteNotice } from './voteNoticeState';

interface Props {
  note: Note;
  /** Smaller form for stickies. */
  compact?: boolean;
}

/**
 * The note's voter count, as a toggle for your own vote (one per person per
 * session) while voting is open; a plain tally otherwise.
 */
export function VoteControl({ note, compact }: Props) {
  const open = useStore((s) => !!s.event?.votingOpen);
  const interactive = useStore(canInteract);
  const remaining = useStore(votesRemaining);
  const you = useStore((s) => s.you);
  const send = useStore((s) => s.send);
  const askNotice = useVoteNotice((s) => s.ask);
  const history = useStore((s) => inClosedWave(note.id, s.waves, s.assignments));
  const canVote = open && interactive && !note.hidden && !history;

  const count = `${note.voteTotal} vote${note.voteTotal === 1 ? '' : 's'}`;
  const classes = `votes${compact ? ' votes-compact' : ''}${note.voted ? ' votes-mine' : ''}`;
  const stop = (e: React.SyntheticEvent) => e.stopPropagation();

  if (!canVote) {
    if (note.voteTotal === 0 && !note.voted) return null;
    return (
      <span
        className={classes}
        title={count + (note.voted ? ', including yours' : '') + (history ? ' (scheduled — votes are final)' : '')}
      >
        <span className="vote-count">▲ {note.voteTotal}</span>
      </span>
    );
  }

  const outOfVotes = !note.voted && remaining === 0;
  const toggle = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (note.voted) {
      send('retract_vote', { noteId: note.id }).catch(() => {});
      return;
    }
    // Before a user's first vote, explain that votes are public; nothing is
    // sent until they confirm.
    if (you && !hasAcknowledged(you.id)) {
      askNotice(note.id);
      return;
    }
    send('cast_vote', { noteId: note.id }).catch(() => {});
  };
  const label = note.voted ? `Remove your vote (${count})` : outOfVotes ? `No votes left (${count})` : `Vote (${count}, ${remaining} left)`;

  return (
    <span className={classes} onPointerDown={stop} onDoubleClick={stop}>
      <button
        type="button"
        className="vote-toggle"
        aria-pressed={note.voted}
        aria-label={label}
        title={label}
        disabled={outOfVotes}
        onClick={toggle}
      >
        ▲ {note.voteTotal}
      </button>
    </span>
  );
}
