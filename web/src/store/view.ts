// Personal view settings (SPEC §11): filters, sort, snap-to-grid, and
// board/list layout. Pure client state — persisted in localStorage, never
// sent to the server.

import { create } from 'zustand';
import { GRID, type Note, type Region } from '../api/protocol';

export type SortKey = 'ranking' | 'newest' | 'mine' | 'starred';
export type FilterMode = 'dim' | 'hide';
export type Layout = 'board' | 'list';

/** Region filter value for notes outside every region. */
export const NO_REGION = '__none__';

export interface Filters {
  mine: boolean;
  myVotes: boolean;
  starred: boolean;
  unscheduled: boolean;
  /** Region ids (or NO_REGION); empty = any region. */
  regions: string[];
  search: string;
}

export interface ViewSettings {
  filters: Filters;
  mode: FilterMode;
  sort: SortKey;
  snap: boolean;
  layout: Layout;
}

export const DEFAULT_VIEW: ViewSettings = {
  filters: { mine: false, myVotes: false, starred: false, unscheduled: false, regions: [], search: '' },
  mode: 'dim',
  sort: 'ranking',
  snap: false,
  layout: 'board',
};

export function filtersActive(f: Filters): boolean {
  return f.mine || f.myVotes || f.starred || f.unscheduled || f.regions.length > 0 || f.search.trim() !== '';
}

/** Whether a note passes every active filter. */
export function matches(n: Note, f: Filters, youId: string | undefined): boolean {
  if (f.mine && n.authorId !== youId) return false;
  if (f.myVotes && n.myVotes === 0) return false;
  if (f.starred && !n.starred) return false;
  if (f.unscheduled && n.scheduled) return false;
  if (f.regions.length > 0 && !f.regions.includes(n.regionId ?? NO_REGION)) return false;
  const q = f.search.trim().toLowerCase();
  if (q && !n.title.toLowerCase().includes(q) && !n.bodyMd.toLowerCase().includes(q)) return false;
  return true;
}

const byRanking = (a: Note, b: Note) =>
  b.voteTotal - a.voteTotal || a.createdAt.localeCompare(b.createdAt) || a.id.localeCompare(b.id);

/** Returns notes ordered by the chosen sort (a new array). */
export function sortNotes(notes: Note[], sort: SortKey, youId: string | undefined): Note[] {
  const out = [...notes];
  switch (sort) {
    case 'ranking':
      return out.sort(byRanking);
    case 'newest':
      return out.sort((a, b) => b.createdAt.localeCompare(a.createdAt) || b.id.localeCompare(a.id));
    case 'mine':
      return out.sort((a, b) => Number(b.authorId === youId) - Number(a.authorId === youId) || byRanking(a, b));
    case 'starred':
      return out.sort((a, b) => Number(b.starred) - Number(a.starred) || byRanking(a, b));
  }
}

export interface RegionGroup {
  region: Region | null;
  notes: Note[];
}

/**
 * Groups notes by region for the list view: regions in board reading order
 * (top to bottom, then left to right), untagged notes last. Empty groups
 * are dropped. Notes keep their incoming order within a group.
 */
export function groupByRegion(notes: Note[], regions: Region[]): RegionGroup[] {
  const ordered = [...regions].sort((a, b) => a.y - b.y || a.x - b.x || a.label.localeCompare(b.label));
  const groups = new Map<string, RegionGroup>(ordered.map((r) => [r.id, { region: r, notes: [] }]));
  const none: RegionGroup = { region: null, notes: [] };
  for (const n of notes) (groups.get(n.regionId ?? '') ?? none).notes.push(n);
  return [...groups.values(), none].filter((g) => g.notes.length > 0);
}

/** Quantizes a board coordinate to the grid when snapping is on. */
export function snap(v: number, on: boolean): number {
  return on ? Math.round(v / GRID) * GRID : v;
}

const STORAGE_KEY = 'unconf.view';

function load(): ViewSettings {
  try {
    const v = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? 'null') as Partial<ViewSettings> | null;
    if (v && typeof v === 'object') {
      return { ...DEFAULT_VIEW, ...v, filters: { ...DEFAULT_VIEW.filters, ...v.filters } };
    }
  } catch {
    // unavailable or corrupt storage: defaults
  }
  return DEFAULT_VIEW;
}

interface ViewStore extends ViewSettings {
  setFilter: <K extends keyof Filters>(key: K, value: Filters[K]) => void;
  clearFilters: () => void;
  set: (patch: Partial<Omit<ViewSettings, 'filters'>>) => void;
}

export const useView = create<ViewStore>()((set) => ({
  ...load(),
  setFilter: (key, value) => set((s) => ({ filters: { ...s.filters, [key]: value } })),
  clearFilters: () => set({ filters: DEFAULT_VIEW.filters }),
  set: (patch) => set(patch),
}));

useView.subscribe((s) => {
  const { filters, mode, sort, snap, layout } = s;
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ filters, mode, sort, snap, layout }));
  } catch {
    // storage unavailable: settings just won't persist
  }
});
