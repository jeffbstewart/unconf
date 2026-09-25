import { describe, expect, it } from 'vitest';
import type { Assignment, Note, Wave } from '../api/protocol';
import { inClosedWave, interest, myClash, nowAndNext, pool, slotConflicts, slotSessions } from './logic';

const note = (id: string, over: Partial<Note> = {}): Note => ({
  id,
  title: id,
  bodyMd: '',
  authorId: `author-${id}`,
  x: 0,
  y: 0,
  color: 'yellow',
  regionId: null,
  voteTotal: 0,
  voted: false,
  voters: [],
  starred: false,
  links: [],
  scheduled: false,
  createdAt: `2026-01-01T00:00:0${id.length}.000Z`,
  updatedAt: '',
  ...over,
});

describe('conflicts', () => {
  it('counts each extra session a person wants in one slot', () => {
    const a = note('a', { voters: ['p1', 'p2'] });
    const b = note('b', { voters: ['p1', 'p3'] });
    const c = note('c', { voters: ['p1', 'p2'] });
    // p1 wants all three (2 extra), p2 wants a and c (1 extra).
    const r = slotConflicts([a, b, c]);
    expect(r.count).toBe(3);
    expect(r.overlaps.a).toEqual([
      { noteId: 'c', shared: 2 },
      { noteId: 'b', shared: 1 },
    ]);
    expect(r.proposerClash.size).toBe(0);
  });

  it('counts the proposer as attending their own session', () => {
    // Pat proposed "a" and voted for "b": Pat can't be in both.
    const a = note('a', { authorId: 'pat' });
    const b = note('b', { voters: ['pat'] });
    expect(interest(a).has('pat')).toBe(true);
    expect(slotConflicts([a, b]).count).toBe(1);
  });

  it('flags a proposer with two sessions in one slot', () => {
    const r = slotConflicts([note('a', { authorId: 'pat' }), note('b', { authorId: 'pat' }), note('c')]);
    expect([...r.proposerClash].sort()).toEqual(['a', 'b']);
  });

  it('is zero for disjoint audiences', () => {
    expect(slotConflicts([note('a', { voters: ['x'] }), note('b', { voters: ['y'] })]).count).toBe(0);
  });
});

describe('picks', () => {
  it('reports a clash when two of your picks share a slot', () => {
    const mine = note('a', { authorId: 'me' });
    const voted = note('b', { voted: true });
    const other = note('c');
    expect(myClash([mine, voted, other], 'me').map((n) => n.id)).toEqual(['a', 'b']);
    expect(myClash([mine, other], 'me')).toEqual([]);
  });
});

const wave = (id: string, status: Wave['status'], slots: [string, string, string][]): Wave => ({
  id,
  name: id,
  status,
  opensAt: null,
  tracks: 2,
  slots: slots.map(([sid, startAt, endAt]) => ({ id: sid, startAt, endAt })),
});

describe('nowAndNext', () => {
  const w1 = wave('w1', 'locked', [
    ['s1', '2026-10-01T10:00:00.000Z', '2026-10-01T10:45:00.000Z'],
    ['s2', '2026-10-01T11:00:00.000Z', '2026-10-01T11:45:00.000Z'],
  ]);
  const planned = wave('w2', 'planned', [['s3', '2026-10-01T10:50:00.000Z', '2026-10-01T10:55:00.000Z']]);
  const at = (iso: string) => Date.parse(iso);

  it('finds the running slot and the next one', () => {
    const r = nowAndNext([w1, planned], at('2026-10-01T10:30:00Z'));
    expect(r.current?.slot.id).toBe('s1');
    expect(r.next?.slot.id).toBe('s2'); // the planned wave's slot is skipped
  });

  it('handles gaps and the end of the day', () => {
    expect(nowAndNext([w1], at('2026-10-01T10:50:00Z')).current).toBeNull();
    const late = nowAndNext([w1], at('2026-10-01T12:00:00Z'));
    expect(late.current).toBeNull();
    expect(late.next).toBeNull();
  });

  it('treats a slot as ending exclusively', () => {
    expect(nowAndNext([w1], at('2026-10-01T10:45:00Z')).current).toBeNull();
  });
});

describe('pool', () => {
  const locked = wave('w1', 'locked', [['s1', 'a', 'b']]);
  const open = wave('w2', 'open', [['s2', 'c', 'd']]);
  const assignments: Assignment[] = [
    { id: 'a1', noteId: 'ran', slotId: 's1', track: 1, meetUrl: null },
    { id: 'a2', noteId: 'placed', slotId: 's2', track: 1, meetUrl: null },
  ];
  const notes = Object.fromEntries(
    [
      note('ran', { voteTotal: 9, scheduled: true }),
      note('placed', { voteTotal: 8, scheduled: true }),
      note('hot', { voteTotal: 5 }),
      note('warm', { voteTotal: 2 }),
      note('cold', { voteTotal: 1 }),
      note('gone', { voteTotal: 7, hidden: true }),
    ].map((n) => [n.id, n]),
  );

  it('splits unscheduled notes at the threshold and lists repeats', () => {
    const p = pool(notes, assignments, [locked, open], 'w2', 2);
    expect(p.eligible.map((n) => n.id)).toEqual(['hot', 'warm']);
    expect(p.below.map((n) => n.id)).toEqual(['cold']);
    expect(p.repeats.map((n) => n.id)).toEqual(['ran']);
  });

  it('lists sessions in a slot by track', () => {
    const as: Assignment[] = [
      { id: 'x', noteId: 'warm', slotId: 's', track: 2, meetUrl: null },
      { id: 'y', noteId: 'hot', slotId: 's', track: 1, meetUrl: null },
    ];
    expect(slotSessions('s', as, notes).map((n) => n.id)).toEqual(['hot', 'warm']);
  });
});

describe('inClosedWave', () => {
  it('is true only for sessions in locked or done waves', () => {
    const waves = [wave('a', 'open', [['s1', 'x', 'y']]), wave('b', 'locked', [['s2', 'x', 'y']])];
    const as: Assignment[] = [
      { id: '1', noteId: 'draft', slotId: 's1', track: 1, meetUrl: null },
      { id: '2', noteId: 'final', slotId: 's2', track: 1, meetUrl: null },
    ];
    expect(inClosedWave('draft', waves, as)).toBe(false);
    expect(inClosedWave('final', waves, as)).toBe(true);
    expect(inClosedWave('none', waves, as)).toBe(false);
  });
});
