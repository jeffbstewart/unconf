import type { Me } from './api/http';
import './TopBar.css';

interface Props {
  me: Me;
  onLogout: () => void;
}

export function TopBar({ me, onLogout }: Props) {
  return (
    <header className="topbar">
      <span className="topbar-brand">unconf</span>
      <span className="topbar-spacer" />
      <span className="topbar-user">{me.name}</span>
      <span className={`role-badge role-${me.role}`}>{me.role}</span>
      <button className="topbar-logout" onClick={onLogout}>
        Log out
      </button>
    </header>
  );
}
