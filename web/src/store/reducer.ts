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
  votesRemaining: number;
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
  votesRemaining: 0,
};

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
        votesRemaining: st.me.votesRemaining,
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
    default:
      return s; // kinds from later milestones
  }
}
