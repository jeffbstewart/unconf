import { useEffect, useState } from 'react';

export type Route = 'board' | 'schedule' | 'admin';

function current(): Route {
  // No window when rendered in tests (Node).
  const hash = typeof location !== 'undefined' ? location.hash : '';
  if (hash === '#/admin') return 'admin';
  if (hash === '#/schedule') return 'schedule';
  return 'board';
}

/** The current screen, from the URL hash (so reloads keep it). */
export function useRoute(): Route {
  const [route, setRoute] = useState<Route>(current);
  useEffect(() => {
    const onChange = () => setRoute(current());
    window.addEventListener('hashchange', onChange);
    return () => window.removeEventListener('hashchange', onChange);
  }, []);
  return route;
}

export function hrefFor(r: Route): string {
  return r === 'board' ? '#/' : `#/${r}`;
}
