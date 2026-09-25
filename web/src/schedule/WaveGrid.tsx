import { useMemo, useState, type DragEvent } from 'react';
import type { Wave } from '../api/protocol';
import { useStore } from '../store/store';
import { myClash, pool, slotConflicts, slotLabel, slotSessions } from './logic';
import { SessionCard } from './SessionCard';

const DND = 'application/x-unconf-session';

interface Dragged {
  noteId: string;
  /** Set when dragging an already-placed session. */
  assignmentId?: string;
}

interface Props {
  wave: Wave;
  /** Scheduler editing (wave open): drag/drop, click-to-place, pool rail. */
  editable: boolean;
  onOpen: (noteId: string) => void;
}

/** A wave's slot × track grid; schedulers also get the pool and overlays. */
export function WaveGrid({ wave, editable, onOpen }: Props) {
  const notes = useStore((s) => s.notes);
  const assignments = useStore((s) => s.assignments);
  const waves = useStore((s) => s.waves);
  const users = useStore((s) => s.users);
  const you = useStore((s) => s.you);
  const isScheduler = !!you && you.role !== 'participant';
  const threshold = useStore((s) => s.event?.scheduleThreshold ?? 2);
  const send = useStore((s) => s.send);
  const [selected, setSelected] = useState<Dragged | null>(null);
  const [showBelow, setShowBelow] = useState(false);

  const tracks = Array.from({ length: wave.tracks }, (_, i) => i + 1);
  const p = useMemo(() => pool(notes, assignments, waves, wave.id, threshold), [notes, assignments, waves, wave.id, threshold]);
  const title = (id: string) => notes[id]?.title ?? '?';

  const place = (d: Dragged, slotId: string, track: number) => {
    setSelected(null);
    send('assign_note', { noteId: d.noteId, slotId, track }).catch(() => {});
  };
  const unplace = (d: Dragged) => {
    setSelected(null);
    if (d.assignmentId) send('unassign_note', { assignmentId: d.assignmentId }).catch(() => {});
  };
  const readDrag = (e: DragEvent): Dragged | null => {
    try {
      return JSON.parse(e.dataTransfer.getData(DND)) as Dragged;
    } catch {
      return null;
    }
  };
  const dragProps = (d: Dragged) =>
    editable
      ? {
          draggable: true,
          onDragStart: (e: DragEvent) => {
            e.dataTransfer.setData(DND, JSON.stringify(d));
            e.dataTransfer.effectAllowed = 'move';
          },
        }
      : {};

  const poolItem = (id: string, warn?: string) => {
    const n = notes[id];
    const on = selected?.noteId === id && !selected.assignmentId;
    return (
      <li key={id}>
        <button
          className={`pool-item sticky-${n.color}${on ? ' selected' : ''}`}
          aria-pressed={on}
          title={warn ?? 'Drag into a cell, or click then click an empty cell'}
          onClick={() => setSelected(on ? null : { noteId: id })}
          {...dragProps({ noteId: id })}
        >
          <span className="pool-title">{n.title}</span>
          <span className="pool-meta">
            ▲ {n.voteTotal} · {users[n.authorId]?.name ?? '?'}
            {warn && ' · ⚠ repeat'}
          </span>
        </button>
      </li>
    );
  };

  return (
    <div className={`wave-layout${editable ? ' with-pool' : ''}`}>
      {editable && (
        <aside
          className="pool"
          aria-label="Unscheduled sessions"
          onDragOver={(e) => e.dataTransfer.types.includes(DND) && e.preventDefault()}
          onDrop={(e) => {
            const d = readDrag(e);
            if (d) unplace(d);
          }}
        >
          <h2>Unscheduled</h2>
          <p className="muted pool-hint">
            {p.eligible.length} with ≥ {threshold} votes · drag into a cell (or click, then click a cell)
          </p>
          <ol className="pool-list">{p.eligible.map((n) => poolItem(n.id))}</ol>
          {p.below.length > 0 && (
            <>
              <button className="pool-divider" onClick={() => setShowBelow((v) => !v)} aria-expanded={showBelow}>
                {showBelow ? '▾' : '▸'} {p.below.length} below {threshold} votes
              </button>
              {showBelow && <ol className="pool-list">{p.below.map((n) => poolItem(n.id))}</ol>}
            </>
          )}
          {p.repeats.length > 0 && (
            <>
              <h3>Already run</h3>
              <ol className="pool-list">{p.repeats.map((n) => poolItem(n.id, 'Already ran in an earlier wave'))}</ol>
            </>
          )}
        </aside>
      )}
      <div className="grid-wrap">
        {wave.slots.length === 0 ? (
          <p className="app-message">{isScheduler ? 'Add time slots above.' : 'This wave has no time slots yet.'}</p>
        ) : (
          <table className="wave-grid">
            <thead>
              <tr>
                <th scope="col">Time</th>
                {tracks.map((t) => (
                  <th key={t} scope="col">
                    Track {t}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {wave.slots.map((slot) => {
                const sessions = slotSessions(slot.id, assignments, notes);
                const conflicts = slotConflicts(sessions);
                const clash = myClash(sessions, you?.id);
                return (
                  <tr key={slot.id}>
                    <th scope="row" className="slot-label">
                      <div>{slotLabel(slot)}</div>
                      <div className="slot-day muted">
                        {new Date(slot.startAt).toLocaleDateString([], { weekday: 'short', month: 'short', day: 'numeric' })}
                      </div>
                      {isScheduler && sessions.length > 1 && (
                        <div
                          className={`slot-conflicts${conflicts.count ? ' has' : ''}`}
                          title="People interested in more than one session in this slot (voters and proposers)"
                        >
                          {conflicts.count ? `⚠ ${conflicts.count} conflict${conflicts.count === 1 ? '' : 's'}` : '✓ no conflicts'}
                        </div>
                      )}
                      {clash.length > 0 && (
                        <div className="slot-clash" title={clash.map((n) => n.title).join(', ')}>
                          {clash.length} of your picks
                          {clash.some((n) => n.authorId === you?.id) ? ' — you’re facilitating one' : ''}
                        </div>
                      )}
                    </th>
                    {tracks.map((track) => {
                      const a = assignments.find((x) => x.slotId === slot.id && x.track === track);
                      const n = a && notes[a.noteId];
                      if (!a || !n) {
                        return (
                          <td
                            key={track}
                            className={`cell empty${editable && selected ? ' targetable' : ''}`}
                            onDragOver={editable ? (e) => e.dataTransfer.types.includes(DND) && e.preventDefault() : undefined}
                            onDrop={
                              editable
                                ? (e) => {
                                    e.preventDefault();
                                    const d = readDrag(e);
                                    if (d) place(d, slot.id, track);
                                  }
                                : undefined
                            }
                            onClick={editable && selected ? () => place(selected, slot.id, track) : undefined}
                            aria-label={`${slotLabel(slot)}, track ${track}: empty`}
                          />
                        );
                      }
                      const overlaps = conflicts.overlaps[n.id] ?? [];
                      const hint = isScheduler
                        ? [
                            conflicts.proposerClash.has(n.id) ? 'Proposer has another session in this slot!' : '',
                            ...overlaps.map((o) => `${o.shared} ${o.shared === 1 ? 'person' : 'people'} also want “${title(o.noteId)}”`),
                          ]
                            .filter(Boolean)
                            .join('\n')
                        : undefined;
                      const d = { noteId: n.id, assignmentId: a.id };
                      const on = selected?.assignmentId === a.id;
                      return (
                        <td key={track} className={`cell${on ? ' selected' : ''}`} {...dragProps(d)}>
                          <SessionCard
                            note={n}
                            assignment={a}
                            onOpen={onOpen}
                            title={hint || undefined}
                            className={[
                              isScheduler && conflicts.proposerClash.has(n.id) ? 'session-clash' : '',
                              isScheduler && overlaps.length ? 'session-overlap' : '',
                            ].join(' ')}
                          />
                          {editable && (
                            <div className="cell-actions">
                              <button onClick={() => setSelected(on ? null : d)} aria-pressed={on} title="Move: click, then click an empty cell">
                                Move
                              </button>
                              <button onClick={() => unplace(d)} title="Back to the pool" aria-label={`Unschedule ${n.title}`}>
                                ✕
                              </button>
                            </div>
                          )}
                        </td>
                      );
                    })}
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
