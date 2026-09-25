import type { Assignment, Note } from '../api/protocol';
import { StarButton } from '../board/Sticky';
import { useStore } from '../store/store';
import { pickOf } from './logic';

interface Props {
  note: Note;
  assignment?: Assignment;
  onOpen: (noteId: string) => void;
  compact?: boolean;
  /** Extra class names, e.g. conflict highlighting. */
  className?: string;
  title?: string;
}

/** A scheduled session: title, proposer, voters, your pick markers, Join. */
export function SessionCard({ note, assignment, onOpen, compact, className, title }: Props) {
  const you = useStore((s) => s.you);
  const author = useStore((s) => s.users[note.authorId]);
  const send = useStore((s) => s.send);
  const pick = pickOf(note, you?.id);
  const classes = ['session', `sticky-${note.color}`];
  if (pick.voted || pick.proposed || pick.starred) classes.push('session-pick');
  if (compact) classes.push('session-compact');
  if (className) classes.push(className);
  return (
    <div className={classes.join(' ')} title={title} data-note-id={note.id}>
      <div className="session-head">
        <button className="session-title" onClick={() => onOpen(note.id)}>
          {note.title}
        </button>
        <StarButton
          note={note}
          className="session-star"
          onStar={(n) => send(n.starred ? 'unstar_note' : 'star_note', { noteId: n.id }).catch(() => {})}
        />
      </div>
      <div className="session-meta">
        {assignment && <span className="session-track">Track {assignment.track}</span>}
        <span>{author?.name ?? '?'}</span>
        <span title={`${note.voteTotal} voters`}>▲ {note.voteTotal}</span>
        {pick.proposed && <span className="pick pick-proposed">You're facilitating</span>}
        {pick.voted && !pick.proposed && <span className="pick pick-voted">✓ Your vote</span>}
        {assignment?.meetUrl && (
          <a className="join" href={assignment.meetUrl} target="_blank" rel="noopener noreferrer">
            Join
          </a>
        )}
      </div>
    </div>
  );
}
