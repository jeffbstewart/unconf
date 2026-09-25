import { useCallback, useEffect, useLayoutEffect, useRef, useState, type PointerEvent } from 'react';
import { MIN_REGION, NOTE_H, NOTE_W, type Note, type Region } from '../api/protocol';
import { canInteract, useStore } from '../store/store';
import { matches, snap, useView } from '../store/view';
import { fitBoxes, loadCamera, panBy, saveCamera, screenToBoard, zoomAt, type Camera } from './camera';
import { REGION_COLORS, rectFromPoints, type Rect } from './regions';
import { RegionToolbar } from './RegionToolbar';
import { RegionView } from './RegionView';
import { Sticky } from './Sticky';
import './Board.css';

/** Minimum pointer travel (px) before a press becomes a drag. */
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
    }
  | { type: 'draw'; pointerId: number; x0: number; y0: number; rect: Rect }
  | {
      type: 'region';
      mode: 'move' | 'resize';
      pointerId: number;
      regionId: string;
      startX: number;
      startY: number;
      orig: Rect;
      moved: boolean;
      rect: Rect;
      token: number;
    };

interface Ghost {
  x: number;
  y: number;
  token: number;
}

interface RegionGhost {
  rect: Rect;
  token: number;
}

/** Returns a callback that drops id's ghost if it still belongs to token. */
function clearGhost<T extends { token: number }>(
  setter: React.Dispatch<React.SetStateAction<Record<string, T>>>,
  id: string,
  token: number,
) {
  return () =>
    setter((g) => {
      if (g[id]?.token !== token) return g;
      const next = { ...g };
      delete next[id];
      return next;
    });
}

export function Board({ onOpen }: { onOpen: (noteId: string) => void }) {
  const notes = useStore((s) => s.notes);
  const users = useStore((s) => s.users);
  const regions = useStore((s) => s.regions);
  const messages = useStore((s) => s.messages);
  const you = useStore((s) => s.you);
  const interactive = useStore(canInteract);
  const send = useStore((s) => s.send);
  const toast = useStore((s) => s.toast);
  const filters = useView((s) => s.filters);
  const mode = useView((s) => s.mode);
  const snapOn = useView((s) => s.snap);
  const isMod = !!you && you.role !== 'participant';
  const editRegions = isMod && interactive;

  const viewportRef = useRef<HTMLDivElement>(null);
  const [cam, setCam] = useState<Camera>(loadCamera);
  const camRef = useRef(cam);
  camRef.current = cam;
  const interaction = useRef<Interaction | null>(null);
  const nextToken = useRef(0);
  /** Optimistic positions of notes being dragged (or awaiting the server's answer). */
  const [ghosts, setGhosts] = useState<Record<string, Ghost>>({});
  const [regionGhosts, setRegionGhosts] = useState<Record<string, RegionGhost>>({});
  // Drafts carry an id: a quick second double-click must not be cleared by
  // the first draft's (late) completion.
  const [draft, setDraft] = useState<{ x: number; y: number; id: number } | null>(null);
  const [drawMode, setDrawMode] = useState(false);
  const [drawing, setDrawing] = useState<Rect | null>(null);
  const [regionDraft, setRegionDraft] = useState<(Rect & { id: number }) | null>(null);
  const [selectedRegion, setSelectedRegion] = useState<string | null>(null);

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

  // Drop region tools when the user can no longer edit regions, and the
  // selection when its region disappears.
  useEffect(() => {
    if (!editRegions) {
      setDrawMode(false);
      setSelectedRegion(null);
      setRegionDraft(null);
    }
  }, [editRegions]);
  useEffect(() => {
    if (selectedRegion && !regions.some((r) => r.id === selectedRegion)) setSelectedRegion(null);
  }, [regions, selectedRegion]);

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

  const onStar = useCallback(
    (note: Note) => {
      send(note.starred ? 'unstar_note' : 'star_note', { noteId: note.id }).catch(() => {});
    },
    [send],
  );

  const onRegionPointerDown = useCallback((mode: 'move' | 'resize', region: Region, e: PointerEvent<HTMLDivElement>) => {
    e.stopPropagation();
    if (e.button !== 0) return;
    setSelectedRegion(region.id);
    const orig = { x: region.x, y: region.y, w: region.w, h: region.h };
    interaction.current = {
      type: 'region',
      mode,
      pointerId: e.pointerId,
      regionId: region.id,
      startX: e.clientX,
      startY: e.clientY,
      orig,
      moved: false,
      rect: orig,
      token: ++nextToken.current,
    };
  }, []);
  const onRegionHeaderPointerDown = useCallback(
    (r: Region, e: PointerEvent<HTMLDivElement>) => onRegionPointerDown('move', r, e),
    [onRegionPointerDown],
  );
  const onRegionResizePointerDown = useCallback(
    (r: Region, e: PointerEvent<HTMLDivElement>) => onRegionPointerDown('resize', r, e),
    [onRegionPointerDown],
  );

  const onPointerDown = (e: PointerEvent<HTMLDivElement>) => {
    if (e.button !== 0) return;
    e.currentTarget.setPointerCapture(e.pointerId);
    setSelectedRegion(null);
    if (drawMode) {
      const p = boardPoint(e.clientX, e.clientY);
      const x0 = snap(p.x, snapOn);
      const y0 = snap(p.y, snapOn);
      const rect = { x: x0, y: y0, w: 0, h: 0 };
      interaction.current = { type: 'draw', pointerId: e.pointerId, x0, y0, rect };
      setDrawing(rect);
      return;
    }
    interaction.current = { type: 'pan', pointerId: e.pointerId, lastX: e.clientX, lastY: e.clientY };
  };

  const onPointerMove = (e: PointerEvent<HTMLDivElement>) => {
    const it = interaction.current;
    if (!it || it.pointerId !== e.pointerId) return;
    const zoom = camRef.current.zoom;
    switch (it.type) {
      case 'pan': {
        const dx = e.clientX - it.lastX;
        const dy = e.clientY - it.lastY;
        it.lastX = e.clientX;
        it.lastY = e.clientY;
        setCam((c) => panBy(c, dx, dy));
        return;
      }
      case 'draw': {
        const p = boardPoint(e.clientX, e.clientY);
        it.rect = rectFromPoints(it.x0, it.y0, snap(p.x, snapOn), snap(p.y, snapOn));
        setDrawing(it.rect);
        return;
      }
      case 'note': {
        const dxPx = e.clientX - it.startX;
        const dyPx = e.clientY - it.startY;
        if (!it.moved) {
          if (Math.hypot(dxPx, dyPx) < DRAG_THRESHOLD) return;
          it.moved = true;
          viewportRef.current?.setPointerCapture(e.pointerId);
        }
        const x = (it.x = snap(it.origX + dxPx / zoom, snapOn));
        const y = (it.y = snap(it.origY + dyPx / zoom, snapOn));
        setGhosts((g) => ({ ...g, [it.noteId]: { x, y, token: it.token } }));
        const now = performance.now();
        if (now - it.lastSent >= MOVE_INTERVAL_MS) {
          it.lastSent = now;
          send('move_note', { noteId: it.noteId, x, y }).catch(() => {});
        }
        return;
      }
      case 'region': {
        const dxPx = e.clientX - it.startX;
        const dyPx = e.clientY - it.startY;
        if (!it.moved) {
          if (Math.hypot(dxPx, dyPx) < DRAG_THRESHOLD) return;
          it.moved = true;
          viewportRef.current?.setPointerCapture(e.pointerId);
        }
        const o = it.orig;
        const rect =
          it.mode === 'move'
            ? { ...o, x: snap(o.x + dxPx / zoom, snapOn), y: snap(o.y + dyPx / zoom, snapOn) }
            : {
                ...o,
                w: Math.max(MIN_REGION, snap(o.w + dxPx / zoom, snapOn)),
                h: Math.max(MIN_REGION, snap(o.h + dyPx / zoom, snapOn)),
              };
        it.rect = rect;
        setRegionGhosts((g) => ({ ...g, [it.regionId]: { rect, token: it.token } }));
        return;
      }
    }
  };

  const onPointerUp = (e: PointerEvent<HTMLDivElement>) => {
    const it = interaction.current;
    if (!it || it.pointerId !== e.pointerId) return;
    interaction.current = null;
    switch (it.type) {
      case 'note': {
        if (!it.moved) return;
        // Keep the ghost until the server answers; its note_moved (with the
        // overlap-resolved position) arrives before the ack.
        const clear = clearGhost(setGhosts, it.noteId, it.token);
        send('move_note', { noteId: it.noteId, x: it.x, y: it.y }).then(clear, clear);
        return;
      }
      case 'draw': {
        setDrawing(null);
        setDrawMode(false);
        if (it.rect.w < MIN_REGION || it.rect.h < MIN_REGION) {
          toast(`Drag out a region at least ${MIN_REGION}×${MIN_REGION}.`);
          return;
        }
        setRegionDraft({ ...it.rect, id: ++nextToken.current });
        return;
      }
      case 'region': {
        if (!it.moved) return; // a click just selects
        const { x, y, w, h } = it.rect;
        const patch = it.mode === 'move' ? { x, y } : { w, h };
        const clear = clearGhost(setRegionGhosts, it.regionId, it.token);
        send('update_region', { regionId: it.regionId, ...patch }).then(clear, clear);
        return;
      }
      default:
        return;
    }
  };

  const onDoubleClick = (e: React.MouseEvent<HTMLDivElement>) => {
    if (!interactive || drawMode) return;
    const p = boardPoint(e.clientX, e.clientY);
    setDraft({ x: snap(p.x - NOTE_W / 2, snapOn), y: snap(p.y - NOTE_H / 2, snapOn), id: ++nextToken.current });
  };

  const newAtCenter = () => {
    const r = viewportRef.current!.getBoundingClientRect();
    const p = screenToBoard(camRef.current, r.width / 2, r.height / 2);
    setDraft({ x: snap(p.x - NOTE_W / 2, snapOn), y: snap(p.y - NOTE_H / 2, snapOn), id: ++nextToken.current });
  };

  const fitAll = () => {
    const r = viewportRef.current!.getBoundingClientRect();
    const boxes = [...Object.values(notes).map((n) => ({ x: n.x, y: n.y, w: NOTE_W, h: NOTE_H })), ...regions];
    setCam(fitBoxes(boxes, r.width, r.height));
  };

  const resetZoom = () => {
    const r = viewportRef.current!.getBoundingClientRect();
    setCam((c) => zoomAt(c, r.width / 2, r.height / 2, 1 / c.zoom));
  };

  const regionColors = Object.fromEntries(regions.map((r) => [r.id, r.color]));
  const gridStep = (cam.zoom >= 0.5 ? 20 : 100) * cam.zoom;
  const selected = regions.find((r) => r.id === selectedRegion);

  return (
    <div className="board">
      <div className="board-toolbar">
        {interactive && (
          <button className="primary" onClick={newAtCenter}>
            + New session
          </button>
        )}
        {editRegions && (
          <button
            className={drawMode ? 'toggled' : undefined}
            aria-pressed={drawMode}
            onClick={() => setDrawMode((d) => !d)}
            title="Drag on the board to draw a region"
          >
            ▭ Draw region
          </button>
        )}
        <button onClick={fitAll} title="Fit all notes">
          ⤢ Fit all
        </button>
        <button onClick={resetZoom} title="Reset zoom to 100%">
          {Math.round(cam.zoom * 100)}%
        </button>
      </div>
      {interactive && Object.keys(notes).length === 0 && !draft && !drawMode && (
        <p className="board-empty">Double-click anywhere to propose a session.</p>
      )}
      <div
        ref={viewportRef}
        className={`board-viewport${drawMode ? ' board-drawing' : ''}`}
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
        <div className="board-layer" style={{ transform: `scale(${cam.zoom}) translate(${-cam.x}px, ${-cam.y}px)` }}>
          {[...regions]
            .sort((a, b) => a.z - b.z)
            .map((r) => (
              <RegionView
                key={r.id}
                region={r}
                rect={regionGhosts[r.id]?.rect ?? r}
                editable={editRegions}
                selected={r.id === selectedRegion}
                onHeaderPointerDown={onRegionHeaderPointerDown}
                onResizePointerDown={onRegionResizePointerDown}
              />
            ))}
          {drawing && (
            <div
              className="region region-drawing"
              style={{ transform: `translate(${drawing.x}px, ${drawing.y}px)`, width: drawing.w, height: drawing.h }}
            />
          )}
          {regionDraft && (
            <DraftRegion
              key={regionDraft.id}
              rect={regionDraft}
              onDone={() => setRegionDraft((d) => (d?.id === regionDraft.id ? null : d))}
            />
          )}
          {Object.values(notes).map((n) => {
            const match = matches(n, filters, you?.id);
            if (!match && mode === 'hide') return null;
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
                dimmed={!match}
                onPointerDown={onStickyPointerDown}
                onOpen={onOpen}
                onStar={onStar}
              />
            );
          })}
          {draft && (
            <DraftNote key={draft.id} at={draft} onDone={() => setDraft((d) => (d?.id === draft.id ? null : d))} />
          )}
        </div>
      </div>
      {selected && editRegions && (
        <RegionToolbar
          key={selected.id}
          region={selected}
          left={Math.max(8, (selected.x - cam.x) * cam.zoom)}
          top={Math.max(56, (selected.y - cam.y) * cam.zoom - 44)}
          onClose={() => setSelectedRegion(null)}
        />
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

/** Label entry for a freshly drawn region; the region is created on Enter. */
function DraftRegion({ rect, onDone }: { rect: Rect; onDone: () => void }) {
  const send = useStore((s) => s.send);
  const regionCount = useStore((s) => s.regions.length);
  const [label, setLabel] = useState('');
  const [busy, setBusy] = useState(false);
  const color = REGION_COLORS[regionCount % REGION_COLORS.length];

  const submit = async () => {
    if (!label.trim()) return onDone();
    setBusy(true);
    try {
      await send('create_region', { label: label.trim(), x: rect.x, y: rect.y, w: rect.w, h: rect.h, color });
      onDone();
    } catch {
      setBusy(false);
    }
  };

  return (
    <form
      className="region region-draft"
      style={{ transform: `translate(${rect.x}px, ${rect.y}px)`, width: rect.w, height: rect.h, borderColor: color }}
      onPointerDown={(e) => e.stopPropagation()}
      onDoubleClick={(e) => e.stopPropagation()}
      onSubmit={(e) => {
        e.preventDefault();
        void submit();
      }}
    >
      <input
        autoFocus
        placeholder="Region label, then Enter…"
        maxLength={60}
        value={label}
        disabled={busy}
        onChange={(e) => setLabel(e.target.value)}
        onKeyDown={(e) => e.key === 'Escape' && onDone()}
        onBlur={() => !label.trim() && onDone()}
      />
    </form>
  );
}
