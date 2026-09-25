import { memo, type PointerEvent } from 'react';
import type { Region } from '../api/protocol';
import { regionFill, type Rect } from './regions';

interface Props {
  region: Region;
  rect: Rect;
  editable: boolean;
  selected: boolean;
  onHeaderPointerDown: (region: Region, e: PointerEvent<HTMLDivElement>) => void;
  onResizePointerDown: (region: Region, e: PointerEvent<HTMLDivElement>) => void;
}

/**
 * A region on the board. Its body lets pointer events through to the board
 * (so panning and double-click-to-create work inside it); moderators grab
 * the header to select/move it and the corner handle to resize it.
 */
export const RegionView = memo(function RegionView({
  region,
  rect,
  editable,
  selected,
  onHeaderPointerDown,
  onResizePointerDown,
}: Props) {
  return (
    <div
      className={`region${selected ? ' region-selected' : ''}`}
      style={{
        transform: `translate(${rect.x}px, ${rect.y}px)`,
        width: rect.w,
        height: rect.h,
        background: regionFill(region.color),
        borderColor: region.color,
      }}
      data-region-id={region.id}
    >
      <div
        className={`region-label${editable ? ' region-label-editable' : ''}`}
        onPointerDown={editable ? (e) => onHeaderPointerDown(region, e) : undefined}
        title={editable ? 'Drag to move · click to edit' : undefined}
      >
        {region.label}
      </div>
      {editable && selected && (
        <div
          className="region-resize"
          onPointerDown={(e) => onResizePointerDown(region, e)}
          title="Drag to resize"
          aria-label="Resize region"
        />
      )}
    </div>
  );
});
