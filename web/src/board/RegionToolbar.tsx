import { useEffect, useState } from 'react';
import type { Commands, Region } from '../api/protocol';
import { useStore } from '../store/store';
import { REGION_COLORS } from './regions';

interface Props {
  region: Region;
  /** Screen position (px, relative to the board) of the region's top-left corner. */
  left: number;
  top: number;
  onClose: () => void;
}

/** Moderator controls for the selected region: rename, recolor, restack, delete. */
export function RegionToolbar({ region, left, top, onClose }: Props) {
  const send = useStore((s) => s.send);
  const regions = useStore((s) => s.regions);
  const [label, setLabel] = useState(region.label);
  const [confirmDelete, setConfirmDelete] = useState(false);

  useEffect(() => setLabel(region.label), [region.label]);

  const update = (patch: Omit<Commands['update_region'], 'regionId'>) =>
    send('update_region', { regionId: region.id, ...patch }).catch(() => {});

  const commitLabel = () => {
    if (label.trim() && label.trim() !== region.label) void update({ label: label.trim() });
    else setLabel(region.label);
  };

  const zs = regions.map((r) => r.z);
  return (
    <div
      className="region-toolbar"
      style={{ left, top }}
      onPointerDown={(e) => e.stopPropagation()}
      onDoubleClick={(e) => e.stopPropagation()}
    >
      <input
        aria-label="Region label"
        value={label}
        maxLength={60}
        onChange={(e) => setLabel(e.target.value)}
        onBlur={commitLabel}
        onKeyDown={(e) => {
          if (e.key === 'Enter') (e.target as HTMLInputElement).blur();
          if (e.key === 'Escape') {
            setLabel(region.label);
            onClose();
          }
        }}
      />
      <span className="region-swatches">
        {REGION_COLORS.map((c) => (
          <button
            key={c}
            className={`region-swatch${c === region.color ? ' selected' : ''}`}
            style={{ background: c }}
            aria-label={`Color ${c}`}
            onClick={() => c !== region.color && void update({ color: c })}
          />
        ))}
      </span>
      <button title="Bring to front (wins containment)" onClick={() => void update({ z: Math.max(...zs) + 1 })}>
        ⬆
      </button>
      <button title="Send to back" onClick={() => void update({ z: Math.min(...zs) - 1 })}>
        ⬇
      </button>
      {confirmDelete ? (
        <>
          <button
            className="danger"
            onClick={() => {
              send('delete_region', { regionId: region.id }).then(onClose, () => setConfirmDelete(false));
            }}
          >
            Delete region
          </button>
          <button onClick={() => setConfirmDelete(false)}>Keep</button>
        </>
      ) : (
        <button className="danger-quiet" onClick={() => setConfirmDelete(true)}>
          Delete…
        </button>
      )}
      <button className="icon" aria-label="Done" onClick={onClose}>
        ✕
      </button>
    </div>
  );
}
