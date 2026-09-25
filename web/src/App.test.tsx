import { renderToString } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { App } from './App';

describe('App', () => {
  it('renders the app name', () => {
    expect(renderToString(<App />)).toContain('unconf');
  });
});
