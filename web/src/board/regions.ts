// Region tints: stored as #rrggbb, drawn translucent behind the notes.

export const REGION_COLORS = ['#f2d45c', '#7fd18b', '#7fb3f0', '#f09ac0', '#b59cf0', '#f5a962', '#b8bec6'];

/** Background fill for a region (its tint at ~30% opacity). */
export function regionFill(color: string): string {
  return `${color}4d`;
}

export interface Rect {
  x: number;
  y: number;
  w: number;
  h: number;
}

/** Normalizes a rectangle dragged from (x1,y1) to (x2,y2). */
export function rectFromPoints(x1: number, y1: number, x2: number, y2: number): Rect {
  return { x: Math.min(x1, x2), y: Math.min(y1, y2), w: Math.abs(x2 - x1), h: Math.abs(y2 - y1) };
}
