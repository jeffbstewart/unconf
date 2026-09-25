import { NO_REGION, filtersActive, useView, type SortKey } from '../store/view';
import { useStore } from '../store/store';
import './ViewBar.css';

const SORTS: { key: SortKey; label: string }[] = [
  { key: 'ranking', label: 'Ranking (votes)' },
  { key: 'newest', label: 'Newest' },
  { key: 'mine', label: 'Authored by me first' },
  { key: 'starred', label: 'Starred first' },
];

/** Personal view controls: layout, filters, sort, snap. Nothing here is shared. */
export function ViewBar() {
  const v = useView();
  const regions = useStore((s) => s.regions);
  const f = v.filters;

  const chip = (key: 'mine' | 'myVotes' | 'starred' | 'unscheduled', label: string) => (
    <button
      className={`chip${f[key] ? ' on' : ''}`}
      aria-pressed={f[key]}
      onClick={() => v.setFilter(key, !f[key])}
    >
      {label}
    </button>
  );

  const toggleRegion = (id: string) =>
    v.setFilter('regions', f.regions.includes(id) ? f.regions.filter((r) => r !== id) : [...f.regions, id]);
  const regionOptions = [...regions].sort((a, b) => a.label.localeCompare(b.label));

  return (
    <div className="viewbar" role="toolbar" aria-label="Personal view">
      <span className="segmented" role="group" aria-label="Layout">
        <button className={v.layout === 'board' ? 'on' : ''} aria-pressed={v.layout === 'board'} onClick={() => v.set({ layout: 'board' })}>
          Board
        </button>
        <button className={v.layout === 'list' ? 'on' : ''} aria-pressed={v.layout === 'list'} onClick={() => v.set({ layout: 'list' })}>
          List
        </button>
      </span>
      <span className="viewbar-sep" />
      {chip('mine', 'My stickies')}
      {chip('myVotes', 'My votes')}
      {chip('starred', '★ Starred')}
      {chip('unscheduled', 'Unscheduled')}
      <details className="region-filter">
        <summary className={`chip${f.regions.length ? ' on' : ''}`}>
          Region{f.regions.length ? ` (${f.regions.length})` : ''} ▾
        </summary>
        <div className="region-filter-menu">
          {regionOptions.map((r) => (
            <label key={r.id}>
              <input type="checkbox" checked={f.regions.includes(r.id)} onChange={() => toggleRegion(r.id)} />
              <span className="swatch-dot" style={{ background: r.color }} /> {r.label}
            </label>
          ))}
          <label>
            <input type="checkbox" checked={f.regions.includes(NO_REGION)} onChange={() => toggleRegion(NO_REGION)} />
            <span className="swatch-dot" /> No region
          </label>
        </div>
      </details>
      <input
        className="viewbar-search"
        type="search"
        placeholder="Search…"
        aria-label="Search notes"
        value={f.search}
        onChange={(e) => v.setFilter('search', e.target.value)}
      />
      {filtersActive(f) && (
        <>
          <span className="segmented" role="group" aria-label="Non-matching notes">
            <button className={v.mode === 'dim' ? 'on' : ''} aria-pressed={v.mode === 'dim'} onClick={() => v.set({ mode: 'dim' })}>
              Dim
            </button>
            <button className={v.mode === 'hide' ? 'on' : ''} aria-pressed={v.mode === 'hide'} onClick={() => v.set({ mode: 'hide' })}>
              Hide
            </button>
          </span>
          <button className="chip" onClick={v.clearFilters}>
            Clear
          </button>
        </>
      )}
      <span className="viewbar-spacer" />
      <label className="viewbar-sort">
        Sort
        <select value={v.sort} onChange={(e) => v.set({ sort: e.target.value as SortKey })}>
          {SORTS.map((s) => (
            <option key={s.key} value={s.key}>
              {s.label}
            </option>
          ))}
        </select>
      </label>
      <button className={`chip${v.snap ? ' on' : ''}`} aria-pressed={v.snap} onClick={() => v.set({ snap: !v.snap })} title="Snap to grid">
        ⊞ Snap
      </button>
    </div>
  );
}
