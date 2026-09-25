// Pure state transitions for server frames. The server is authoritative:
// shared state changes only here, in response to snapshot/event frames.

import type { Assignment, BoardEvent, EventInfo, Message, Note, Region, ServerFrame, User, Wave } from '../api/protocol';

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

/** Re-derives notes' `scheduled` flag (has any assignment) after assignments change. */
function withAssignments(s: BoardState, assignments: Assignment[], noteIds: string[]): BoardState {
  let notes = s.notes;
  for (const id of noteIds) {
    const n = notes[id];
    if (!n) continue;
    const scheduled = assignments.some((a) => a.noteId === id);
    if (scheduled !== n.scheduled) notes = { ...notes, [id]: { ...n, scheduled } };
  }
  return { ...s, notes, assignments };
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
        votesUsed: Math.max(0, s.votesUsed - (s.notes[e.noteId].voted ? 1 : 0)),
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
      const cast = e.kind === 'vote_cast';
      const voters = cast
        ? n.voters.includes(e.byUserId)
          ? n.voters
          : [...n.voters, e.byUserId]
        : n.voters.filter((v) => v !== e.byUserId);
      if (e.byUserId !== s.you?.id) return patchNote(s, e.noteId, { voteTotal: e.total, voters });
      const changed = cast !== n.voted;
      return {
        ...patchNote(s, e.noteId, { voteTotal: e.total, voted: cast, voters }),
        votesUsed: changed ? s.votesUsed + (cast ? 1 : -1) : s.votesUsed,
      };
    }
    case 'voting_set':
      return s.event ? { ...s, event: { ...s.event, votingOpen: e.open } } : s;
    case 'votes_per_user_set':
      return s.event ? { ...s, event: { ...s.event, votesPerUser: e.n } } : s;
    case 'votes_used_set':
      return { ...s, votesUsed: e.votesUsed };
    case 'schedule_threshold_set':
      return s.event ? { ...s, event: { ...s.event, scheduleThreshold: e.n } } : s;
    case 'wave_created':
      return { ...s, waves: [...s.waves.filter((w) => w.id !== e.wave.id), e.wave] };
    case 'wave_updated':
      return { ...s, waves: s.waves.map((w) => (w.id === e.wave.id ? e.wave : w)) };
    case 'wave_deleted': {
      const gone = new Set(s.waves.find((w) => w.id === e.waveId)?.slots.map((sl) => sl.id));
      const removed = s.assignments.filter((a) => gone.has(a.slotId));
      return withAssignments(
        { ...s, waves: s.waves.filter((w) => w.id !== e.waveId) },
        s.assignments.filter((a) => !gone.has(a.slotId)),
        removed.map((a) => a.noteId),
      );
    }
    case 'wave_status_set':
      return { ...s, waves: s.waves.map((w) => (w.id === e.waveId ? { ...w, status: e.status } : w)) };
    case 'slot_created':
      return {
        ...s,
        waves: s.waves.map((w) =>
          w.id === e.waveId
            ? { ...w, slots: [...w.slots.filter((sl) => sl.id !== e.slot.id), e.slot].sort((a, b) => a.startAt.localeCompare(b.startAt)) }
            : w,
        ),
      };
    case 'slot_deleted': {
      const removed = s.assignments.filter((a) => a.slotId === e.slotId);
      return withAssignments(
        { ...s, waves: s.waves.map((w) => (w.id === e.waveId ? { ...w, slots: w.slots.filter((sl) => sl.id !== e.slotId) } : w)) },
        s.assignments.filter((a) => a.slotId !== e.slotId),
        removed.map((a) => a.noteId),
      );
    }
    case 'note_assigned':
      return withAssignments(
        s,
        [...s.assignments.filter((a) => a.id !== e.assignment.id), e.assignment],
        [e.assignment.noteId],
      );
    case 'note_unassigned': {
      const a = s.assignments.find((x) => x.id === e.assignmentId);
      if (!a) return s;
      return withAssignments(s, s.assignments.filter((x) => x.id !== e.assignmentId), [a.noteId]);
    }
    case 'assignment_links_set':
      return {
        ...s,
        assignments: s.assignments.map((a) => (e.links[a.id] ? { ...a, meetUrl: e.links[a.id] } : a)),
      };
    default:
      return s; // kinds from later milestones
  }
}
