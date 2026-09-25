// REST calls for identity (SPEC §6). Everything else flows over the
// WebSocket from milestone 3 on.

export type Role = 'participant' | 'moderator' | 'organizer';

export interface Me {
  id: string;
  name: string;
  role: Role;
  votesRemaining: number;
}

export interface LoginRequest {
  name: string;
  email?: string;
  adminKey?: string;
}

export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
  }
}

async function errorFrom(res: Response): Promise<ApiError> {
  let message = res.statusText || `HTTP ${res.status}`;
  try {
    const body = (await res.json()) as { error?: string };
    if (body.error) message = body.error;
  } catch {
    // non-JSON error body; keep the status text
  }
  return new ApiError(res.status, message);
}

/** The logged-in user, or null when there is no valid session. */
export async function fetchMe(): Promise<Me | null> {
  const res = await fetch('/api/me', { credentials: 'same-origin' });
  if (res.status === 401) return null;
  if (!res.ok) throw await errorFrom(res);
  return (await res.json()) as Me;
}

export async function login(req: LoginRequest): Promise<Me> {
  const body: LoginRequest = { name: req.name };
  if (req.email?.trim()) body.email = req.email.trim();
  if (req.adminKey) body.adminKey = req.adminKey;
  const res = await fetch('/api/login', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!res.ok) throw await errorFrom(res);
  return (await res.json()) as Me;
}

export async function logout(): Promise<void> {
  const res = await fetch('/api/logout', { method: 'POST', credentials: 'same-origin' });
  // 401 means the session was already gone, which is the goal.
  if (!res.ok && res.status !== 401) throw await errorFrom(res);
}
