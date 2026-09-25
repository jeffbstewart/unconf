import { memo, type PointerEvent } from 'react';
import { NOTE_H, NOTE_W, type Note } from '../api/protocol';
import { initials, snippet } from './text';

interface Props {
  note: Note;
  x: number;
  y: number;
  authorName: string;
  chatCount: number;
  regionColor: string | null;
  dragging: boolean;
  onPointerDown: (note: Note, e: PointerEvent<HTMLDivElement>) => void;
  onOpen: (noteId: string) => void;
}

export const Sticky = memo(function Sticky({
  note,
  x,
  y,
  authorName,
  chatCount,
  regionColor,
  dragging,
  onPointerDown,
  onOpen,
}: Props) {
  const classes = ['sticky', `sticky-${note.color}`];
  if (dragging) classes.push('sticky-dragging');
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
        {note.voteTotal > 0 && (
          <span className="sticky-badge" title={`${note.voteTotal} votes`}>
            ● {note.voteTotal}
          </span>
        )}
      </div>
    </div>
  );
});
