import { describe, expect, it } from 'vitest';
import { initials, snippet } from './text';

describe('snippet', () => {
  it('strips markdown syntax', () => {
    expect(snippet('# Title\n\nSome **bold** and [a link](https://x).\n- item')).toBe('Title Some bold and a link. item');
  });

  it('truncates long text', () => {
    const s = snippet('word '.repeat(100), 20);
    expect(s).toHaveLength(20);
    expect(s.endsWith('…')).toBe(true);
  });
});

describe('initials', () => {
  it('uses first and last names', () => {
    expect(initials('Ada Lovelace')).toBe('AL');
    expect(initials('  grace  ')).toBe('G');
    expect(initials('Jean Paul Sartre')).toBe('JS');
    expect(initials('')).toBe('?');
  });
});
