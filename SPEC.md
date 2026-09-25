# Unconf — Specification

A web application for running a fully online unconference: a shared whiteboard of
sticky notes proposing sessions, dot-voting, wave-based scheduling into time slots
and virtual rooms, per-sticky chat, and moderation — with a Google Workspace
integration layer (Sheets, Calendar, Drive, Chat, Gemini meeting notes) that is
**stubbed in phase 1** and wired to real APIs later.

This document is self-contained: an implementer should be able to build the entire
system from it without access to prior discussion.

---

## 1. Goals and constraints

- **Audience/scale:** 50–300 concurrent participants in one unconference event.
  A single Go server process with in-memory pub/sub is sufficient; do not build
  horizontal scaling, CRDTs, or external message brokers.
- **Deployment:** one static Go binary serving the API, WebSocket, and the
  embedded frontend. SQLite on local disk is the source of truth.
- **Browsers:** Google Chrome (current stable) is the sole deployment target.
  Development machines test with Safari, so the app must remain functional
  there, but spend no effort on other browsers, legacy versions, or polyfills.
- **Google Workspace:** the Google Sheet is a *mirror* of our data, never the
  primary store. All Google services are behind Go interfaces with local stub
  implementations in phase 1 (see §10). Google Calendar will eventually create
  the breakout meetings, which bring their own Meet rooms and Google Chat —
  therefore our built-in chat only needs to serve the ideation and scheduling
  phases, and chat storage must be *thread-shaped* so threads can later map onto
  Google Chat sub-threads.
- **Identity:** asserted identity for now (user types a name, no password).
  Single sign-on comes later; keep the auth surface small and replaceable.
- **Timeline shape:** ideation and voting are *ongoing* activities that may open
  a week or more before the live event. Scheduling happens in **waves**: schedule
  some breakouts, run them, break for lunch, come back and schedule another wave.
- **Moderation:** some users hold moderation privileges and can hide notes and
  chat messages that are not safe for work. Hidden items disappear for
  participants but remain visible (flagged) to moderators and organizers.

### Non-goals (phase 1)

- Real Google API calls, OAuth/SSO, multi-event UI (schema is multi-event-ready,
  UI serves one event), mobile-native apps, offline editing, horizontal scaling.

---

## 2. Glossary

| Term | Meaning |
|---|---|
| **Event** | One unconference instance. |
| **Note (sticky)** | A proposed session: a card on the board with a title, rich detail, links, votes, and a chat thread. |
| **Region** | A moderator-drawn background rectangle with a color and header label (a full-width/height one acts as a swimlane). Regions are *semantic*: a note inside a region is tagged by it. |
| **Wave** | One scheduling round: a set of time slots that organizers fill from the pool of top-voted unscheduled notes. |
| **Slot** | A time interval (start/end) within a wave. |
| **Room** | A named virtual meeting room (event-level; later carries a Google Meet link). |
| **Assignment** | A note placed at (slot, room). |
| **Personal view settings** | Client-only settings — filters, sort order, snap-to-grid, camera — that never affect other users' rendering. |
| **Star** | A personal bookmark on a note. Stored server-side (so it survives devices/reloads) but visible only to its owner; feeds personal filters and sorts. |

---

## 3. Roles and permissions

Three roles, strictly ordered: `participant < moderator < organizer`.
A higher role can do everything a lower role can.

| Capability | Participant | Moderator | Organizer |
|---|---|---|---|
| Create notes; edit/delete **own** notes; add/remove links on own notes | ✔ | ✔ | ✔ |
| Move any (unscheduled, unhidden) note on the board | ✔ | ✔ | ✔ |
| Vote / retract votes while voting is open | ✔ | ✔ | ✔ |
| Star/unstar any note (personal bookmark) | ✔ | ✔ | ✔ |
| Post chat messages | ✔ | ✔ | ✔ |
| Edit/delete **any** note; edit links on any note | | ✔ | ✔ |
| Hide/unhide notes and chat messages | | ✔ | ✔ |
| Create/edit/delete regions | | ✔ | ✔ |
| See hidden items (flagged) and the audit log | | ✔ | ✔ |
| Open/close voting; set votes-per-user | | | ✔ |
| Manage waves, slots, rooms, assignments | | | ✔ |
| Advance event lifecycle; promote/demote users (participant ↔ moderator) | | | ✔ |

- The first organizer is bootstrapped by logging in with the admin key (§6).
- Organizers cannot demote other organizers via the UI (avoid lockout games);
  changing an organizer requires the admin key login path.

---

## 4. Lifecycle and waves

### Event lifecycle

`setup → active → done` (organizer advances; transitions are one-way).

- **setup:** only organizers/moderators can interact (dress the board, draw
  regions, define waves/rooms). Participants see a "not started" page.
- **active:** the working state for the whole multi-day/multi-wave period.
  Note creation, editing, chat are allowed throughout. Voting is gated by the
  independent event flag `voting_open` (organizer toggles it at will — e.g.
  close it during breakouts, reopen between waves).
- **done:** read-only archive for everyone; export still works.

### Wave state machine

`planned → open → locked → done`, organizer-driven, one-way.

- **planned:** wave and its slots exist and are visible (schedule preview);
  no assignments yet.
- **open:** organizers drag notes into the wave's slot×room grid. Assignments
  are broadcast live.
- **locked:** the schedule for this wave is final and published. *Integration
  seam:* on lock, call `CalendarService.CreateBreakouts` (stub logs in phase 1;
  later creates Calendar events with Meet rooms per assignment).
- **done:** the wave has run; its assignments are history.

Multiple waves may exist in any mix of states, but at most **one wave may be
`open` at a time** (server-enforced).

A note is **scheduled** if it has any assignment. The **unscheduled pool** =
visible notes with zero assignments, ranked by votes. Organizers may
exceptionally assign an already-run note to a later wave (popular repeats);
the UI shows a warning but the server allows it — at most one assignment per
note per wave.

---

## 5. Architecture and stack

### Server

- Go ≥ 1.25 (required by the SQLite driver), stdlib `net/http` with pattern routing (`"GET /api/me"` style).
  No web framework.
- WebSocket: `github.com/coder/websocket` (v1.8.x).
- SQLite: `modernc.org/sqlite` (pure Go, no cgo), `database/sql`.
  Single-writer discipline: all mutations flow through one goroutine (the hub,
  §8) so SQLite write contention is a non-issue. Enable WAL mode.
- Frontend embedded with `embed.FS`; `make build` compiles the Vite app into
  `web/dist` and the Go binary embeds it.

### Client

- React 18 + TypeScript 5 + Vite 8 (Rolldown-based; requires Node ^20.19 or ≥22.12),
  tested with Vitest 5.
- State: Zustand (v4) store fed exclusively by WebSocket events (server is
  authoritative; optimistic UI only for drag-in-progress).
- Markdown rendering: `react-markdown` + `remark-gfm` (safe by default — no raw
  HTML passthrough).
- No component library; hand-rolled CSS (CSS modules or plain files). Custom
  pointer-event drag for stickies and region drawing; the board is a pannable,
  zoomable canvas implemented with a CSS transform on a positioned container
  (no `<canvas>` element needed).

### Realtime model

Server-authoritative event log (§8): clients send **commands**; the server
validates, persists, assigns a monotonically increasing `seq`, and broadcasts
**events**. Clients never mutate shared state locally except for in-flight drag
previews. Personal filters and pan/zoom are pure client state and never sent to
the server.

### Project layout

```
cmd/unconf/main.go        — flags/env, wiring, serve
internal/domain/          — types, lifecycle/wave state machines, command
                            validation, region containment
internal/store/           — sqlite open/migrate (embedded SQL), queries
internal/server/          — http handlers, ws hub, session middleware, embed
internal/integrations/    — google service interfaces + stubs
web/                      — vite react app
  src/api/                — ws client, protocol types (mirror of §8 shapes)
  src/store/              — zustand store + selectors (incl. filters)
  src/board/              — canvas, sticky, region layer, detail modal
  src/schedule/           — wave grid, unscheduled pool
  src/chat/               — thread panel (inside detail modal)
  src/admin/              — organizer/moderator panels
Makefile, README.md, SPEC.md, LICENSE
```

---

## 6. Identity, sessions, auth

- `POST /api/login` body `{ "name": string, "email"?: string, "adminKey"?: string }`.
  - Names are trimmed; 1–40 chars; uniqueness is case-insensitive per event.
  - If the name is new → create a `users` row (role `participant`).
  - If the name exists → **resume that user** (asserted identity is trusting by
    design; SSO replaces this later — keep this logic isolated in
    `internal/server/auth.go`).
  - If `adminKey` matches env `UNCONF_ADMIN_KEY`, the user's role is set to
    `organizer`.
- Session cookie `unconf_session` = `base64url(userID) + "." + base64url(HMAC-SHA256(userID, secret))`.
  Secret: env `UNCONF_SESSION_SECRET`, else generated once and persisted in the
  `meta` table. Cookie: `HttpOnly`, `SameSite=Lax`, `Secure` when behind TLS.
- `POST /api/logout` clears the cookie. `GET /api/me` returns
  `{ id, name, role, votesRemaining }`.
- All `/api/*` (except login/healthz) and `/ws` require a valid cookie → 401.

---

## 7. Data model

SQLite schema (migrations embedded in `internal/store/migrations/*.sql`,
applied by version at startup). All ids are server-generated opaque strings
(`crypto/rand`, 16 bytes, base32 — sortable not required). Timestamps are UTC
RFC-3339 strings.

```sql
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);

CREATE TABLE events (
  id             TEXT PRIMARY KEY,
  name           TEXT NOT NULL,
  lifecycle      TEXT NOT NULL DEFAULT 'setup',   -- setup|active|done
  voting_open    INTEGER NOT NULL DEFAULT 0,
  votes_per_user INTEGER NOT NULL DEFAULT 5,
  created_at     TEXT NOT NULL
);

CREATE TABLE users (
  id         TEXT PRIMARY KEY,
  event_id   TEXT NOT NULL REFERENCES events(id),
  name       TEXT NOT NULL,
  name_key   TEXT NOT NULL,                        -- lower(name), for uniqueness
  email      TEXT,
  role       TEXT NOT NULL DEFAULT 'participant',  -- participant|moderator|organizer
  created_at TEXT NOT NULL,
  UNIQUE (event_id, name_key)
);

CREATE TABLE notes (
  id         TEXT PRIMARY KEY,
  event_id   TEXT NOT NULL REFERENCES events(id),
  author_id  TEXT NOT NULL REFERENCES users(id),
  title      TEXT NOT NULL,                        -- 1..120 chars
  body_md    TEXT NOT NULL DEFAULT '',             -- markdown, ≤ 20000 chars
  x REAL NOT NULL, y REAL NOT NULL,                -- board coords, unbounded
  color      TEXT NOT NULL DEFAULT 'yellow',       -- yellow|pink|blue|green|orange|purple
  region_id  TEXT REFERENCES regions(id),          -- derived (§9), nullable
  hidden_by  TEXT REFERENCES users(id),            -- moderation
  hidden_at  TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE note_links (
  id      TEXT PRIMARY KEY,
  note_id TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  title   TEXT NOT NULL,
  url     TEXT NOT NULL,                           -- http(s) only, validated
  kind    TEXT NOT NULL DEFAULT 'other'            -- doc|slides|other
);

CREATE TABLE regions (
  id       TEXT PRIMARY KEY,
  event_id TEXT NOT NULL REFERENCES events(id),
  label    TEXT NOT NULL,
  x REAL NOT NULL, y REAL NOT NULL, w REAL NOT NULL, h REAL NOT NULL,
  color    TEXT NOT NULL,                          -- muted background tint
  z        INTEGER NOT NULL DEFAULT 0              -- higher wins containment
);

CREATE TABLE votes (
  id       TEXT PRIMARY KEY,
  user_id  TEXT NOT NULL REFERENCES users(id),
  note_id  TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  cast_at  TEXT NOT NULL
);          -- one row per dot; stacking allowed; budget enforced in domain code

CREATE TABLE stars (
  user_id TEXT NOT NULL REFERENCES users(id),
  note_id TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  PRIMARY KEY (user_id, note_id)
);          -- personal bookmarks; never exposed to any other user

CREATE TABLE waves (
  id       TEXT PRIMARY KEY,
  event_id TEXT NOT NULL REFERENCES events(id),
  name     TEXT NOT NULL,
  status   TEXT NOT NULL DEFAULT 'planned',        -- planned|open|locked|done
  opens_at TEXT                                    -- informational display only
);

CREATE TABLE slots (
  id       TEXT PRIMARY KEY,
  wave_id  TEXT NOT NULL REFERENCES waves(id) ON DELETE CASCADE,
  start_at TEXT NOT NULL,
  end_at   TEXT NOT NULL
);

CREATE TABLE rooms (
  id       TEXT PRIMARY KEY,
  event_id TEXT NOT NULL REFERENCES events(id),
  name     TEXT NOT NULL,
  meet_url TEXT                                    -- filled by CalendarService later
);

CREATE TABLE assignments (
  id      TEXT PRIMARY KEY,
  note_id TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  slot_id TEXT NOT NULL REFERENCES slots(id) ON DELETE CASCADE,
  room_id TEXT NOT NULL REFERENCES rooms(id),
  UNIQUE (slot_id, room_id)
);          -- plus domain rule: at most one assignment per (note, wave)

CREATE TABLE messages (
  id         TEXT PRIMARY KEY,
  note_id    TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  author_id  TEXT NOT NULL REFERENCES users(id),
  thread_id  TEXT,            -- null = root message; else id of the root message
  body       TEXT NOT NULL,   -- plain text, ≤ 2000 chars
  hidden_by  TEXT REFERENCES users(id),
  hidden_at  TEXT,
  created_at TEXT NOT NULL
);

CREATE TABLE audit_log (
  id       TEXT PRIMARY KEY,
  event_id TEXT NOT NULL,
  actor_id TEXT NOT NULL,
  action   TEXT NOT NULL,     -- e.g. hide_note, unhide_message, wave_locked, role_changed
  target   TEXT NOT NULL,     -- id of the affected entity
  detail   TEXT,              -- optional JSON
  at       TEXT NOT NULL
);
```

On first run the server creates a default event (`name` from env
`UNCONF_EVENT_NAME`, default "Unconference") and stores its id in `meta`.

---

## 8. WebSocket protocol

Endpoint: `GET /ws?since=<seq>` (cookie-authenticated). All frames are JSON text.

### Server → client frames

```jsonc
{ "type": "hello",    "you": {"id","name","role"}, "eventSeq": 1234 }
{ "type": "snapshot", "seq": 1234, "state": { /* full visible state, §8.4 */ } }
{ "type": "event",    "seq": 1235, "event": { "kind": "...", ... } }
{ "type": "ack",      "cmdId": "c-17", "seq": 1235 }          // command applied
{ "type": "error",    "cmdId": "c-17", "code": "forbidden", "message": "..." }
```

### Client → server frames

```jsonc
{ "type": "cmd", "cmdId": "c-17", "cmd": "move_note", "payload": { ... } }
{ "type": "ping" }        // server replies {"type":"pong"}; also rely on ws pings
```

`cmdId` is client-generated and only used to correlate ack/error.

### 8.1 Sequencing, snapshot, reconnect

- The hub is a single goroutine owning: the SQLite write connection, the global
  `seq` counter (persisted in `meta`), an in-memory ring buffer of the last
  10 000 events, and the set of connected clients.
- Every state change = exactly one event with the next `seq`, written to the DB
  and broadcast in the same hub iteration.
- On connect with `?since=s`: if `s` is within the ring buffer, replay events
  `> s`; otherwise send a full `snapshot`. The client tracks the highest seq it
  has applied and reconnects with it (exponential backoff, 1s→30s).
- Events are **role-filtered at fan-out** (see §8.3): a participant connection
  and a moderator connection may receive different frames for the same seq, and
  some seqs are skipped entirely for some roles. Clients must tolerate gaps in
  seq numbers.

### 8.2 Commands

Payload fields are exactly the entity fields named in §7. Validation errors →
`error` with code `bad_request`; permission failures → `forbidden`; gating
failures (voting closed, wave not open, lifecycle) → `not_allowed_now`.

| cmd | payload | who / gating |
|---|---|---|
| `create_note` | `{title, bodyMd?, x, y, color?}` | any, lifecycle `active` (mods+ also during `setup`); position is overlap-resolved by the server (§9) |
| `update_note` | `{noteId, title?, bodyMd?, color?}` | author or mods+ |
| `move_note` | `{noteId, x, y}` | any; note must be unhidden; requested position is overlap-resolved by the server (§9); throttle: server coalesces to ≤ 20 moves/s per note |
| `delete_note` | `{noteId}` | author (only if note has 0 votes and 0 assignments), or mods+ always |
| `set_links` | `{noteId, links: [{title,url,kind}]}` | author or mods+ (full replace) |
| `cast_vote` | `{noteId}` | any, `voting_open`, budget remaining |
| `retract_vote` | `{noteId}` | any, `voting_open`, has a vote there |
| `star_note` / `unstar_note` | `{noteId}` | any; stars are personal — resulting events reach only the acting user's connections |
| `post_message` | `{noteId, body, threadId?}` | any, lifecycle `active`; `threadId` must reference a root message on the same note |
| `hide_message` / `unhide_message` | `{messageId}` | mods+ |
| `hide_note` / `unhide_note` | `{noteId}` | mods+ |
| `create_region` | `{label, x, y, w, h, color, z?}` | mods+ |
| `update_region` | `{regionId, label?, x?, y?, w?, h?, color?, z?}` | mods+ |
| `delete_region` | `{regionId}` | mods+ |
| `set_voting` | `{open: bool}` | organizer |
| `set_votes_per_user` | `{n}` (1–20) | organizer |
| `set_lifecycle` | `{lifecycle}` | organizer, forward-only |
| `create_wave` / `update_wave` | `{name, opensAt?}` / `{waveId, ...}` | organizer |
| `set_wave_status` | `{waveId, status}` | organizer, forward-only, ≤ 1 wave `open` |
| `create_slot` / `delete_slot` | `{waveId, startAt, endAt}` / `{slotId}` | organizer, wave `planned|open` |
| `create_room` / `update_room` / `delete_room` | `{name}` / `{roomId, name?, meetUrl?}` / `{roomId}` | organizer; delete only if room has no assignments |
| `assign_note` | `{noteId, slotId, roomId}` | organizer, wave `open`; replaces any existing assignment of that note **in that wave**; cell must be free |
| `unassign_note` | `{assignmentId}` | organizer, wave `open` |
| `set_role` | `{userId, role}` | organizer; participant↔moderator only |

Rate limiting: per connection, 20 commands/s sustained, burst 60 (token
bucket); `move_note` drags should be client-throttled to ~15 Hz. Frame size cap
64 KB. Violations → `error{code:"rate_limited"}`, repeated → close.

### 8.3 Events and moderation-aware fan-out

Event kinds mirror commands: `note_created`, `note_updated`, `note_moved`,
`note_deleted`, `note_retagged {noteId, regionId|null}`, `links_set`,
`vote_cast {noteId, byUserId, total}`, `vote_retracted {...}`,
`note_starred` / `note_unstarred {noteId}` (personal, see below),
`message_posted`, `region_created|updated|deleted`, `voting_set`,
`votes_per_user_set`, `lifecycle_set`, `wave_*`, `slot_*`, `room_*`,
`note_assigned`, `note_unassigned`, `role_set`, `user_joined`.

Moderation events are role-split at fan-out:

- `hide_note`: moderators+ receive `note_hidden {noteId, byUserId}`;
  participants receive `note_deleted {noteId}` (the client just removes it).
- `unhide_note`: moderators+ receive `note_unhidden`; participants receive a
  fresh `note_created` carrying the full note (plus its links; vote totals and
  messages are re-sent as part of the payload).
- Same pattern for messages (`message_hidden` vs `message_deleted`, etc.).
- Snapshots for participants simply omit hidden notes/messages; snapshots for
  moderators+ include them with `hidden: true`.

Vote events carry the new per-note `total` so clients never count rows.
`byUserId` lets a client update its own remaining budget.

`note_starred` / `note_unstarred` are personal: they are delivered only to the
acting user's own connections. Other clients simply see a seq gap (already
required by role filtering), and no user's stars ever appear in another user's
events or snapshots.

`note_moved` always carries the server-resolved final coordinates (§9), which
may differ from what the mover requested; the originating client snaps its
optimistic position to them.

### 8.4 Snapshot shape

```jsonc
{
  "event":   { "id","name","lifecycle","votingOpen","votesPerUser" },
  "users":   [ {"id","name","role"} ],
  "notes":   [ {"id","title","bodyMd","authorId","x","y","color","regionId",
                "voteTotal","myVotes","starred","hidden"?,"links":[...],"scheduled":bool} ],
  "regions": [ {"id","label","x","y","w","h","color","z"} ],
  "waves":   [ {"id","name","status","opensAt","slots":[{"id","startAt","endAt"}]} ],
  "rooms":   [ {"id","name","meetUrl"} ],
  "assignments": [ {"id","noteId","slotId","roomId"} ],
  "messages": { "<noteId>": [ {"id","authorId","threadId","body","hidden"?,"createdAt"} ] },
  "me":      { "votesRemaining": n }
}
```

---

## 9. Board geometry: containment and non-overlap

### Region containment (semantic tagging)

- A note belongs to the region whose rectangle contains the note's **center
  point** `(x + W/2, y + H/2)` where `W×H` is the fixed sticky size
  (180×120 board units). If several regions contain it, highest `z` wins;
  ties broken by newest region. No containing region → `region_id = NULL`.
- Recomputed server-side (in `internal/domain`):
  - on `move_note` / `create_note` → for that note;
  - on `create_region` / `update_region` / `delete_region` → for **all** notes
    of the event (≤ a few hundred; trivial).
- Changes emit `note_retagged` events. `region_id` feeds personal filters,
  the grouped list view, and the spreadsheet export's "Region" column.

### Sticky non-overlap

Sticky notes never overlap. All stickies are 180×120 board units; the rule is
server-authoritative and deterministic:

- On `create_note` and `move_note`, if the requested rectangle intersects any
  other **visible** note's rectangle, the server resolves to the nearest free
  position: scan outward from the requested point in a square spiral with a
  20-unit step; the first non-intersecting position wins. (A few hundred notes
  → brute-force intersection checks are fine.)
- The resulting `note_created` / `note_moved` event carries the resolved
  coordinates; clients render the authoritative position (§8.3). During a drag
  the client shows its optimistic ghost and snaps on the ack event.
- **Hidden notes are ignored** for overlap (participants can't see them and
  must not be able to infer them from blocked placement). On `unhide_note`,
  the server re-resolves the unhidden note's own position if it now overlaps,
  emitting a `note_moved` alongside the unhide.

### Board extent, pan/zoom, snap-to-grid

- The board is far larger than any viewport: coordinates are unbounded floats,
  and every client views it through its own pan/zoom camera (§11). There is no
  shared "edge"; the UI offers a "fit all notes" button to re-find content.
- **Snap-to-grid** is a personal view setting: when enabled, the client
  quantizes drag/create coordinates to a 20-unit grid *before* sending the
  command. The server is grid-agnostic (overlap resolution's 20-unit spiral
  step keeps resolved positions on-grid for grid users).

---

## 10. Google integration layer (`internal/integrations`)

Interfaces are final-shaped now; phase 1 ships the listed stubs. Wiring is via
constructor injection in `main.go` so real implementations are a swap.

```go
type BoardSnapshot struct { /* flattened notes+votes+region+links+schedule, JSON- and CSV-able */ }

type SheetMirror interface {
    // Called debounced (≥ 2s quiet or ≥ 30s max) after any state change.
    Sync(ctx context.Context, s BoardSnapshot) error
}
// Stub: writes UNCONF_EXPORT_DIR/snapshot.json and snapshot.csv; the same
// bytes are served at GET /api/export.{json,csv}. Real impl: Sheets API,
// one tab "Sessions", one tab per wave "Schedule — <wave>".

type Breakout struct { NoteID, Title, Room string; Start, End time.Time; Attendees []string }
type CalendarService interface {
    // Called when a wave is locked. Returns per-note meet links to store on rooms/assignments.
    CreateBreakouts(ctx context.Context, waveName string, b []Breakout) (map[string]string, error)
}
// Stub: logs and returns fake meet URLs ("https://meet.example/<noteID>").

type DriveService interface {
    // Later: create a notes doc per scheduled session, return its URL.
    CreateSessionDoc(ctx context.Context, title string) (url string, err error)
}
// Stub: returns "" (feature hidden in UI when empty).

type ChatService interface {
    // Later: mirror a note's chat thread into a Google Chat thread.
    EnsureThread(ctx context.Context, noteID, title string) (threadRef string, err error)
    Post(ctx context.Context, threadRef, author, body string) error
}
// Stub: no-op. messages.thread_id keeps our data mappable onto Chat sub-threads.

type MeetingNotesService interface {
    // Later: enable/collect Gemini meeting notes for a breakout.
    Enable(ctx context.Context, calendarEventID string) error
    FetchSummary(ctx context.Context, calendarEventID string) (docURL string, err error)
}
// Stub: no-op / "".
```

Export CSV columns (Sessions tab): `id, title, author, region, votes, status
(unscheduled|scheduled|done), wave, slot_start, slot_end, room, links
(semicolon-joined), hidden (mods-only export includes it; public export omits
hidden rows entirely)`.

---

## 11. Frontend specification

### Screens

1. **Login** — name (+ optional email, optional admin key under a disclosure).
2. **Board** (default) — the whiteboard.
3. **Schedule** — per-wave slot×room grids; organizers get the editing view
   with the unscheduled pool; everyone else a read-only published view.
4. **Admin** — organizer: lifecycle, voting toggle + budget, waves/slots/rooms,
   user roles. Moderator: hidden-items list (unhide from here), audit log.

Top bar: event name, view tabs, connection indicator (green/amber during
reconnect), votes remaining (when voting open), user name + role badge.

### Board

- The board surface is much larger than the browser viewport: pannable (drag
  background / wheel) and zoomable (ctrl-wheel / pinch, 0.25×–2×), with a
  "fit all notes" control. The camera is client-local, persisted in
  `localStorage`.
- **Region layer** under notes: tinted rects with header labels. Moderators get
  a "draw region" tool (drag to create) and move/resize/relabel handles.
- **Stickies** (180×120): title + 2-line body snippet, color, vote-dot count
  badge, chat-count badge, author initials, region tint strip, star toggle
  (☆/★ on hover — personal). Drag to move (optimistic ghost; the server's
  overlap-resolved `note_moved` reconciles, §9). Double-click/tap →
  **detail modal**: full markdown body, links list (doc/slides icons), vote
  button (+/-), star toggle, chat thread panel, edit affordances per role,
  moderator hide button.
- **Create**: double-click empty board or a "+ New session" button → inline
  title entry, then optional detail editing in the modal.
- **View bar** (client-only): filters — My stickies · My votes · Starred ·
  Unscheduled · Region (multi-select) · text search — with a toggle between
  *dim* (default, non-matching at 25 % opacity) and *hide*; a **sort** control
  (used by the list view): Ranking (votes) · Newest · Authored by me first ·
  Starred first; and a **snap-to-grid** toggle (§9). All of it is personal:
  persisted in `localStorage`, never sent to the server (stars being the one
  server-stored — but still private — piece).
- **List view** toggle: same data grouped by region and ordered by the chosen
  sort — useful during voting and for accessibility.

### Voting

Dots on stickies; clicking + on a sticky (or in the modal) casts, − retracts.
Budget shown in top bar; stacking multiple dots on one note allowed. When
`voting_open` flips off, controls disable live.

### Scheduling (organizer, wave `open`)

Left rail: unscheduled pool sorted by votes (drag source). Grid: columns =
rooms, rows = wave's slots. Drag a note into a cell → `assign_note`; drag out →
unassign; occupied cells reject drops. "Lock wave" button with confirm →
`set_wave_status`. Locked/done waves render read-only with meet links when
present.

### Realtime & errors

Zustand store applies events by `kind`; `error` frames surface as toasts.
On reconnect-with-snapshot, replace store wholesale. In-flight optimistic drag
is reconciled by the authoritative `note_moved` event (last-write-wins is fine
at this scale).

---

## 12. Configuration

| Env var | Default | Purpose |
|---|---|---|
| `UNCONF_ADDR` | `:8080` | listen address |
| `UNCONF_DB` | `unconf.db` | SQLite path |
| `UNCONF_ADMIN_KEY` | *(required)* | organizer bootstrap key |
| `UNCONF_SESSION_SECRET` | generated→`meta` | cookie HMAC secret |
| `UNCONF_EVENT_NAME` | `Unconference` | default event name |
| `UNCONF_EXPORT_DIR` | `./export` | SheetMirror stub output |

`make dev`: runs the Go server (`go run ./cmd/unconf`) and `vite dev` with a
proxy of `/api` and `/ws` to the Go server. `make build`: `vite build` then
`go build` (embeds `web/dist`). `make test`: `go test ./...` and `vitest run`.

---

## 13. Milestones and acceptance criteria

Build in order; each milestone ends compiling, tested, and demoable.

1. **Scaffold** — repo layout, Makefile, health endpoint, embedded static
   serving, Vite shell renders "unconf".
   ✓ `make build` yields one binary that serves the app; `make dev` hot-reloads.
2. **Identity & roles** — login/logout/me, cookie HMAC, admin-key bootstrap,
   role middleware.
   ✓ Two browsers hold distinct identities; admin key yields organizer badge.
3. **Board core** — WS hub (seq/ring/snapshot), note CRUD + move with
   server-side overlap resolution, pan/zoom camera over an
   effectively-unbounded board, detail modal with markdown + links, SQLite
   persistence.
   ✓ Two windows see each other's notes and drags live; dropping a note onto
   another nudges it to the nearest free spot in every window; server restart
   preserves the board; reconnect resyncs (kill server, restart, clients
   recover).
4. **Regions & personal views** — region draw/edit (mods), containment
   tagging, view bar (filters, sorts, snap-to-grid), stars, grouped list view.
   ✓ Dragging a note into a region tags it (visible in list view); filters,
   sort order, snap-to-grid, and stars affect only the local window/user;
   list view orders by ranking / authored / starred as selected.
5. **Voting** — voting toggle, budgets, live tallies, vote sort.
   ✓ Budget enforced server-side; totals update live in all windows; closed
   voting rejects with `not_allowed_now`.
6. **Waves & scheduling** — wave/slot/room CRUD, one-open-wave rule,
   assignment grid, lock/publish.
   ✓ Organizer schedules top-voted notes; participants see the published grid;
   locking calls the Calendar stub (visible in logs/fake meet links).
7. **Chat** — thread panel, realtime, thread-shaped storage.
   ✓ Messages appear live in the other window's modal; replies nest under
   roots; counts on stickies update.
8. **Moderation** — hide/unhide notes & messages with role-split fan-out,
   hidden-items admin list, audit log.
   ✓ Hiding a note removes it from participant windows instantly but leaves it
   flagged for mods; unhide restores it (with votes/messages intact);
   participant snapshots never contain hidden content.
9. **Export & integration stubs** — BoardSnapshot builder, debounced
   SheetMirror stub, `/api/export.{csv,json}`, remaining service stubs wired.
   ✓ `curl /api/export.csv` reflects a change within ~5 s; export omits hidden
   notes; schedule columns filled after a wave locks.

---

## 14. Testing & verification

- **Go unit tests:** store CRUD + migrations; vote budget math; wave/lifecycle
  transition rules (incl. one-open-wave); region containment (z-order, ties,
  region-edit retagging); sticky non-overlap resolution (deterministic spiral,
  hidden notes ignored, re-resolution on unhide); star privacy (star events
  reach only the acting user; snapshots never carry another user's stars);
  command permission matrix (table-driven: every command × every role);
  snapshot role-filtering (participant snapshot contains no hidden items).
- **Hub tests:** in-process WS clients (httptest server): connect two clients,
  apply commands, assert both receive ordered events; reconnect with
  `?since` inside and outside the ring buffer; role-split moderation fan-out.
- **Frontend:** vitest for the store reducer (event application, filter
  selectors). Manual e2e script in README: the two-window walkthrough from
  milestones 3–8.
- **Load sanity:** a Go test spawning 300 WS clients each sending 1 move/s for
  30 s; assert no dropped events and p99 broadcast latency < 250 ms locally.

---

## 15. Future work (explicit seams, do not build now)

- Google SSO replacing asserted identity (isolated in `auth.go`).
- Real Sheets/Calendar/Drive/Chat/Gemini implementations of §10 interfaces.
- Google Chat sub-thread mirroring using `messages.thread_id`.
- Multi-event UI on the already event-scoped schema.
- Room capacity hints, attendance tracking ("I'll attend" RSVPs feeding
  Calendar invites).
