import { useState, type FormEvent } from 'react';
import { login, type Me } from '../api/http';
import './Login.css';

interface Props {
  onLogin: (me: Me) => void;
}

export function Login({ onLogin }: Props) {
  const [name, setName] = useState('');
  const [email, setEmail] = useState('');
  const [adminKey, setAdminKey] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      onLogin(await login({ name, email, adminKey }));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="login">
      <form className="login-card" onSubmit={submit}>
        <h1>unconf</h1>
        <p className="login-hint">
          Enter the name others will see. Use the same name to pick up where you left off.
        </p>
        <label>
          Name
          <input
            name="name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            maxLength={40}
            autoComplete="name"
            autoFocus
            required
          />
        </label>
        <label>
          Email <span className="optional">(optional)</span>
          <input
            name="email"
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            autoComplete="email"
          />
        </label>
        <details>
          <summary>Organizer?</summary>
          <label>
            Admin key
            <input
              name="adminKey"
              type="password"
              value={adminKey}
              onChange={(e) => setAdminKey(e.target.value)}
              autoComplete="off"
            />
          </label>
        </details>
        {error && (
          <p className="login-error" role="alert">
            {error}
          </p>
        )}
        <button type="submit" disabled={busy || name.trim() === ''}>
          {busy ? 'Joining…' : 'Join'}
        </button>
      </form>
    </main>
  );
}
