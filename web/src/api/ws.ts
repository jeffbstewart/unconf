// WebSocket connection with reconnect (SPEC §8.1): resumes from the last
// applied seq, backing off 1s → 30s between attempts.

import type { CommandName, Commands, ErrorCode, ServerFrame } from './protocol';

export type ConnStatus = 'connecting' | 'open' | 'reconnecting';

export class CommandError extends Error {
  constructor(
    readonly code: ErrorCode | 'disconnected',
    message: string,
  ) {
    super(message);
  }
}

/** Delay before reconnect attempt n (0-based): 1s, 2s, 4s, … capped at 30s. */
export function backoffDelay(attempt: number): number {
  return Math.min(30_000, 1000 * 2 ** attempt);
}

interface Pending {
  resolve: (seq: number) => void;
  reject: (err: CommandError) => void;
}

export interface ConnectionHandlers {
  onFrame: (f: ServerFrame) => void;
  onStatus: (s: ConnStatus) => void;
  /** The seq to resume from on (re)connect. */
  lastSeq: () => number;
  /** Called when a connection attempt fails before hello; resolve false to stop (e.g. logged out). */
  shouldRetry: () => Promise<boolean>;
}

export class Connection {
  private ws: WebSocket | null = null;
  private attempt = 0;
  private timer: ReturnType<typeof setTimeout> | undefined;
  private stopped = false;
  private nextId = 0;
  private pending = new Map<string, Pending>();

  constructor(private readonly h: ConnectionHandlers) {}

  start() {
    this.stopped = false;
    this.open();
  }

  stop() {
    this.stopped = true;
    clearTimeout(this.timer);
    this.ws?.close();
    this.ws = null;
    this.failPending();
  }

  /** Sends a command; resolves with the seq on ack, rejects on error. */
  send<C extends CommandName>(cmd: C, payload: Commands[C]): Promise<number> {
    const ws = this.ws;
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      return Promise.reject(new CommandError('disconnected', 'Not connected to the server'));
    }
    const cmdId = `c-${++this.nextId}`;
    ws.send(JSON.stringify({ type: 'cmd', cmdId, cmd, payload }));
    return new Promise((resolve, reject) => this.pending.set(cmdId, { resolve, reject }));
  }

  private open() {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const ws = new WebSocket(`${proto}//${location.host}/ws?since=${this.h.lastSeq()}`);
    this.ws = ws;
    let greeted = false;
    this.h.onStatus(this.attempt === 0 ? 'connecting' : 'reconnecting');

    ws.onmessage = (msg) => {
      let f: ServerFrame;
      try {
        f = JSON.parse(msg.data as string) as ServerFrame;
      } catch {
        return;
      }
      if (f.type === 'hello') {
        greeted = true;
        this.attempt = 0;
        this.h.onStatus('open');
      }
      if (f.type === 'ack' || f.type === 'error') {
        const p = this.pending.get(f.cmdId);
        if (p) {
          this.pending.delete(f.cmdId);
          if (f.type === 'ack') p.resolve(f.seq);
          else p.reject(new CommandError(f.code, f.message));
        }
      }
      this.h.onFrame(f);
    };

    ws.onclose = () => {
      if (this.ws !== ws) return; // superseded or stopped
      this.ws = null;
      this.failPending();
      if (this.stopped) return;
      this.h.onStatus('reconnecting');
      const retry = greeted ? Promise.resolve(true) : this.h.shouldRetry();
      void retry.then((ok) => {
        if (!ok || this.stopped) return;
        this.timer = setTimeout(() => this.open(), backoffDelay(this.attempt++));
      });
    };
  }

  private failPending() {
    for (const p of this.pending.values()) {
      p.reject(new CommandError('disconnected', 'Connection lost'));
    }
    this.pending.clear();
  }
}
