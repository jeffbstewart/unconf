import { beforeEach, describe, expect, it, vi } from 'vitest';

describe('vote notice acknowledgement', () => {
  beforeEach(() => {
    vi.resetModules();
    vi.unstubAllGlobals();
  });

  it('is remembered per user in localStorage', async () => {
    const data = new Map<string, string>();
    vi.stubGlobal('localStorage', {
      getItem: (k: string) => data.get(k) ?? null,
      setItem: (k: string, v: string) => void data.set(k, v),
    });
    const m = await import('./voteNoticeState');
    expect(m.hasAcknowledged('u1')).toBe(false);
    m.acknowledge('u1');
    expect(m.hasAcknowledged('u1')).toBe(true);
    expect(m.hasAcknowledged('u2')).toBe(false);

    vi.resetModules(); // a reload: memory is gone, storage persists
    const reloaded = await import('./voteNoticeState');
    expect(reloaded.hasAcknowledged('u1')).toBe(true);
  });

  it('falls back to memory when storage throws', async () => {
    vi.stubGlobal('localStorage', {
      getItem: () => {
        throw new Error('blocked');
      },
      setItem: () => {
        throw new Error('blocked');
      },
    });
    const m = await import('./voteNoticeState');
    expect(m.hasAcknowledged('u1')).toBe(false);
    m.acknowledge('u1');
    expect(m.hasAcknowledged('u1')).toBe(true);
  });
});
