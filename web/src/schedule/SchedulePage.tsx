import { useEffect, useMemo, useState } from 'react';
import type { Note, Wave } from '../api/protocol';
import { NoteModal } from '../board/NoteModal';
import { canInteract, useStore } from '../store/store';
import { nowAndNext, slotLabel, slotSessions, slotWaves } from './logic';
import { SessionCard } from './SessionCard';
import { WaveGrid } from './WaveGrid';
import { WaveSetup } from './WaveSetup';
import './Schedule.css';

/** Re-renders every `ms` so "now" stays current. */
function useNow(ms: number): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), ms);
    return () => clearInterval(t);
  }, [ms]);
  return now;
}

/** Which wave to show first: the open one, else the latest locked, else the newest. */
function defaultWave(waves: Wave[]): string | null {
  return (
    waves.find((w) => w.status === 'open')?.id ??
    [...waves].reverse().find((w) => w.status === 'locked')?.id ??
    waves[waves.length - 1]?.id ??
    null
  );
}

export function SchedulePage() {
  const waves = useStore((s) => s.waves);
  const notes = useStore((s) => s.notes);
  const you = useStore((s) => s.you);
  const interactive = useStore(canInteract);
  const isScheduler = !!you && you.role !== 'participant' && interactive;
  const [picked, setPicked] = useState<string | null>(null);
  const [openNoteId, setOpenNoteId] = useState<string | null>(null);
  const [newWave, setNewWave] = useState(false);
  const now = useNow(30_000);

  const current = picked && waves.some((w) => w.id === picked) ? picked : defaultWave(waves);
  const wave = waves.find((w) => w.id === current);

  useEffect(() => {
    if (openNoteId && !notes[openNoteId]) setOpenNoteId(null);
  }, [notes, openNoteId]);

  return (
    <main className="schedule">
      <NowNext now={now} onOpen={setOpenNoteId} />
      <nav className="wave-tabs" aria-label="Waves">
        {waves.map((w) => (
          <button
            key={w.id}
            className={`wave-tab${w.id === current ? ' on' : ''}`}
            aria-pressed={w.id === current}
            onClick={() => setPicked(w.id)}
          >
            {w.name} <span className={`wave-status wave-${w.status}`}>{w.status === 'open' ? 'draft' : w.status}</span>
          </button>
        ))}
        {isScheduler && (
          <button className="wave-tab wave-new" onClick={() => setNewWave(true)}>
            + New wave
          </button>
        )}
      </nav>
      {newWave && <NewWaveForm onDone={(id) => (setNewWave(false), id && setPicked(id))} />}
      {!wave ? (
        <p className="app-message">
          {isScheduler ? 'Create a wave to start scheduling.' : 'No sessions are scheduled yet.'}
        </p>
      ) : (
        <>
          {isScheduler && <WaveSetup wave={wave} />}
          {wave.status === 'open' && <p className="draft-banner">Draft — schedulers are still arranging this wave; it may change.</p>}
          <WaveGrid wave={wave} editable={isScheduler && wave.status === 'open'} onOpen={setOpenNoteId} />
        </>
      )}
      {openNoteId && notes[openNoteId] && <NoteModal note={notes[openNoteId]} onClose={() => setOpenNoteId(null)} />}
    </main>
  );
}

function NewWaveForm({ onDone }: { onDone: (waveId: string | null) => void }) {
  const send = useStore((s) => s.send);
  const count = useStore((s) => s.waves.length);
  const [name, setName] = useState(`Wave ${count + 1}`);
  const [tracks, setTracks] = useState(6);
  return (
    <form
      className="new-wave"
      onSubmit={(e) => {
        e.preventDefault();
        send('create_wave', { name: name.trim(), tracks }).then(
          () => onDone(useStore.getState().waves.at(-1)?.id ?? null),
          () => {},
        );
      }}
    >
      <label>
        Name <input value={name} onChange={(e) => setName(e.target.value)} maxLength={60} required autoFocus />
      </label>
      <label>
        Tracks{' '}
        <input type="number" min={1} max={20} value={tracks} onChange={(e) => setTracks(Number(e.target.value))} />
      </label>
      <button className="primary" type="submit" disabled={!name.trim() || tracks < 1 || tracks > 20}>
        Create wave
      </button>
      <button type="button" onClick={() => onDone(null)}>
        Cancel
      </button>
    </form>
  );
}

/** "Happening now" and "Up next", with one-click Join links (law of two feet). */
function NowNext({ now, onOpen }: { now: number; onOpen: (noteId: string) => void }) {
  const waves = useStore((s) => s.waves);
  const assignments = useStore((s) => s.assignments);
  const notes = useStore((s) => s.notes);
  const { current, next } = useMemo(() => nowAndNext(waves, now), [waves, now]);
  const waveOf = useMemo(() => slotWaves(waves), [waves]);
  if (!current && !next) return null;

  const row = (label: string, t: typeof current, big: boolean) => {
    if (!t) return null;
    const sessions: Note[] = slotSessions(t.slot.id, assignments, notes);
    const byNote = Object.fromEntries(assignments.filter((a) => a.slotId === t.slot.id).map((a) => [a.noteId, a]));
    return (
      <section className={`now-row${big ? ' now-current' : ''}`} aria-label={label}>
        <h2>
          {label} <span className="muted">{slotLabel(t.slot)} · {waveOf[t.slot.id]?.name}</span>
        </h2>
        {sessions.length === 0 ? (
          <p className="muted">Nothing scheduled.</p>
        ) : (
          <div className="now-sessions">
            {sessions.map((n) => (
              <SessionCard key={n.id} note={n} assignment={byNote[n.id]} onOpen={onOpen} compact={!big} />
            ))}
          </div>
        )}
      </section>
    );
  };
  return (
    <div className="now-next">
      {current ? row('Happening now', current, true) : <p className="now-idle">Nothing is running right now.</p>}
      {row('Up next', next, false)}
    </div>
  );
}
