// The zustand store: shared board state (fed only by server frames), the
// connection, and transient UI like toasts.

import { create } from 'zustand';
import { fetchMe } from '../api/http';
import type { CommandName, Commands, ServerFrame } from '../api/protocol';
import { CommandError, Connection, type ConnStatus } from '../api/ws';
import { applyFrame, initialBoard, type BoardState } from './reducer';

export interface Toast {
  id: number;
  message: string;
}

interface Store extends BoardState {
  status: ConnStatus;
  toasts: Toast[];
  /** Set when the server no longer accepts our session. */
  loggedOut: boolean;
  connect: () => void;
  disconnect: () => void;
  /** Sends a command; failures are shown as toasts and re-thrown. */
  send: <C extends CommandName>(cmd: C, payload: Commands[C]) => Promise<number>;
  toast: (message: string) => void;
  dismissToast: (id: number) => void;
}

let conn: Connection | null = null;
let toastId = 0;

export const useStore = create<Store>()((set, get) => ({
  ...initialBoard,
  status: 'connecting',
  toasts: [],
  loggedOut: false,

  connect() {
    if (conn) return;
    set({ ...initialBoard, status: 'connecting', loggedOut: false });
    conn = new Connection({
      onFrame: (f: ServerFrame) => {
        set((s) => applyFrame(s, f));
        if (f.type === 'error' && !f.cmdId) get().toast(f.message);
      },
      onStatus: (status) => set({ status }),
      lastSeq: () => get().seq,
      shouldRetry: async () => {
        try {
          if (await fetchMe()) return true;
          set({ loggedOut: true });
          return false;
        } catch {
          return true; // server unreachable: keep trying
        }
      },
    });
    conn.start();
  },

  disconnect() {
    conn?.stop();
    conn = null;
  },

  async send(cmd, payload) {
    try {
      if (!conn) throw new CommandError('disconnected', 'Not connected to the server');
      return await conn.send(cmd, payload);
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      get().toast(message);
      throw err;
    }
  },

  toast(message) {
    const id = ++toastId;
    set((s) => ({ toasts: [...s.toasts, { id, message }] }));
    setTimeout(() => get().dismissToast(id), 5000);
  },

  dismissToast(id) {
    set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) }));
  },
}));

/** Whether the current user may edit/delete the note (author or moderator+). */
export function canEditNote(s: Pick<BoardState, 'you'>, authorId: string): boolean {
  const you = s.you;
  return !!you && (you.id === authorId || you.role !== 'participant');
}

/** Whether the current user may change the board at all right now. */
export function canInteract(s: Pick<BoardState, 'you' | 'event'>): boolean {
  const lc = s.event?.lifecycle;
  if (!s.you || !lc || lc === 'done') return false;
  return lc === 'active' || s.you.role !== 'participant';
}
