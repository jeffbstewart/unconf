import { describe, expect, it } from 'vitest';
import { clampZoom, fitBoxes, MAX_ZOOM, MIN_ZOOM, panBy, screenToBoard, zoomAt } from './camera';

describe('camera', () => {
  it('maps screen to board coordinates', () => {
    expect(screenToBoard({ x: 100, y: 50, zoom: 2 }, 40, 20)).toEqual({ x: 120, y: 60 });
  });

  it('pans with the pointer', () => {
    expect(panBy({ x: 0, y: 0, zoom: 2 }, 10, -20)).toEqual({ x: -5, y: 10, zoom: 2 });
  });

  it('zooms about the cursor, keeping the point under it fixed', () => {
    const cam = { x: 30, y: -10, zoom: 1 };
    const before = screenToBoard(cam, 200, 150);
    const zoomed = zoomAt(cam, 200, 150, 1.5);
    expect(zoomed.zoom).toBe(1.5);
    const after = screenToBoard(zoomed, 200, 150);
    expect(after.x).toBeCloseTo(before.x);
    expect(after.y).toBeCloseTo(before.y);
  });

  it('clamps zoom to 0.25×–2×', () => {
    expect(clampZoom(10)).toBe(MAX_ZOOM);
    expect(clampZoom(0.01)).toBe(MIN_ZOOM);
    expect(zoomAt({ x: 0, y: 0, zoom: 1.9 }, 0, 0, 2).zoom).toBe(2);
  });

  it('fits all boxes, centered, without zooming past 100%', () => {
    const cam = fitBoxes([{ x: 0, y: 0, w: 180, h: 120 }], 1000, 800);
    expect(cam.zoom).toBe(1);
    const center = screenToBoard(cam, 500, 400);
    expect(center.x).toBeCloseTo(90);
    expect(center.y).toBeCloseTo(60);

    const wide = fitBoxes([{ x: 0, y: 0, w: 180, h: 120 }, { x: 3820, y: 0, w: 180, h: 120 }], 1000, 800);
    expect(wide.zoom).toBeCloseTo(1000 / 4080);
  });
});
