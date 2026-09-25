// Pure state transitions for server frames. The server is authoritative:
// shared state changes only here, in response to snapshot/event frames.

import type { Assignment, BoardEvent, EventInfo, Message, Note, Region, Room, ServerFrame, User, Wave } from '../api/protocol';

export interface BoardState {
  /** The logged-in user, from the hello frame. */
  you: User | null;
  /** Highest seq applied; reconnects resume from here. */
  seq: number;
  /** True once a snapshot has been applied. */
  ready: boolean;
  event: EventInfo | null;
  users: Record<string, User>;
  notes: Record<string, Note>;
  regions: Region[];
  waves: Wave[];
  rooms: Room[];
  assignments: Assignment[];
  messages: Record<string, Message[]>;
  /** Vote dots this user has placed (see votesRemaining). */
  votesUsed: number;
}

export const initialBoard: BoardState = {
  you: null,
  seq: 0,
  ready: false,
  event: null,
  users: {},
  notes: {},
  regions: [],
  waves: [],
  rooms: [],
  assignments: [],
  messages: {},
  votesUsed: 0,
};

/** Votes the user may still cast. Over budget (after a cut) reads as 0. */
export function votesRemaining(s: Pick<BoardState, 'event' | 'votesUsed'>): number {
  return Math.max(0, (s.event?.votesPerUser ?? 0) - s.votesUsed);
}

function byId<T extends { id: string }>(items: T[]): Record<string, T> {
  return Object.fromEntries(items.map((i) => [i.id, i]));
}

/** Applies one server frame. Frames that don't change state return s. */
export function applyFrame(s: BoardState, f: ServerFrame): BoardState {
  switch (f.type) {
    case 'hello':
      return { ...s, you: f.you };
    case 'snapshot': {
      const st = f.state;
      return {
        ...s,
        seq: f.seq,
        ready: true,
        event: st.event,
        users: byId(st.users),
        notes: byId(st.notes),
        regions: st.regions,
        waves: st.waves,
        rooms: st.rooms,
        assignments: st.assignments,
        messages: st.messages,
        votesUsed: st.me.votesUsed,
      };
    }
    case 'event':
      // Seqs can skip (role-filtered or personal events); never go backwards.
      if (f.seq <= s.seq) return s;
      return { ...applyEvent(s, f.event as BoardEvent), seq: f.seq };
    default:
      return s;
  }
}

function patchNote(s: BoardState, id: string, patch: Partial<Note>): BoardState {
  const n = s.notes[id];
  if (!n) return s;
  return { ...s, notes: { ...s.notes, [id]: { ...n, ...patch } } };
}

export function applyEvent(s: BoardState, e: BoardEvent): BoardState {
  switch (e.kind) {
    case 'note_created':
      return { ...s, notes: { ...s.notes, [e.note.id]: e.note } };
    case 'note_updated':
      return patchNote(s, e.noteId, { title: e.title, bodyMd: e.bodyMd, color: e.color, updatedAt: e.updatedAt });
    case 'note_moved':
      return patchNote(s, e.noteId, { x: e.x, y: e.y });
    case 'note_deleted': {
      if (!s.notes[e.noteId]) return s;
      const notes = { ...s.notes };
      delete notes[e.noteId];
      const messages = { ...s.messages };
      delete messages[e.noteId];
      return {
        ...s,
        notes,
        messages,
        assignments: s.assignments.filter((a) => a.noteId !== e.noteId),
        // The note's votes were deleted with it, refunding ours.
        votesUsed: Math.max(0, s.votesUsed - s.notes[e.noteId].myVotes),
      };
    }
    case 'links_set':
      return patchNote(s, e.noteId, { links: e.links });
    case 'user_joined':
      return { ...s, users: { ...s.users, [e.user.id]: e.user } };
    case 'role_set': {
      const u = s.users[e.userId];
      const users = u ? { ...s.users, [e.userId]: { ...u, role: e.role } } : s.users;
      const you = s.you?.id === e.userId ? { ...s.you, role: e.role } : s.you;
      return { ...s, users, you };
    }
    case 'lifecycle_set':
      return s.event ? { ...s, event: { ...s.event, lifecycle: e.lifecycle } } : s;
    case 'region_created':
      return { ...s, regions: [...s.regions.filter((r) => r.id !== e.region.id), e.region] };
    case 'region_updated':
      return { ...s, regions: s.regions.map((r) => (r.id === e.region.id ? e.region : r)) };
    case 'region_deleted':
      return { ...s, regions: s.regions.filter((r) => r.id !== e.regionId) };
    case 'note_retagged':
      return patchNote(s, e.noteId, { regionId: e.regionId });
    case 'note_starred':
      return patchNote(s, e.noteId, { starred: true });
    case 'note_unstarred':
      return patchNote(s, e.noteId, { starred: false });
    case 'vote_cast':
    case 'vote_retracted': {
      const n = s.notes[e.noteId];
      if (!n) return s;
      const mine = e.byUserId === s.you?.id;
      const delta = e.kind === 'vote_cast' ? 1 : -1;
      return {
        ...patchNote(s, e.noteId, { voteTotal: e.total, myVotes: mine ? n.myVotes + delta : n.myVotes }),
        votesUsed: mine ? s.votesUsed + delta : s.votesUsed,
      };
    }
    case 'voting_set':
      return s.event ? { ...s, event: { ...s.event, votingOpen: e.open } } : s;
    case 'votes_per_user_set':
      return s.event ? { ...s, event: { ...s.event, votesPerUser: e.n } } : s;
    default:
      return s; // kinds from later milestones
  }
}
