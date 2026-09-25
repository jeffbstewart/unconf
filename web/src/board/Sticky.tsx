import { memo, type PointerEvent } from 'react';
import { NOTE_H, NOTE_W, type Note } from '../api/protocol';
import { initials, snippet } from './text';
import { VoteControl } from './VoteControl';

interface Props {
  note: Note;
  x: number;
  y: number;
  authorName: string;
  chatCount: number;
  regionColor: string | null;
  dragging: boolean;
  /** Fails the personal filters in "dim" mode. */
  dimmed: boolean;
  onPointerDown: (note: Note, e: PointerEvent<HTMLDivElement>) => void;
  onOpen: (noteId: string) => void;
  onStar: (note: Note) => void;
}

export const Sticky = memo(function Sticky({
  note,
  x,
  y,
  authorName,
  chatCount,
  regionColor,
  dragging,
  dimmed,
  onPointerDown,
  onOpen,
  onStar,
}: Props) {
  const classes = ['sticky', `sticky-${note.color}`];
  if (dragging) classes.push('sticky-dragging');
  if (dimmed) classes.push('sticky-dimmed');
  if (note.hidden) classes.push('sticky-hidden');
  return (
    <div
      className={classes.join(' ')}
      style={{ transform: `translate(${x}px, ${y}px)`, width: NOTE_W, height: NOTE_H }}
      onPointerDown={(e) => onPointerDown(note, e)}
      onDoubleClick={(e) => {
        e.stopPropagation();
        onOpen(note.id);
      }}
      onKeyDown={(e) => {
        if (e.key === 'Enter') onOpen(note.id);
      }}
      tabIndex={0}
      role="button"
      aria-label={`Session: ${note.title}`}
      data-note-id={note.id}
    >
      {regionColor && <span className="sticky-region" style={{ background: regionColor }} />}
      <StarButton note={note} onStar={onStar} className="sticky-star" />
      <div className="sticky-title">{note.title}</div>
      <div className="sticky-body">{snippet(note.bodyMd)}</div>
      <div className="sticky-footer">
        <span className="sticky-author" title={authorName}>
          {initials(authorName)}
        </span>
        {note.hidden && <span className="sticky-flag">hidden</span>}
        <span className="sticky-spacer" />
        {chatCount > 0 && (
          <span className="sticky-badge" title={`${chatCount} messages`}>
            💬 {chatCount}
          </span>
        )}
        <VoteControl note={note} compact />
      </div>
    </div>
  );
});

/** Personal bookmark toggle (☆/★); stars are private to their owner. */
export function StarButton({
  note,
  onStar,
  className,
}: {
  note: Note;
  onStar: (note: Note) => void;
  className: string;
}) {
  return (
    <button
      type="button"
      className={`${className}${note.starred ? ' starred' : ''}`}
      aria-pressed={note.starred}
      aria-label={note.starred ? 'Unstar' : 'Star'}
      title={note.starred ? 'Starred (only you see this)' : 'Star (only you see this)'}
      onPointerDown={(e) => e.stopPropagation()}
      onDoubleClick={(e) => e.stopPropagation()}
      onClick={(e) => {
        e.stopPropagation();
        onStar(note);
      }}
    >
      {note.starred ? '★' : '☆'}
    </button>
  );
}
