import { describe, expect, it } from 'vitest';
import type { Note, Region } from '../api/protocol';
import { DEFAULT_VIEW, filtersActive, groupByRegion, matches, NO_REGION, snap, sortNotes, type Filters } from './view';

const note = (id: string, over: Partial<Note> = {}): Note => ({
  id,
  title: `Note ${id}`,
  bodyMd: '',
  authorId: 'other',
  x: 0,
  y: 0,
  color: 'yellow',
  regionId: null,
  voteTotal: 0,
  voted: false,
  starred: false,
  links: [],
  scheduled: false,
  createdAt: `2026-01-01T00:00:0${id.length}Z`,
  updatedAt: '',
  ...over,
});

const f = (over: Partial<Filters>): Filters => ({ ...DEFAULT_VIEW.filters, ...over });

describe('matches', () => {
  it('passes everything with no filters', () => {
    expect(filtersActive(DEFAULT_VIEW.filters)).toBe(false);
    expect(matches(note('a'), DEFAULT_VIEW.filters, 'me')).toBe(true);
  });

  it('applies each filter', () => {
    const mine = note('a', { authorId: 'me' });
    expect(matches(mine, f({ mine: true }), 'me')).toBe(true);
    expect(matches(note('b'), f({ mine: true }), 'me')).toBe(false);
    expect(matches(note('c', { voted: true }), f({ myVotes: true }), 'me')).toBe(true);
    expect(matches(note('d'), f({ myVotes: true }), 'me')).toBe(false);
    expect(matches(note('e', { starred: true }), f({ starred: true }), 'me')).toBe(true);
    expect(matches(note('f'), f({ starred: true }), 'me')).toBe(false);
    expect(matches(note('g', { scheduled: true }), f({ unscheduled: true }), 'me')).toBe(false);
    expect(matches(note('h'), f({ unscheduled: true }), 'me')).toBe(true);
  });

  it('filters by region, including "no region"', () => {
    const inA = note('a', { regionId: 'A' });
    const loose = note('b');
    expect(matches(inA, f({ regions: ['A'] }), 'me')).toBe(true);
    expect(matches(loose, f({ regions: ['A'] }), 'me')).toBe(false);
    expect(matches(loose, f({ regions: [NO_REGION] }), 'me')).toBe(true);
    expect(matches(inA, f({ regions: ['B', NO_REGION] }), 'me')).toBe(false);
  });

  it('searches title and body case-insensitively', () => {
    const n = note('a', { title: 'Rust at Work', bodyMd: 'borrow checker' });
    expect(matches(n, f({ search: 'rust' }), 'me')).toBe(true);
    expect(matches(n, f({ search: 'BORROW' }), 'me')).toBe(true);
    expect(matches(n, f({ search: 'go' }), 'me')).toBe(false);
    expect(matches(n, f({ search: '   ' }), 'me')).toBe(true);
  });

  it('combines filters with AND', () => {
    const n = note('a', { authorId: 'me', starred: false });
    expect(matches(n, f({ mine: true, starred: true }), 'me')).toBe(false);
  });
});

describe('sortNotes', () => {
  const a = note('a', { voteTotal: 1, createdAt: '2026-01-01T00:00:01Z' });
  const b = note('b', { voteTotal: 5, createdAt: '2026-01-01T00:00:02Z' });
  const c = note('c', { voteTotal: 1, createdAt: '2026-01-01T00:00:03Z', authorId: 'me', starred: true });
  const ids = (ns: Note[]) => ns.map((n) => n.id).join('');

  it('ranks by votes, oldest first on ties', () => {
    expect(ids(sortNotes([a, c, b], 'ranking', 'me'))).toBe('bac');
  });
  it('orders newest first', () => {
    expect(ids(sortNotes([a, c, b], 'newest', 'me'))).toBe('cba');
  });
  it('puts my notes first, then ranking', () => {
    expect(ids(sortNotes([a, b, c], 'mine', 'me'))).toBe('cba');
  });
  it('puts starred first, then ranking', () => {
    expect(ids(sortNotes([a, b, c], 'starred', 'me'))).toBe('cba');
  });
  it('does not mutate its input', () => {
    const input = [a, b, c];
    sortNotes(input, 'ranking', 'me');
    expect(ids(input)).toBe('abc');
  });
});

describe('groupByRegion', () => {
  const region = (id: string, x: number, y: number): Region => ({ id, label: id, x, y, w: 200, h: 200, color: '#ffffff', z: 0 });

  it('groups in board reading order with untagged notes last, dropping empty groups', () => {
    const top = region('Top', 500, 0);
    const left = region('Left', 0, 400);
    const right = region('Right', 600, 400);
    const empty = region('Empty', 0, 0);
    const notes = [note('1', { regionId: 'Right' }), note('2'), note('3', { regionId: 'Top' }), note('4', { regionId: 'Left' })];
    const groups = groupByRegion(notes, [right, empty, left, top]);
    expect(groups.map((g) => g.region?.id ?? 'none')).toEqual(['Top', 'Left', 'Right', 'none']);
    expect(groups[3].notes.map((n) => n.id)).toEqual(['2']);
  });

  it('treats a tag for an unknown region as untagged', () => {
    expect(groupByRegion([note('1', { regionId: 'gone' })], [])[0].region).toBeNull();
  });
});

describe('snap', () => {
  it('quantizes to the 20-unit grid only when on', () => {
    expect(snap(29, true)).toBe(20);
    expect(snap(31, true)).toBe(40);
    expect(snap(-29, true)).toBe(-20);
    expect(snap(29, false)).toBe(29);
  });
});
