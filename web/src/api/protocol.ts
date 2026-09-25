// WebSocket protocol types (SPEC §8). Mirrors internal/server/protocol.go.

import type { Role } from './http';

export type Lifecycle = 'setup' | 'active' | 'done';
export type NoteColor = 'yellow' | 'pink' | 'blue' | 'green' | 'orange' | 'purple';
export type LinkKind = 'doc' | 'slides' | 'other';

export const NOTE_COLORS: NoteColor[] = ['yellow', 'pink', 'blue', 'green', 'orange', 'purple'];
export const NOTE_W = 180;
export const NOTE_H = 120;
/** Board grid (and snap) step, in board units. */
export const GRID = 20;
/** Smallest region the server accepts, in board units. */
export const MIN_REGION = 100;

export interface User {
  id: string;
  name: string;
  role: Role;
}

export interface Link {
  id: string;
  title: string;
  url: string;
  kind: LinkKind;
}

export interface Note {
  id: string;
  title: string;
  bodyMd: string;
  authorId: string;
  x: number;
  y: number;
  color: NoteColor;
  regionId: string | null;
  voteTotal: number;
  /** Whether this user voted for it (one vote per person per note). */
  voted: boolean;
  starred: boolean;
  hidden?: boolean;
  links: Link[];
  scheduled: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface EventInfo {
  id: string;
  name: string;
  lifecycle: Lifecycle;
  votingOpen: boolean;
  votesPerUser: number;
}

export interface Region {
  id: string;
  label: string;
  x: number;
  y: number;
  w: number;
  h: number;
  color: string;
  z: number;
}

export interface Slot {
  id: string;
  startAt: string;
  endAt: string;
}

export interface Wave {
  id: string;
  name: string;
  status: 'planned' | 'open' | 'locked' | 'done';
  opensAt: string | null;
  slots: Slot[];
}

export interface Room {
  id: string;
  name: string;
  meetUrl: string | null;
}

export interface Assignment {
  id: string;
  noteId: string;
  slotId: string;
  roomId: string;
}

export interface Message {
  id: string;
  authorId: string;
  threadId: string | null;
  body: string;
  hidden?: boolean;
  createdAt: string;
}

export interface Snapshot {
  event: EventInfo;
  users: User[];
  notes: Note[];
  regions: Region[];
  waves: Wave[];
  rooms: Room[];
  assignments: Assignment[];
  messages: Record<string, Message[]>;
  me: { votesRemaining: number; votesUsed: number };
}

/** Events the client applies. Unknown kinds (from later milestones) are ignored. */
export type BoardEvent =
  | { kind: 'note_created'; note: Note }
  | { kind: 'note_updated'; noteId: string; title: string; bodyMd: string; color: NoteColor; updatedAt: string }
  | { kind: 'note_moved'; noteId: string; x: number; y: number; byUserId: string }
  | { kind: 'note_deleted'; noteId: string }
  | { kind: 'links_set'; noteId: string; links: Link[] }
  | { kind: 'user_joined'; user: User }
  | { kind: 'role_set'; userId: string; role: Role }
  | { kind: 'lifecycle_set'; lifecycle: Lifecycle }
  | { kind: 'region_created'; region: Region }
  | { kind: 'region_updated'; region: Region }
  | { kind: 'region_deleted'; regionId: string }
  | { kind: 'note_retagged'; noteId: string; regionId: string | null }
  | { kind: 'note_starred'; noteId: string }
  | { kind: 'note_unstarred'; noteId: string }
  | { kind: 'vote_cast'; noteId: string; byUserId: string; total: number }
  | { kind: 'vote_retracted'; noteId: string; byUserId: string; total: number }
  | { kind: 'voting_set'; open: boolean }
  | { kind: 'votes_per_user_set'; n: number };

export type ServerFrame =
  | { type: 'hello'; you: User; eventSeq: number }
  | { type: 'snapshot'; seq: number; state: Snapshot }
  | { type: 'event'; seq: number; event: BoardEvent | { kind: string } }
  | { type: 'ack'; cmdId: string; seq: number }
  | { type: 'error'; cmdId: string; code: ErrorCode; message: string }
  | { type: 'pong' };

export type ErrorCode = 'bad_request' | 'forbidden' | 'not_allowed_now' | 'rate_limited' | 'internal';

/** Command payloads by name (SPEC §8.2) — the subset implemented so far. */
export interface Commands {
  create_note: { title: string; bodyMd?: string; x: number; y: number; color?: NoteColor };
  update_note: { noteId: string; title?: string; bodyMd?: string; color?: NoteColor };
  move_note: { noteId: string; x: number; y: number };
  delete_note: { noteId: string };
  set_links: { noteId: string; links: { title: string; url: string; kind: LinkKind }[] };
  set_lifecycle: { lifecycle: Lifecycle };
  create_region: { label: string; x: number; y: number; w: number; h: number; color: string; z?: number };
  update_region: {
    regionId: string;
    label?: string;
    x?: number;
    y?: number;
    w?: number;
    h?: number;
    color?: string;
    z?: number;
  };
  delete_region: { regionId: string };
  star_note: { noteId: string };
  unstar_note: { noteId: string };
  cast_vote: { noteId: string };
  retract_vote: { noteId: string };
  set_voting: { open: boolean };
  set_votes_per_user: { n: number };
}
export type CommandName = keyof Commands;
