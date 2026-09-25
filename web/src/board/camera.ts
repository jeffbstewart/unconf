// Client-local board camera (SPEC §9, §11). The camera is the board
// coordinate shown at the viewport's top-left corner, plus a zoom factor.

export interface Camera {
  x: number;
  y: number;
  zoom: number;
}

export const MIN_ZOOM = 0.25;
export const MAX_ZOOM = 2;
export const DEFAULT_CAMERA: Camera = { x: -40, y: -40, zoom: 1 };

export function clampZoom(z: number): number {
  return Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, z));
}

/** Converts a viewport point (px) to board coordinates. */
export function screenToBoard(cam: Camera, sx: number, sy: number): { x: number; y: number } {
  return { x: cam.x + sx / cam.zoom, y: cam.y + sy / cam.zoom };
}

/** Pans by a viewport-pixel delta (content follows the pointer). */
export function panBy(cam: Camera, dx: number, dy: number): Camera {
  return { ...cam, x: cam.x - dx / cam.zoom, y: cam.y - dy / cam.zoom };
}

/** Zooms by factor, keeping the board point under viewport (sx, sy) fixed. */
export function zoomAt(cam: Camera, sx: number, sy: number, factor: number): Camera {
  const zoom = clampZoom(cam.zoom * factor);
  const anchor = screenToBoard(cam, sx, sy);
  return { zoom, x: anchor.x - sx / zoom, y: anchor.y - sy / zoom };
}

export interface Box {
  x: number;
  y: number;
  w: number;
  h: number;
}

/** A camera showing every box in a viewport of the given size, with a margin. */
export function fitBoxes(boxes: Box[], viewW: number, viewH: number, margin = 40): Camera {
  if (boxes.length === 0 || viewW <= 0 || viewH <= 0) return DEFAULT_CAMERA;
  const minX = Math.min(...boxes.map((b) => b.x));
  const minY = Math.min(...boxes.map((b) => b.y));
  const maxX = Math.max(...boxes.map((b) => b.x + b.w));
  const maxY = Math.max(...boxes.map((b) => b.y + b.h));
  const w = maxX - minX + 2 * margin;
  const h = maxY - minY + 2 * margin;
  const zoom = clampZoom(Math.min(viewW / w, viewH / h, 1));
  // Center the content in the viewport.
  return {
    zoom,
    x: (minX + maxX) / 2 - viewW / zoom / 2,
    y: (minY + maxY) / 2 - viewH / zoom / 2,
  };
}

const STORAGE_KEY = 'unconf.camera';

export function loadCamera(): Camera {
  try {
    const c = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? 'null') as Camera | null;
    if (c && [c.x, c.y, c.zoom].every(Number.isFinite)) return { ...c, zoom: clampZoom(c.zoom) };
  } catch {
    // unavailable or corrupt storage: fall through
  }
  return DEFAULT_CAMERA;
}

export function saveCamera(c: Camera) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(c));
  } catch {
    // storage unavailable (private mode, quota): the camera just won't persist
  }
}
