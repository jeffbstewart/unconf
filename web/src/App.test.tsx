import { renderToString } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { App } from './App';

describe('App', () => {
  it('shows the login form when anonymous', () => {
    const html = renderToString(<App initial={{ status: 'anonymous' }} />);
    expect(html).toContain('name="name"');
    expect(html).toContain('name="email"');
    expect(html).toContain('name="adminKey"'); // behind the "Organizer?" disclosure
  });

  it('shows the user and role badge once logged in', () => {
    const me = { id: 'u1', name: 'Ada', role: 'organizer', votesRemaining: 5 } as const;
    const html = renderToString(<App initial={{ status: 'ready', me }} />);
    expect(html).toContain('Ada');
    expect(html).toContain('role-badge role-organizer');
  });
});
