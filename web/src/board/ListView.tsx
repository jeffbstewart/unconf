import type { Note } from '../api/protocol';
import { useStore } from '../store/store';
import { groupByRegion, matches, sortNotes, useView } from '../store/view';
import { StarButton } from './Sticky';
import { VoteControl } from './VoteControl';
import './ListView.css';

/** The board as a list: grouped by region, ordered by the personal sort. */
export function ListView({ onOpen }: { onOpen: (noteId: string) => void }) {
  const notes = useStore((s) => s.notes);
  const regions = useStore((s) => s.regions);
  const users = useStore((s) => s.users);
  const you = useStore((s) => s.you);
  const send = useStore((s) => s.send);
  const { filters, mode, sort } = useView();

  const shown = Object.values(notes).filter((n) => mode === 'dim' || matches(n, filters, you?.id));
  const groups = groupByRegion(sortNotes(shown, sort, you?.id), regions);
  const star = (n: Note) => send(n.starred ? 'unstar_note' : 'star_note', { noteId: n.id }).catch(() => {});

  if (groups.length === 0) {
    return (
      <main className="list-view">
        <p className="muted list-empty">{Object.keys(notes).length ? 'No notes match your filters.' : 'No sessions proposed yet.'}</p>
      </main>
    );
  }
  return (
    <main className="list-view">
      {groups.map((g) => (
        <section key={g.region?.id ?? 'none'} className="list-group" aria-label={g.region?.label ?? 'No region'}>
          <h2>
            <span className="swatch-dot" style={g.region ? { background: g.region.color } : undefined} />
            {g.region?.label ?? 'No region'}
            <span className="list-count">{g.notes.length}</span>
          </h2>
          <ol>
            {g.notes.map((n) => (
              <li
                key={n.id}
                className={`list-row${matches(n, filters, you?.id) ? '' : ' list-row-dimmed'}${n.hidden ? ' list-row-hidden' : ''}`}
                data-note-id={n.id}
              >
                <StarButton note={n} onStar={star} className="list-star" />
                <span className={`list-color sticky-${n.color}`} />
                <button className="list-title" onClick={() => onOpen(n.id)}>
                  {n.title}
                </button>
                <span className="list-meta">{users[n.authorId]?.name ?? '?'}</span>
                {n.hidden && <span className="sticky-flag">hidden</span>}
                {n.scheduled && <span className="list-badge">scheduled</span>}
                <VoteControl note={n} />
              </li>
            ))}
          </ol>
        </section>
      ))}
    </main>
  );
}
