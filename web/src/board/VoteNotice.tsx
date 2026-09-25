import { useEffect } from 'react';
import { useStore } from '../store/store';
import { acknowledge, useVoteNotice } from './voteNoticeState';
import './NoteModal.css';

/** Interstitial before a user's first vote: votes are visible to everyone. */
export function VoteNotice() {
  const noteId = useVoteNotice((s) => s.pendingNoteId);
  const close = useVoteNotice((s) => s.close);
  const you = useStore((s) => s.you);
  const title = useStore((s) => (noteId ? s.notes[noteId]?.title : undefined));
  const send = useStore((s) => s.send);

  useEffect(() => {
    if (!noteId) return;
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && close();
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [noteId, close]);

  if (!noteId || !you) return null;
  const confirm = () => {
    acknowledge(you.id);
    close();
    send('cast_vote', { noteId }).catch(() => {});
  };
  return (
    <div className="modal-backdrop" onPointerDown={(e) => e.target === e.currentTarget && close()}>
      <div className="modal vote-notice" role="alertdialog" aria-modal="true" aria-labelledby="vote-notice-title">
        <h2 id="vote-notice-title">Votes are public</h2>
        <p>
          Everyone at this event can see <strong>who</strong> voted for which sessions, not just the totals.
          This helps schedule sessions so that people who want the same topics aren't split across the same
          time slot.
        </p>
        {title && <p className="muted">You're about to vote for “{title}”.</p>}
        <footer className="note-modal-actions">
          <span className="spacer" />
          <button onClick={close}>Cancel</button>
          <button className="primary" onClick={confirm} autoFocus>
            Got it, cast my vote
          </button>
        </footer>
      </div>
    </div>
  );
}
