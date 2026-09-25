import { describe, expect, it } from 'vitest';
import type { Note, ServerFrame, Snapshot } from '../api/protocol';
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
  myVotes: 0,
  starred: false,
  links: [],
  scheduled: false,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
  ...over,
});

const snapshot = (over: Partial<Snapshot> = {}): Snapshot => ({
  event: { id: 'e', name: 'Camp', lifecycle: 'active', votingOpen: false, votesPerUser: 5 },
  users: [{ id: 'u1', name: 'Ada', role: 'participant' }],
  notes: [note('n1')],
  regions: [],
  waves: [],
  rooms: [],
  assignments: [{ id: 'a1', noteId: 'n1', slotId: 's', roomId: 'r' }],
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

  it('tracks totals, my dots, and my budget', () => {
    let s = loaded();
    expect(votesRemaining(s)).toBe(5);
    s = run([vote(11, 'vote_cast', 'u1', 1), vote(12, 'vote_cast', 'u2', 2), vote(13, 'vote_cast', 'u1', 3)], s);
    expect(s.notes.n1).toMatchObject({ voteTotal: 3, myVotes: 2 });
    expect(s.votesUsed).toBe(2);
    expect(votesRemaining(s)).toBe(3);
    s = applyFrame(s, vote(14, 'vote_retracted', 'u2', 2));
    expect(s.notes.n1).toMatchObject({ voteTotal: 2, myVotes: 2 });
    s = applyFrame(s, vote(15, 'vote_retracted', 'u1', 1));
    expect(s.notes.n1.myVotes).toBe(1);
    expect(votesRemaining(s)).toBe(4);
  });

  it('refunds my dots when a voted note is deleted', () => {
    let s = run([vote(11, 'vote_cast', 'u1', 1), vote(12, 'vote_cast', 'u1', 2)], loaded());
    s = applyFrame(s, { type: 'event', seq: 13, event: { kind: 'note_deleted', noteId: 'n1' } });
    expect(s.votesUsed).toBe(0);
  });

  it('follows voting open/closed and budget changes', () => {
    let s = run([vote(11, 'vote_cast', 'u1', 1), vote(12, 'vote_cast', 'u1', 2)], loaded());
    s = run(
      [
        { type: 'event', seq: 13, event: { kind: 'voting_set', open: true } },
        { type: 'event', seq: 14, event: { kind: 'votes_per_user_set', n: 1 } },
      ],
      s,
    );
    expect(s.event?.votingOpen).toBe(true);
    expect(votesRemaining(s)).toBe(0); // over budget after the cut: 2 used of 1
  });
});
