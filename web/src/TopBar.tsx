import { useState } from 'react';
import type { Me } from './api/http';
import type { Lifecycle } from './api/protocol';
import { useStore } from './store/store';
import './TopBar.css';

interface Props {
  me: Me;
  onLogout: () => void;
}

const STATUS_LABEL = { open: 'Connected', connecting: 'Connecting…', reconnecting: 'Reconnecting…' };

export function TopBar({ me, onLogout }: Props) {
  const event = useStore((s) => s.event);
  const you = useStore((s) => s.you);
  const status = useStore((s) => s.status);
  const role = you?.role ?? me.role;

  return (
    <header className="topbar">
      <span className="topbar-brand">{event?.name ?? 'unconf'}</span>
      {event && <LifecycleControl lifecycle={event.lifecycle} isOrganizer={role === 'organizer'} />}
      <span className="topbar-spacer" />
      <span className={`conn conn-${status}`} title={STATUS_LABEL[status]} aria-label={STATUS_LABEL[status]} />
      <span className="topbar-user">{me.name}</span>
      <span className={`role-badge role-${role}`}>{role}</span>
      <button className="topbar-logout" onClick={onLogout}>
        Log out
      </button>
    </header>
  );
}

const NEXT: Partial<Record<Lifecycle, { to: Lifecycle; label: string; confirm: string }>> = {
  setup: { to: 'active', label: 'Start event', confirm: 'Open the board to all participants?' },
  active: { to: 'done', label: 'End event', confirm: 'End the event? The board becomes read-only for everyone.' },
};

function LifecycleControl({ lifecycle, isOrganizer }: { lifecycle: Lifecycle; isOrganizer: boolean }) {
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
