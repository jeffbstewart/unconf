import { useEffect, useState } from 'react';
import { fetchMe, logout, type Me } from './api/http';
import { Login } from './auth/Login';
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

  async function handleLogout() {
    await logout().catch(() => {});
    setSession({ status: 'anonymous' });
  }

  if (error) return <p className="app-message">Could not reach the server: {error}</p>;
  switch (session.status) {
    case 'loading':
      return null;
    case 'anonymous':
      return <Login onLogin={(me) => setSession({ status: 'ready', me })} />;
    case 'ready':
      return (
        <>
          <TopBar me={session.me} onLogout={handleLogout} />
          <main className="app-message">The board arrives in milestone 3.</main>
        </>
      );
  }
}
