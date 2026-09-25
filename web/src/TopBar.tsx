import { LifecycleControl } from './admin/LifecycleControl';
import type { Me } from './api/http';
import { hrefFor, type Route } from './route';
import { votesRemaining } from './store/reducer';
import { useStore } from './store/store';
import './TopBar.css';

interface Props {
  me: Me;
  route: Route;
  onLogout: () => void;
}

const STATUS_LABEL = { open: 'Connected', connecting: 'Connecting…', reconnecting: 'Reconnecting…' };

export function TopBar({ me, route, onLogout }: Props) {
  const event = useStore((s) => s.event);
  const you = useStore((s) => s.you);
  const status = useStore((s) => s.status);
  const remaining = useStore(votesRemaining);
  const role = you?.role ?? me.role;
  const isOrganizer = role === 'organizer';

  return (
    <header className="topbar">
      <span className="topbar-brand">{event?.name ?? 'unconf'}</span>
      {event && <LifecycleControl lifecycle={event.lifecycle} isOrganizer={isOrganizer} />}
      <nav className="topbar-tabs" aria-label="Screens">
        <a href={hrefFor('board')} aria-current={route === 'board' ? 'page' : undefined}>
          Board
        </a>
        <a href={hrefFor('schedule')} aria-current={route === 'schedule' ? 'page' : undefined}>
          Schedule
        </a>
        {isOrganizer && (
          <a href={hrefFor('admin')} aria-current={route === 'admin' ? 'page' : undefined}>
            Admin
          </a>
        )}
      </nav>
      <span className="topbar-spacer" />
      {event?.votingOpen && event.lifecycle !== 'done' && (
        <span className="topbar-votes" title={`You have ${remaining} of ${event.votesPerUser} votes left`}>
          <strong>{remaining}</strong> / {event.votesPerUser} votes left
        </span>
      )}
      <span className={`conn conn-${status}`} title={STATUS_LABEL[status]} aria-label={STATUS_LABEL[status]} />
      <span className="topbar-user">{me.name}</span>
      <span className={`role-badge role-${role}`}>{role}</span>
      <button className="topbar-logout" onClick={onLogout}>
        Log out
      </button>
    </header>
  );
}
