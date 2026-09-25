import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError, fetchMe, login, logout } from './http';

function mockFetch(status: number, body?: unknown) {
  const fn = vi.fn(
    async () =>
      new Response(body === undefined ? null : JSON.stringify(body), {
        status,
        headers: { 'Content-Type': 'application/json' },
      }),
  );
  vi.stubGlobal('fetch', fn);
  return fn;
}

afterEach(() => vi.unstubAllGlobals());

const me = { id: 'u1', name: 'Ada', role: 'participant', votesRemaining: 5 };

describe('fetchMe', () => {
  it('returns the user', async () => {
    mockFetch(200, me);
    expect(await fetchMe()).toEqual(me);
  });

  it('returns null without a session', async () => {
    mockFetch(401, { error: 'not logged in' });
    expect(await fetchMe()).toBeNull();
  });

  it('throws on server errors', async () => {
    mockFetch(500, { error: 'internal error' });
    await expect(fetchMe()).rejects.toThrow('internal error');
  });
});

describe('login', () => {
  it('posts JSON and omits empty optional fields', async () => {
    const fn = mockFetch(200, me);
    await login({ name: 'Ada', email: '  ', adminKey: '' });
    const [url, init] = fn.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('/api/login');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual({ name: 'Ada' });
  });

  it('sends email and admin key when given', async () => {
    const fn = mockFetch(200, { ...me, role: 'organizer' });
    await login({ name: 'Ada', email: ' a@b.c ', adminKey: 'k' });
    const init = (fn.mock.calls[0] as unknown as [string, RequestInit])[1];
    expect(JSON.parse(init.body as string)).toEqual({ name: 'Ada', email: 'a@b.c', adminKey: 'k' });
  });

  it('surfaces the server error message', async () => {
    mockFetch(403, { error: 'invalid admin key' });
    const err = await login({ name: 'Ada', adminKey: 'x' }).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).status).toBe(403);
    expect((err as ApiError).message).toBe('invalid admin key');
  });
});

describe('logout', () => {
  it('treats an already-expired session as success', async () => {
    mockFetch(401, { error: 'not logged in' });
    await expect(logout()).resolves.toBeUndefined();
  });
});
