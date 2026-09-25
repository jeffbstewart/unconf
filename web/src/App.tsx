import { useEffect, useState } from 'react';
import { fetchMe, logout, type Me } from './api/http';
import { Login } from './auth/Login';
import { Board } from './board/Board';
import { useStore } from './store/store';
import { Toasts } from './Toasts';
import { TopBar } from './TopBar';

type Session = { status: 'loading' } | { status: 'anonymous' } | { status: 'ready'; me: Me };

export function App({ initial = { status: 'loading' } }: { initial?: Session }) {
  const [session, setSession] = useState<Session>(initial);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (session.status !== 'loading') return;
    fetchMe()
      .then((me) => setSession(me ? { status: 'ready', me } : { status: 'anonymous' }))
      .catch((err: unknown) => setError(err instanceof Error ? err.message : String(err)));
  }, [session.status]);

  if (error) return <p className="app-message">Could not reach the server: {error}</p>;
  switch (session.status) {
    case 'loading':
      return null;
    case 'anonymous':
      return <Login onLogin={(me) => setSession({ status: 'ready', me })} />;
    case 'ready':
      return <Workspace me={session.me} onLoggedOut={() => setSession({ status: 'anonymous' })} />;
  }
}

/** The logged-in app: holds the realtime connection while mounted. */
function Workspace({ me, onLoggedOut }: { me: Me; onLoggedOut: () => void }) {
  const connect = useStore((s) => s.connect);
  const disconnect = useStore((s) => s.disconnect);
  const loggedOut = useStore((s) => s.loggedOut);
  const ready = useStore((s) => s.ready);
  const lifecycle = useStore((s) => s.event?.lifecycle);
  const role = useStore((s) => s.you?.role ?? me.role);

  useEffect(() => {
    connect();
    return disconnect;
  }, [connect, disconnect]);

  useEffect(() => {
    if (loggedOut) onLoggedOut();
  }, [loggedOut, onLoggedOut]);

  async function handleLogout() {
    disconnect();
    await logout().catch(() => {});
    onLoggedOut();
  }

  let body;
  if (!ready) body = <p className="app-message">Connecting…</p>;
  else if (lifecycle === 'setup' && role === 'participant') body = <NotStarted />;
  else body = <Board />;

  return (
    <>
      <TopBar me={me} onLogout={handleLogout} />
      {body}
      <Toasts />
    </>
  );
}

function NotStarted() {
  return (
    <main className="not-started">
      <div>
        <h1>Not started yet</h1>
        <p className="muted">The organizers are still setting up the board. This page updates when the event starts.</p>
      </div>
    </main>
  );
}
