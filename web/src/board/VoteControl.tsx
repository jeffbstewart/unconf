import type { Note } from '../api/protocol';
import { votesRemaining } from '../store/reducer';
import { canInteract, useStore } from '../store/store';

interface Props {
  note: Note;
  /** Compact form for stickies: −/+ appear on hover. */
  compact?: boolean;
}

/** Dot tally with cast (+) and retract (−) buttons while voting is open. */
export function VoteControl({ note, compact }: Props) {
  const open = useStore((s) => !!s.event?.votingOpen);
  const interactive = useStore(canInteract);
  const remaining = useStore(votesRemaining);
  const send = useStore((s) => s.send);
  const canVote = open && interactive && !note.hidden;

  const title =
    `${note.voteTotal} vote${note.voteTotal === 1 ? '' : 's'}` + (note.myVotes ? ` (${note.myVotes} yours)` : '');
  const vote = (cmd: 'cast_vote' | 'retract_vote') => (e: React.MouseEvent) => {
    e.stopPropagation();
    send(cmd, { noteId: note.id }).catch(() => {});
  };
  const stop = (e: React.SyntheticEvent) => e.stopPropagation();

  if (!canVote && note.voteTotal === 0) return null;
  return (
    <span
      className={`votes${compact ? ' votes-compact' : ''}${note.myVotes ? ' votes-mine' : ''}`}
      onPointerDown={stop}
      onDoubleClick={stop}
    >
      {canVote && (
        <button
          type="button"
          className="vote-btn"
          aria-label="Retract a vote"
          title="Retract a vote"
          disabled={note.myVotes === 0}
          onClick={vote('retract_vote')}
        >
          −
        </button>
      )}
      <span className="vote-count" title={title} aria-label={title}>
        ● {note.voteTotal}
      </span>
      {canVote && (
        <button
          type="button"
          className="vote-btn"
          aria-label="Cast a vote"
          title={remaining > 0 ? `Vote (${remaining} left)` : 'No votes left'}
          disabled={remaining === 0}
          onClick={vote('cast_vote')}
        >
          +
        </button>
      )}
    </span>
  );
}
