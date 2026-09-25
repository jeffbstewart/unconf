import { useState } from 'react';
import type { Lifecycle } from '../api/protocol';
import { useStore } from '../store/store';

const NEXT: Partial<Record<Lifecycle, { to: Lifecycle; label: string; confirm: string }>> = {
  setup: { to: 'active', label: 'Start event', confirm: 'Open the board to all participants?' },
  active: { to: 'done', label: 'End event', confirm: 'End the event? The board becomes read-only for everyone.' },
};

/** Lifecycle badge, plus the (confirmed) advance button for organizers. */
export function LifecycleControl({ lifecycle, isOrganizer }: { lifecycle: Lifecycle; isOrganizer: boolean }) {
  const send = useStore((s) => s.send);
  const [confirming, setConfirming] = useState(false);
  const next = NEXT[lifecycle];
  return (
    <span className="lifecycle">
      <span className={`lifecycle-badge lifecycle-${lifecycle}`}>{lifecycle}</span>
      {isOrganizer &&
        next &&
        (confirming ? (
          <span className="lifecycle-confirm">
            {next.confirm}{' '}
            <button
              className="primary"
              onClick={() => {
                setConfirming(false);
                send('set_lifecycle', { lifecycle: next.to }).catch(() => {});
              }}
            >
              {next.label}
            </button>{' '}
            <button onClick={() => setConfirming(false)}>Cancel</button>
          </span>
        ) : (
          <button onClick={() => setConfirming(true)}>{next.label}</button>
        ))}
    </span>
  );
}
