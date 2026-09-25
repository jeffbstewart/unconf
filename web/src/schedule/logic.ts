// Pure scheduling helpers for the Schedule screen (SPEC §4, §11). The
// conflict metric matches the server's suggester (§4.2).

import type { Assignment, Note, Slot, Wave } from '../api/protocol';

/** Who wants to be in a session: its voters plus its proposer (facilitator). */
export function interest(n: Note): Set<string> {
  return new Set([...n.voters, n.authorId]);
}

export interface SlotConflicts {
  /** Σ over people of (sessions they're interested in here − 1). */
  count: number;
  /** For each session, the others in the slot sharing interest, with the shared count. */
  overlaps: Record<string, { noteId: string; shared: number }[]>;
  /** Sessions whose proposer also proposed another session in this slot. */
  proposerClash: Set<string>;
}

export function slotConflicts(sessions: Note[]): SlotConflicts {
  const perPerson = new Map<string, number>();
  for (const n of sessions) for (const p of interest(n)) perPerson.set(p, (perPerson.get(p) ?? 0) + 1);
  let count = 0;
  for (const k of perPerson.values()) count += Math.max(0, k - 1);

  const overlaps: SlotConflicts['overlaps'] = {};
  const proposerClash = new Set<string>();
  for (const a of sessions) {
    const ia = interest(a);
    overlaps[a.id] = [];
    for (const b of sessions) {
      if (a.id === b.id) continue;
      let shared = 0;
      for (const p of interest(b)) if (ia.has(p)) shared++;
      if (shared > 0) overlaps[a.id].push({ noteId: b.id, shared });
      if (a.authorId === b.authorId) proposerClash.add(a.id);
    }
    overlaps[a.id].sort((x, y) => y.shared - x.shared || x.noteId.localeCompare(y.noteId));
  }
  return { count, overlaps, proposerClash };
}

/** The notes assigned in a slot, by track. */
export function slotSessions(slotId: string, assignments: Assignment[], notes: Record<string, Note>): Note[] {
  return assignments
    .filter((a) => a.slotId === slotId && notes[a.noteId])
    .sort((a, b) => a.track - b.track)
    .map((a) => notes[a.noteId]);
}

export interface Pick {
  voted: boolean;
  proposed: boolean;
  starred: boolean;
}

export function pickOf(n: Note, youId: string | undefined): Pick {
  return { voted: n.voted, proposed: !!youId && n.authorId === youId, starred: n.starred };
}

export function isPick(p: Pick): boolean {
  return p.voted || p.proposed || p.starred;
}

/** Your picks in a slot, when there's more than one (you can't be in two places). */
export function myClash(sessions: Note[], youId: string | undefined): Note[] {
  const mine = sessions.filter((n) => isPick(pickOf(n, youId)));
  return mine.length > 1 ? mine : [];
}

export interface TimedSlot {
  wave: Wave;
  slot: Slot;
}

/**
 * The slot running at `now` and the next one to start, across waves whose
 * schedule is visible (open drafts included). Planned waves have no sessions.
 */
export function nowAndNext(waves: Wave[], now: number): { current: TimedSlot | null; next: TimedSlot | null } {
  let current: TimedSlot | null = null;
  let next: TimedSlot | null = null;
  for (const wave of waves) {
    if (wave.status === 'planned') continue;
    for (const slot of wave.slots) {
      const start = Date.parse(slot.startAt);
      const end = Date.parse(slot.endAt);
      if (start <= now && now < end && !current) current = { wave, slot };
      if (start > now && (!next || start < Date.parse(next.slot.startAt))) next = { wave, slot };
    }
  }
  return { current, next };
}

/** Wave id for each slot id. */
export function slotWaves(waves: Wave[]): Record<string, Wave> {
  const m: Record<string, Wave> = {};
  for (const w of waves) for (const sl of w.slots) m[sl.id] = w;
  return m;
}

export interface Pool {
  /** Unscheduled, at or above the threshold, most voted first. */
  eligible: Note[];
  /** Unscheduled, below the threshold. */
  below: Note[];
  /** Already run in a locked/done wave and not in this wave: possible repeats. */
  repeats: Note[];
}

const byVoters = (a: Note, b: Note) =>
  b.voteTotal - a.voteTotal || a.createdAt.localeCompare(b.createdAt) || a.id.localeCompare(b.id);

/** The pool of sessions a scheduler can drag into `waveId`. */
export function pool(
  notes: Record<string, Note>,
  assignments: Assignment[],
  waves: Wave[],
  waveId: string,
  threshold: number,
): Pool {
  const waveOf = slotWaves(waves);
  const inWave = new Set<string>();
  const inClosed = new Set<string>();
  for (const a of assignments) {
    const w = waveOf[a.slotId];
    if (!w) continue;
    if (w.id === waveId) inWave.add(a.noteId);
    if (w.status === 'locked' || w.status === 'done') inClosed.add(a.noteId);
  }
  const out: Pool = { eligible: [], below: [], repeats: [] };
  for (const n of Object.values(notes)) {
    if (n.hidden || inWave.has(n.id)) continue;
    if (inClosed.has(n.id)) out.repeats.push(n);
    else if (!n.scheduled) (n.voteTotal >= threshold ? out.eligible : out.below).push(n);
  }
  out.eligible.sort(byVoters);
  out.below.sort(byVoters);
  out.repeats.sort(byVoters);
  return out;
}

/** "10:00–10:45", in the viewer's time zone. */
export function slotLabel(slot: Slot): string {
  const f = (iso: string) => new Date(iso).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
  return `${f(slot.startAt)}–${f(slot.endAt)}`;
}

/** Whether a note is scheduled in a locked or done wave: its votes are history (SPEC §4.1). */
export function inClosedWave(noteId: string, waves: Wave[], assignments: Assignment[]): boolean {
  const closed = new Set(waves.filter((w) => w.status === 'locked' || w.status === 'done').flatMap((w) => w.slots.map((sl) => sl.id)));
  return assignments.some((a) => a.noteId === noteId && closed.has(a.slotId));
}
