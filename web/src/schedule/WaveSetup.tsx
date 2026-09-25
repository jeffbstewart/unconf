import { useState } from 'react';
import type { Wave, WaveStatus } from '../api/protocol';
import { useStore } from '../store/store';
import { slotLabel } from './logic';

const NEXT: Partial<Record<WaveStatus, { to: WaveStatus; label: string; confirm?: string }>> = {
  planned: { to: 'open', label: 'Open for scheduling' },
  open: {
    to: 'locked',
    label: 'Lock & publish',
    confirm:
      'Lock this wave? The schedule becomes final, a meeting is created for each session, and voters get those votes back for the next wave.',
  },
  locked: { to: 'done', label: 'Mark as done' },
};

/** Scheduler controls for a wave: status, name, tracks, slots, threshold. */
export function WaveSetup({ wave }: { wave: Wave }) {
  const send = useStore((s) => s.send);
  const threshold = useStore((s) => s.event?.scheduleThreshold ?? 2);
  const [confirming, setConfirming] = useState(false);
  const [confirmClear, setConfirmClear] = useState(false);
  const next = NEXT[wave.status];
  const editable = wave.status === 'planned' || wave.status === 'open';

  const advance = () => {
    setConfirming(false);
    if (next) send('set_wave_status', { waveId: wave.id, status: next.to }).catch(() => {});
  };

  return (
    <section className="wave-setup" aria-label="Wave setup">
      <div className="setup-row">
        <strong>{wave.name}</strong>
        <span className={`wave-status wave-${wave.status}`}>{wave.status}</span>
        {next &&
          (confirming ? (
            <span className="confirm">
              {next.confirm}{' '}
              <button className="primary" onClick={advance}>
                {next.label}
              </button>{' '}
              <button onClick={() => setConfirming(false)}>Cancel</button>
            </span>
          ) : (
            <button className="primary" onClick={() => (next.confirm ? setConfirming(true) : advance())}>
              {next.label}
            </button>
          ))}
        {wave.status === 'open' &&
          (confirmClear ? (
            <span className="confirm">
              Remove every session from this wave?{' '}
              <button
                className="danger"
                onClick={() => {
                  setConfirmClear(false);
                  send('clear_wave', { waveId: wave.id }).catch(() => {});
                }}
              >
                Clear
              </button>{' '}
              <button onClick={() => setConfirmClear(false)}>Keep</button>
            </span>
          ) : (
            <button onClick={() => setConfirmClear(true)}>Clear…</button>
          ))}
        {wave.status === 'planned' && (
          <button className="danger-quiet" onClick={() => send('delete_wave', { waveId: wave.id }).catch(() => {})}>
            Delete wave
          </button>
        )}
      </div>
      {editable && (
        <div className="setup-row">
          <label>
            Tracks{' '}
            <input
              key={wave.tracks}
              type="number"
              min={1}
              max={20}
              defaultValue={wave.tracks}
              aria-label="Tracks"
              onBlur={(e) => {
                const n = Number(e.target.value);
                if (n !== wave.tracks) send('update_wave', { waveId: wave.id, tracks: n }).catch(() => (e.target.value = String(wave.tracks)));
              }}
            />
          </label>
          <label>
            Min. votes to suggest{' '}
            <input
              key={threshold}
              type="number"
              min={1}
              max={20}
              defaultValue={threshold}
              aria-label="Minimum votes"
              onBlur={(e) => {
                const n = Number(e.target.value);
                if (n !== threshold) send('set_schedule_threshold', { n }).catch(() => (e.target.value = String(threshold)));
              }}
            />
          </label>
          <span className="slot-chips" aria-label="Slots">
            {wave.slots.map((sl) => (
              <span key={sl.id} className="slot-chip">
                {slotLabel(sl)}
                <button
                  className="icon"
                  aria-label={`Remove slot ${slotLabel(sl)}`}
                  onClick={() => send('delete_slot', { slotId: sl.id }).catch(() => {})}
                >
                  ✕
                </button>
              </span>
            ))}
          </span>
          <AddSlot wave={wave} />
        </div>
      )}
    </section>
  );
}

/** "YYYY-MM-DDTHH:mm" in local time, for a datetime-local input. */
function localInput(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function AddSlot({ wave }: { wave: Wave }) {
  const send = useStore((s) => s.send);
  const last = wave.slots.at(-1);
  // Default: right after the last slot (plus a 15-minute break), or the next hour.
  const suggested = last
    ? new Date(Date.parse(last.endAt) + 15 * 60_000)
    : new Date(Math.ceil(Date.now() / 3_600_000) * 3_600_000);
  const [start, setStart] = useState(localInput(suggested));
  const [minutes, setMinutes] = useState(45);
  const add = () => {
    const s = new Date(start);
    if (Number.isNaN(s.getTime())) return;
    const e = new Date(s.getTime() + minutes * 60_000);
    send('create_slot', { waveId: wave.id, startAt: s.toISOString(), endAt: e.toISOString() }).then(
      () => setStart(localInput(new Date(e.getTime() + 15 * 60_000))),
      () => {},
    );
  };
  return (
    <span className="add-slot">
      <input type="datetime-local" value={start} onChange={(e) => setStart(e.target.value)} aria-label="Slot start" />
      <select value={minutes} onChange={(e) => setMinutes(Number(e.target.value))} aria-label="Slot length">
        {[30, 45, 50, 60, 90].map((m) => (
          <option key={m} value={m}>
            {m} min
          </option>
        ))}
      </select>
      <button onClick={add}>+ Add slot</button>
    </span>
  );
}
