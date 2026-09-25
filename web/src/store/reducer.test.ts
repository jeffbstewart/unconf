import { describe, expect, it } from 'vitest';
import type { BoardEvent, Note, ServerFrame, Snapshot } from '../api/protocol';
import { applyFrame, initialBoard, votesRemaining, type BoardState } from './reducer';

const note = (id: string, over: Partial<Note> = {}): Note => ({
  id,
  title: `Note ${id}`,
  bodyMd: '',
  authorId: 'u1',
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
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
  ...over,
});

const snapshot = (over: Partial<Snapshot> = {}): Snapshot => ({
  event: { id: 'e', name: 'Camp', lifecycle: 'active', votingOpen: false, votesPerUser: 5, scheduleThreshold: 2 },
  users: [{ id: 'u1', name: 'Ada', role: 'participant' }],
  notes: [note('n1')],
  regions: [],
  waves: [],
  assignments: [{ id: 'a1', noteId: 'n1', slotId: 's', track: 1, meetUrl: null }],
  messages: { n1: [{ id: 'm1', authorId: 'u1', threadId: null, body: 'hi', createdAt: 't' }] },
  me: { votesRemaining: 5, votesUsed: 0 },
  ...over,
});

function run(frames: ServerFrame[], from: BoardState = initialBoard): BoardState {
  return frames.reduce(applyFrame, from);
}

const loaded = () =>
  run([
    { type: 'hello', you: { id: 'u1', name: 'Ada', role: 'participant' }, eventSeq: 10 },
    { type: 'snapshot', seq: 10, state: snapshot() },
  ]);

describe('applyFrame', () => {
  it('replaces state wholesale on snapshot', () => {
    const s = loaded();
    expect(s.ready).toBe(true);
    expect(s.seq).toBe(10);
    expect(s.you?.name).toBe('Ada');
    expect(Object.keys(s.notes)).toEqual(['n1']);
    expect(s.users.u1.name).toBe('Ada');

    const again = applyFrame(s, { type: 'snapshot', seq: 3, state: snapshot({ notes: [] }) });
    expect(again.notes).toEqual({});
    expect(again.seq).toBe(3); // server was reset: follow it
  });

  it('applies note events and tracks seq across gaps', () => {
    let s = loaded();
    s = run(
      [
        { type: 'event', seq: 11, event: { kind: 'note_created', note: note('n2', { x: 5 }) } },
        { type: 'event', seq: 14, event: { kind: 'note_moved', noteId: 'n2', x: 200, y: 120, byUserId: 'u2' } },
        {
          type: 'event',
          seq: 15,
          event: { kind: 'note_updated', noteId: 'n2', title: 'New', bodyMd: 'b', color: 'pink', updatedAt: 'later' },
        },
        {
          type: 'event',
          seq: 16,
          event: { kind: 'links_set', noteId: 'n2', links: [{ id: 'l', title: 'D', url: 'https://x', kind: 'doc' }] },
        },
      ],
      s,
    );
    expect(s.seq).toBe(16);
    expect(s.notes.n2).toMatchObject({ x: 200, y: 120, title: 'New', bodyMd: 'b', color: 'pink' });
    expect(s.notes.n2.links).toHaveLength(1);
  });

  it('removes a deleted note with its messages and assignments', () => {
    const s = applyFrame(loaded(), { type: 'event', seq: 11, event: { kind: 'note_deleted', noteId: 'n1' } });
    expect(s.notes.n1).toBeUndefined();
    expect(s.messages.n1).toBeUndefined();
    expect(s.assignments).toEqual([]);
  });

  it('ignores stale and duplicate events', () => {
    const s = loaded();
    const stale = applyFrame(s, { type: 'event', seq: 10, event: { kind: 'note_deleted', noteId: 'n1' } });
    expect(stale).toBe(s);
  });

  it('ignores events for unknown notes and unknown kinds but advances seq', () => {
    let s = loaded();
    s = applyFrame(s, { type: 'event', seq: 11, event: { kind: 'note_moved', noteId: 'ghost', x: 1, y: 1, byUserId: 'u' } });
    s = applyFrame(s, { type: 'event', seq: 12, event: { kind: 'vote_cast' } });
    expect(s.seq).toBe(12);
    expect(Object.keys(s.notes)).toEqual(['n1']);
  });

  it('tracks users, roles (including our own), and lifecycle', () => {
    let s = loaded();
    s = run(
      [
        { type: 'event', seq: 11, event: { kind: 'user_joined', user: { id: 'u2', name: 'Bo', role: 'participant' } } },
        { type: 'event', seq: 12, event: { kind: 'role_set', userId: 'u1', role: 'organizer' } },
        { type: 'event', seq: 13, event: { kind: 'lifecycle_set', lifecycle: 'done' } },
      ],
      s,
    );
    expect(s.users.u2.name).toBe('Bo');
    expect(s.users.u1.role).toBe('organizer');
    expect(s.you?.role).toBe('organizer');
    expect(s.event?.lifecycle).toBe('done');
  });

  it('leaves state untouched for ack, error, and pong', () => {
    const s = loaded();
    expect(applyFrame(s, { type: 'ack', cmdId: 'c', seq: 99 })).toBe(s);
    expect(applyFrame(s, { type: 'error', cmdId: 'c', code: 'forbidden', message: 'no' })).toBe(s);
    expect(applyFrame(s, { type: 'pong' })).toBe(s);
  });
});

describe('regions and stars', () => {
  const region = { id: 'r1', label: 'Track A', x: 0, y: 0, w: 400, h: 300, color: '#f2d45c', z: 0 };

  it('creates, updates, and deletes regions', () => {
    let s = loaded();
    s = applyFrame(s, { type: 'event', seq: 11, event: { kind: 'region_created', region } });
    expect(s.regions).toEqual([region]);
    s = applyFrame(s, { type: 'event', seq: 12, event: { kind: 'region_updated', region: { ...region, label: 'B' } } });
    expect(s.regions[0].label).toBe('B');
    s = applyFrame(s, { type: 'event', seq: 13, event: { kind: 'region_deleted', regionId: 'r1' } });
    expect(s.regions).toEqual([]);
  });

  it('retags notes', () => {
    let s = applyFrame(loaded(), { type: 'event', seq: 11, event: { kind: 'note_retagged', noteId: 'n1', regionId: 'r1' } });
    expect(s.notes.n1.regionId).toBe('r1');
    s = applyFrame(s, { type: 'event', seq: 12, event: { kind: 'note_retagged', noteId: 'n1', regionId: null } });
    expect(s.notes.n1.regionId).toBeNull();
  });

  it('stars and unstars', () => {
    let s = applyFrame(loaded(), { type: 'event', seq: 11, event: { kind: 'note_starred', noteId: 'n1' } });
    expect(s.notes.n1.starred).toBe(true);
    s = applyFrame(s, { type: 'event', seq: 12, event: { kind: 'note_unstarred', noteId: 'n1' } });
    expect(s.notes.n1.starred).toBe(false);
  });
});

describe('voting', () => {
  const vote = (seq: number, kind: 'vote_cast' | 'vote_retracted', by: string, total: number): ServerFrame => ({
    type: 'event',
    seq,
    event: { kind, noteId: 'n1', byUserId: by, total },
  });

  it('tracks totals, my vote, and my budget', () => {
    let s = loaded();
    expect(votesRemaining(s)).toBe(5);
    s = run([vote(11, 'vote_cast', 'u1', 1), vote(12, 'vote_cast', 'u2', 2)], s);
    expect(s.notes.n1).toMatchObject({ voteTotal: 2, voted: true });
    expect(s.votesUsed).toBe(1);
    expect(votesRemaining(s)).toBe(4);
    s = applyFrame(s, vote(13, 'vote_retracted', 'u2', 1));
    expect(s.notes.n1).toMatchObject({ voteTotal: 1, voted: true });
    s = applyFrame(s, vote(14, 'vote_retracted', 'u1', 0));
    expect(s.notes.n1).toMatchObject({ voteTotal: 0, voted: false });
    expect(votesRemaining(s)).toBe(5);
  });

  it('never double-counts my own vote', () => {
    // e.g. a vote_cast for a vote the snapshot already included
    let s = run([vote(11, 'vote_cast', 'u1', 1), vote(12, 'vote_cast', 'u1', 1)], loaded());
    expect(s.votesUsed).toBe(1);
    s = run([vote(13, 'vote_retracted', 'u1', 0), vote(14, 'vote_retracted', 'u1', 0)], s);
    expect(s.votesUsed).toBe(0);
  });

  it('refunds my vote when a voted note is deleted', () => {
    let s = applyFrame(loaded(), vote(11, 'vote_cast', 'u1', 1));
    s = applyFrame(s, { type: 'event', seq: 12, event: { kind: 'note_deleted', noteId: 'n1' } });
    expect(s.votesUsed).toBe(0);
  });

  it('follows voting open/closed and budget changes', () => {
    let s = applyFrame(loaded(), vote(11, 'vote_cast', 'u1', 1));
    s = run(
      [
        { type: 'event', seq: 12, event: { kind: 'voting_set', open: true } },
        { type: 'event', seq: 13, event: { kind: 'votes_per_user_set', n: 1 } },
      ],
      s,
    );
    expect(s.event?.votingOpen).toBe(true);
    expect(votesRemaining(s)).toBe(0);
    s = applyFrame(s, { type: 'event', seq: 14, event: { kind: 'votes_per_user_set', n: 3 } });
    expect(votesRemaining(s)).toBe(2);
  });
});

describe('scheduling', () => {
  const wave = { id: 'w', name: 'AM', status: 'planned' as const, opensAt: null, tracks: 2, slots: [] };
  const slot = (id: string, startAt: string) => ({ id, startAt, endAt: startAt });
  const ev = (seq: number, event: BoardEvent): ServerFrame => ({
    type: 'event',
    seq,
    event,
  });

  it('builds waves, slots, and assignments, keeping note.scheduled in sync', () => {
    let s = run(
      [
        ev(11, { kind: 'wave_created', wave }),
        ev(12, { kind: 'slot_created', waveId: 'w', slot: slot('s2', '2026-10-01T11:00:00.000Z') }),
        ev(13, { kind: 'slot_created', waveId: 'w', slot: slot('s1', '2026-10-01T10:00:00.000Z') }),
        ev(14, { kind: 'wave_status_set', waveId: 'w', status: 'open' }),
      ],
      { ...loaded(), assignments: [] },
    );
    expect(s.waves[0].slots.map((x) => x.id)).toEqual(['s1', 's2']); // sorted by start
    expect(s.waves[0].status).toBe('open');

    s = applyFrame(s, ev(15, { kind: 'note_assigned', assignment: { id: 'a', noteId: 'n1', slotId: 's1', track: 1, meetUrl: null } }));
    expect(s.notes.n1.scheduled).toBe(true);
    s = applyFrame(s, ev(16, { kind: 'assignment_links_set', links: { a: 'https://meet.example/a' } }));
    expect(s.assignments[0].meetUrl).toBe('https://meet.example/a');
    s = applyFrame(s, ev(17, { kind: 'note_unassigned', assignmentId: 'a' }));
    expect(s.notes.n1.scheduled).toBe(false);

    s = applyFrame(s, ev(18, { kind: 'note_assigned', assignment: { id: 'b', noteId: 'n1', slotId: 's2', track: 2, meetUrl: null } }));
    s = applyFrame(s, ev(19, { kind: 'slot_deleted', waveId: 'w', slotId: 's2' }));
    expect(s.assignments).toEqual([]);
    expect(s.notes.n1.scheduled).toBe(false);

    s = applyFrame(s, ev(20, { kind: 'wave_updated', wave: { ...s.waves[0], tracks: 6 } }));
    expect(s.waves[0].tracks).toBe(6);
    s = applyFrame(s, ev(21, { kind: 'wave_deleted', waveId: 'w' }));
    expect(s.waves).toEqual([]);
  });

  it('applies budget refunds and the threshold', () => {
    let s = applyFrame(loaded(), ev(11, { kind: 'votes_used_set', votesUsed: 3 }));
    expect(s.votesUsed).toBe(3);
    s = applyFrame(s, ev(12, { kind: 'schedule_threshold_set', n: 4 }));
    expect(s.event?.scheduleThreshold).toBe(4);
  });

  it('keeps note voters in step with vote events', () => {
    let s = applyFrame(loaded(), ev(11, { kind: 'vote_cast', noteId: 'n1', byUserId: 'u2', total: 1 }));
    s = applyFrame(s, ev(12, { kind: 'vote_cast', noteId: 'n1', byUserId: 'u1', total: 2 }));
    expect(s.notes.n1.voters).toEqual(['u2', 'u1']);
    s = applyFrame(s, ev(13, { kind: 'vote_retracted', noteId: 'n1', byUserId: 'u2', total: 1 }));
    expect(s.notes.n1.voters).toEqual(['u1']);
  });
});
