import { useCallback, useEffect, useLayoutEffect, useRef, useState, type PointerEvent } from 'react';
import { NOTE_H, NOTE_W, type Note } from '../api/protocol';
import { canInteract, useStore } from '../store/store';
import { fitBoxes, loadCamera, panBy, saveCamera, screenToBoard, zoomAt, type Camera } from './camera';
import { NoteModal } from './NoteModal';
import { Sticky } from './Sticky';
import './Board.css';

/** Minimum pointer travel (px) before a press on a sticky becomes a drag. */
const DRAG_THRESHOLD = 3;
/** Client-side throttle for live move commands (~15 Hz, SPEC §8.2). */
const MOVE_INTERVAL_MS = 66;

type Interaction =
  | { type: 'pan'; pointerId: number; lastX: number; lastY: number }
  | {
      type: 'note';
      pointerId: number;
      noteId: string;
      startX: number;
      startY: number;
      origX: number;
      origY: number;
      moved: boolean;
      x: number;
      y: number;
      lastSent: number;
      token: number;
    };

interface Ghost {
  x: number;
  y: number;
  token: number;
}

export function Board() {
  const notes = useStore((s) => s.notes);
  const users = useStore((s) => s.users);
  const regions = useStore((s) => s.regions);
  const messages = useStore((s) => s.messages);
  const interactive = useStore(canInteract);
  const send = useStore((s) => s.send);

  const viewportRef = useRef<HTMLDivElement>(null);
  const [cam, setCam] = useState<Camera>(loadCamera);
  const camRef = useRef(cam);
  camRef.current = cam;
  const interaction = useRef<Interaction | null>(null);
  const nextToken = useRef(0);
  /** Optimistic positions of notes being dragged (or awaiting the server's answer). */
  const [ghosts, setGhosts] = useState<Record<string, Ghost>>({});
  const [openNoteId, setOpenNoteId] = useState<string | null>(null);
  const [draft, setDraft] = useState<{ x: number; y: number } | null>(null);

  // Persist the camera, debounced.
  useEffect(() => {
    const t = setTimeout(() => saveCamera(cam), 300);
    return () => clearTimeout(t);
  }, [cam]);

  // Wheel pans; ctrl/⌘-wheel (and trackpad pinch in Chrome) zooms. Needs a
  // non-passive listener to stop the page from scrolling or zooming.
  useLayoutEffect(() => {
    const el = viewportRef.current;
    if (!el) return;
    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      const r = el.getBoundingClientRect();
      if (e.ctrlKey || e.metaKey) {
        // A mouse-wheel notch (deltaY ≈ 100) zooms ~18%; trackpad pinches
        // send many small deltas and stay smooth.
        const factor = Math.exp(-Math.max(-50, Math.min(50, e.deltaY)) * 0.004);
        setCam((c) => zoomAt(c, e.clientX - r.left, e.clientY - r.top, factor));
      } else {
        setCam((c) => panBy(c, -e.deltaX, -e.deltaY));
      }
    };
    // Safari reports trackpad pinch as proprietary gesture events.
    let gestureStart: Camera | null = null;
    const onGestureStart = (e: Event) => {
      e.preventDefault();
      gestureStart = camRef.current;
    };
    const onGestureChange = (e: Event) => {
      e.preventDefault();
      const g = e as Event & { scale: number; clientX: number; clientY: number };
      if (!gestureStart) return;
      const r = el.getBoundingClientRect();
      setCam(zoomAt(gestureStart, g.clientX - r.left, g.clientY - r.top, g.scale));
    };
    el.addEventListener('wheel', onWheel, { passive: false });
    el.addEventListener('gesturestart', onGestureStart);
    el.addEventListener('gesturechange', onGestureChange);
    return () => {
      el.removeEventListener('wheel', onWheel);
      el.removeEventListener('gesturestart', onGestureStart);
      el.removeEventListener('gesturechange', onGestureChange);
    };
  }, []);

  // Close the modal if its note disappears (deleted, or hidden from us).
  useEffect(() => {
    if (openNoteId && !notes[openNoteId]) setOpenNoteId(null);
  }, [notes, openNoteId]);

  const boardPoint = (clientX: number, clientY: number) => {
    const r = viewportRef.current!.getBoundingClientRect();
    return screenToBoard(camRef.current, clientX - r.left, clientY - r.top);
  };

  const onStickyPointerDown = useCallback(
    (note: Note, e: PointerEvent<HTMLDivElement>) => {
      e.stopPropagation();
      if (e.button !== 0 || !interactive || note.hidden) return;
      // Pointer capture waits until the drag starts, so a double-click
      // still reaches the sticky.
      interaction.current = {
        type: 'note',
        pointerId: e.pointerId,
        noteId: note.id,
        startX: e.clientX,
        startY: e.clientY,
        origX: note.x,
        origY: note.y,
        moved: false,
        x: note.x,
        y: note.y,
        lastSent: 0,
        token: ++nextToken.current,
      };
    },
    [interactive],
  );

  const onPointerDown = (e: PointerEvent<HTMLDivElement>) => {
    if (e.button !== 0) return;
    e.currentTarget.setPointerCapture(e.pointerId);
    interaction.current = { type: 'pan', pointerId: e.pointerId, lastX: e.clientX, lastY: e.clientY };
  };

  const onPointerMove = (e: PointerEvent<HTMLDivElement>) => {
    const it = interaction.current;
    if (!it || it.pointerId !== e.pointerId) return;
    if (it.type === 'pan') {
      const dx = e.clientX - it.lastX;
      const dy = e.clientY - it.lastY;
      it.lastX = e.clientX;
      it.lastY = e.clientY;
      setCam((c) => panBy(c, dx, dy));
      return;
    }
    const dxPx = e.clientX - it.startX;
    const dyPx = e.clientY - it.startY;
    if (!it.moved) {
      if (Math.hypot(dxPx, dyPx) < DRAG_THRESHOLD) return;
      it.moved = true;
      viewportRef.current?.setPointerCapture(e.pointerId);
    }
    const zoom = camRef.current.zoom;
    const x = (it.x = it.origX + dxPx / zoom);
    const y = (it.y = it.origY + dyPx / zoom);
    setGhosts((g) => ({ ...g, [it.noteId]: { x, y, token: it.token } }));
    const now = performance.now();
    if (now - it.lastSent >= MOVE_INTERVAL_MS) {
      it.lastSent = now;
      send('move_note', { noteId: it.noteId, x, y }).catch(() => {});
    }
  };

  const onPointerUp = (e: PointerEvent<HTMLDivElement>) => {
    const it = interaction.current;
    if (!it || it.pointerId !== e.pointerId) return;
    interaction.current = null;
    if (it.type !== 'note' || !it.moved) return;
    const { x, y } = it;
    // Keep the ghost until the server answers; its note_moved (with the
    // overlap-resolved position) arrives before the ack.
    const clear = () =>
      setGhosts((g) => {
        if (g[it.noteId]?.token !== it.token) return g;
        const next = { ...g };
        delete next[it.noteId];
        return next;
      });
    send('move_note', { noteId: it.noteId, x, y }).then(clear, clear);
  };

  const onDoubleClick = (e: React.MouseEvent<HTMLDivElement>) => {
    if (!interactive) return;
    const p = boardPoint(e.clientX, e.clientY);
    setDraft({ x: p.x - NOTE_W / 2, y: p.y - NOTE_H / 2 });
  };

  const newAtCenter = () => {
    const r = viewportRef.current!.getBoundingClientRect();
    const p = screenToBoard(camRef.current, r.width / 2, r.height / 2);
    setDraft({ x: p.x - NOTE_W / 2, y: p.y - NOTE_H / 2 });
  };

  const fitAll = () => {
    const r = viewportRef.current!.getBoundingClientRect();
    const boxes = [
      ...Object.values(notes).map((n) => ({ x: n.x, y: n.y, w: NOTE_W, h: NOTE_H })),
      ...regions,
    ];
    setCam(fitBoxes(boxes, r.width, r.height));
  };

  const resetZoom = () => {
    const r = viewportRef.current!.getBoundingClientRect();
    setCam((c) => zoomAt(c, r.width / 2, r.height / 2, 1 / c.zoom));
  };

  const regionColors = Object.fromEntries(regions.map((r) => [r.id, r.color]));
  const gridStep = (cam.zoom >= 0.5 ? 20 : 100) * cam.zoom;

  return (
    <div className="board">
      <div className="board-toolbar">
        {interactive && (
          <button className="primary" onClick={newAtCenter}>
            + New session
          </button>
        )}
        <button onClick={fitAll} title="Fit all notes">
          ⤢ Fit all
        </button>
        <button onClick={resetZoom} title="Reset zoom to 100%">
          {Math.round(cam.zoom * 100)}%
        </button>
      </div>
      {interactive && Object.keys(notes).length === 0 && !draft && (
        <p className="board-empty">Double-click anywhere to propose a session.</p>
      )}
      <div
        ref={viewportRef}
        className="board-viewport"
        style={{
          backgroundSize: `${gridStep}px ${gridStep}px`,
          backgroundPosition: `${-cam.x * cam.zoom}px ${-cam.y * cam.zoom}px`,
        }}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
        onPointerCancel={onPointerUp}
        onDoubleClick={onDoubleClick}
      >
        <div
          className="board-layer"
          style={{ transform: `scale(${cam.zoom}) translate(${-cam.x}px, ${-cam.y}px)` }}
        >
          {[...regions]
            .sort((a, b) => a.z - b.z)
            .map((r) => (
              <div
                key={r.id}
                className="region"
                style={{ transform: `translate(${r.x}px, ${r.y}px)`, width: r.w, height: r.h, background: r.color }}
              >
                <div className="region-label">{r.label}</div>
              </div>
            ))}
          {Object.values(notes).map((n) => {
            const g = ghosts[n.id];
            return (
              <Sticky
                key={n.id}
                note={n}
                x={g ? g.x : n.x}
                y={g ? g.y : n.y}
                authorName={users[n.authorId]?.name ?? '?'}
                chatCount={messages[n.id]?.length ?? 0}
                regionColor={n.regionId ? (regionColors[n.regionId] ?? null) : null}
                dragging={!!g}
                onPointerDown={onStickyPointerDown}
                onOpen={setOpenNoteId}
              />
            );
          })}
          {draft && <DraftNote at={draft} onDone={() => setDraft(null)} />}
        </div>
      </div>
      {openNoteId && notes[openNoteId] && (
        <NoteModal note={notes[openNoteId]} onClose={() => setOpenNoteId(null)} />
      )}
    </div>
  );
}

/** Inline title entry for a new note, placed where it will be created. */
function DraftNote({ at, onDone }: { at: { x: number; y: number }; onDone: () => void }) {
  const send = useStore((s) => s.send);
  const [title, setTitle] = useState('');
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    if (!title.trim()) return onDone();
    setBusy(true);
    try {
      await send('create_note', { title: title.trim(), x: at.x, y: at.y });
      onDone();
    } catch {
      setBusy(false); // the error is already shown as a toast
    }
  };

  return (
    <form
      className="sticky sticky-yellow sticky-draft"
      style={{ transform: `translate(${at.x}px, ${at.y}px)`, width: NOTE_W, height: NOTE_H }}
      onPointerDown={(e) => e.stopPropagation()}
      onDoubleClick={(e) => e.stopPropagation()}
      onSubmit={(e) => {
        e.preventDefault();
        void submit();
      }}
    >
      <textarea
        autoFocus
        placeholder="Session title…"
        maxLength={120}
        value={title}
        disabled={busy}
        onChange={(e) => setTitle(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault();
            void submit();
          }
          if (e.key === 'Escape') onDone();
        }}
        onBlur={() => {
          if (!title.trim()) onDone();
        }}
      />
      <span className="sticky-draft-hint">Enter to add · Esc to cancel</span>
    </form>
  );
}
