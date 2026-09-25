// The "votes are public" notice (shown before a user's first vote). The
// acknowledgement is remembered per user in this browser; if storage is
// unavailable it lasts for the page's lifetime.

import { create } from 'zustand';

const key = (userId: string) => `unconf.voteNoticeAck.${userId}`;
const inMemory = new Set<string>();

export function hasAcknowledged(userId: string): boolean {
  if (inMemory.has(userId)) return true;
  try {
    return localStorage.getItem(key(userId)) === '1';
  } catch {
    return false;
  }
}

export function acknowledge(userId: string) {
  inMemory.add(userId);
  try {
    localStorage.setItem(key(userId), '1');
  } catch {
    // storage unavailable: remembered until reload
  }
}

interface VoteNoticeStore {
  /** Note whose vote is waiting on the notice, if any. */
  pendingNoteId: string | null;
  ask: (noteId: string) => void;
  close: () => void;
}

export const useVoteNotice = create<VoteNoticeStore>()((set) => ({
  pendingNoteId: null,
  ask: (noteId) => set({ pendingNoteId: noteId }),
  close: () => set({ pendingNoteId: null }),
}));
