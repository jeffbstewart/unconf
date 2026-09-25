# Unconf — Specification

A web application for running a fully online unconference: a shared whiteboard of
sticky notes proposing sessions, voting, wave-based scheduling into time slots
and parallel tracks (with a server-side schedule suggester), per-sticky chat,
and moderation — with a Google Workspace
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
| **Wave** | One scheduling round: a set of time slots × a number of parallel tracks, filled from the pool of top-voted unscheduled notes. |
| **Slot** | A time interval (start/end) within a wave. |
| **Track** | One of a wave's numbered parallel lanes (1…T). The event is fully virtual, so tracks are just numbers — every scheduled session gets its own meeting (§10). |
| **Cell** | A (slot, track) position in a wave's grid; holds at most one session. |
| **Assignment** | A note placed in a cell. |
| **Scheduler** | Anyone who builds schedules: moderators and organizers. |
| **Interest** | Who wants to be in a session: its voters, plus its proposer (who facilitates it and so can't attend anything concurrent). |
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
| Manage waves, slots, tracks, assignments; run the schedule suggester | | ✔ | ✔ |
| Advance event lifecycle; promote/demote users (participant ↔ moderator) | | | ✔ |

- The first organizer is bootstrapped by logging in with the admin key (§6).
- Organizers cannot demote other organizers via the UI (avoid lockout games);
  changing an organizer requires the admin key login path.

---

## 4. Lifecycle and waves

### Event lifecycle

`setup → active → done` (organizer advances; transitions are one-way).

- **setup:** only organizers/moderators can interact (dress the board, draw
  regions, define waves). Participants see a "not started" page.
- **active:** the working state for the whole multi-day/multi-wave period.
  Note creation, editing, chat are allowed throughout. Voting is gated by the
  independent event flag `voting_open` (organizer toggles it at will — e.g.
  close it during breakouts, reopen between waves).
- **done:** read-only archive for everyone; export still works.

### Wave state machine

`planned → open → locked → done`, scheduler-driven, one-way.

- **planned:** the wave, its slots, and its track count exist and are visible
  (schedule preview); no assignments yet.
- **open:** schedulers fill the wave's slot × track grid, by hand (drag) and/or
  with the suggester (§4.2). Assignments are broadcast live, and everyone can
  watch the grid take shape, labelled *Draft*.
- **locked:** the schedule for this wave is final and published. *Integration
  seam:* on lock, call `CalendarService.CreateBreakouts` (stub in phase 1;
  later creates one Calendar event with its own Meet link per assignment).
  The call runs **outside** the hub's transaction (it is network I/O); its
  meet links come back as a follow-up `assignment_links_set` event.
- **done:** the wave has run; its assignments are history.

Multiple waves may exist in any mix of states, but at most **one wave may be
`open` at a time** (server-enforced).

A note is **scheduled** if it has any assignment. The **unscheduled pool** =
visible notes with zero assignments, ranked by voters. Schedulers may
exceptionally assign an already-run note to a later wave (popular repeats);
the UI shows a warning but the server allows it — at most one assignment per
note per wave.

### 4.1 Votes across waves

Each person has **one vote per session** and a budget (default 5) of
concurrent votes. A vote stops counting against the budget once it has "paid
off" or can't:

> `votesUsed` = the user's votes on notes that are **not hidden** (M8) and
> **not assigned in a `locked` or `done` wave**.

So when a wave locks, everyone who voted for its sessions gets those votes
back for the next wave; the vote rows are kept as interest history. Votes on
the *open* wave's draft still count (the draft can change). Voting for — or
withdrawing a vote from — a note already scheduled in a locked/done wave is
`not_allowed_now` (it's history). Whenever a user's `votesUsed` changes for a
reason other than their own vote (lock, hide, unhide), the server sends them a
personal `votes_used_set {votesUsed}` event.

### 4.2 Schedule suggester

`suggest_schedule {waveId}` (schedulers, wave `open`) fills the wave's
**empty** cells. Cells already filled — by hand or by an earlier suggestion —
are kept as-is ("pinned"). The result is ordinary `note_assigned` events;
schedulers then adjust by hand and lock. It never locks on its own.

1. **Eligible:** visible notes not assigned in this wave and not scheduled in
   any locked/done wave, with **at least `scheduleThreshold` voters** (event
   setting, default 2). (Repeats and below-threshold notes can still be
   placed by hand.)
2. **How many:** K = number of empty cells (e.g. 4 slots × 6 tracks = 24,
   minus any pinned).
3. **Which:** the top K eligible notes by voter count; ties → older first,
   then id. The rest stay in the pool for a later wave.
4. **Where:** place the K sessions into slots to minimize **conflicts**. For
   a slot holding sessions S, a person *p* with interest in *k* of them
   contributes *k − 1* conflicts (they can only be in one place). Interest =
   voters ∪ {proposer}.
   - **Hard constraint:** a proposer never has two of their own sessions in
     one slot. Sessions that can't be placed without breaking it stay in the
     pool (the ack reports how many).
   - **Secondary:** balance slots — minimize the largest per-slot sum of
     voters, so every slot offers good alternatives to people who use the
     "law of two feet".
   - **Method:** greedy placement in selection order (each session into the
     slot with a free track that adds the fewest conflicts; ties → lighter
     slot, then earlier slot), then local search: try moving a session to a
     free cell or swapping two unpinned sessions across slots; apply any move
     that strictly improves (conflicts, then balance); repeat until none
     does (bounded iterations). Fully deterministic for a given input.
   - **Tracks within a slot:** fill free track numbers in ascending order,
     most-voted first. Track numbers carry no meaning beyond the grid.

The same conflict computation powers the grid's conflict overlay (§11), so
hand-built schedules get the same feedback.

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
internal/store/           — sqlite open, queries
internal/store/schema/    — schema evolution plumbing + NNN_*.sql fragments (§7.1)
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

SQLite schema, delivered as numbered fragments through the schema evolution
mechanism (§7.1); the listing below is the logical result. All ids are server-generated opaque strings
(`crypto/rand`, 16 bytes, base32 — sortable not required). Timestamps are UTC
RFC-3339 strings with fixed-width milliseconds (`2026-09-25T12:00:00.000Z`),
so they sort chronologically as plain strings.

```sql
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);

CREATE TABLE events (
  id             TEXT PRIMARY KEY,
  name           TEXT NOT NULL,
  lifecycle      TEXT NOT NULL DEFAULT 'setup',   -- setup|active|done
  voting_open    INTEGER NOT NULL DEFAULT 0,
  votes_per_user INTEGER NOT NULL DEFAULT 5,
  schedule_threshold INTEGER NOT NULL DEFAULT 2,  -- min voters for the suggester (§4.2)
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
  cast_at  TEXT NOT NULL,
  UNIQUE (user_id, note_id)   -- fragment 007: one vote per person per session
);          -- budget enforced in domain code

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
  opens_at TEXT,                                   -- informational display only
  tracks   INTEGER NOT NULL DEFAULT 1              -- parallel tracks, numbered 1..tracks
);

CREATE TABLE slots (
  id       TEXT PRIMARY KEY,
  wave_id  TEXT NOT NULL REFERENCES waves(id) ON DELETE CASCADE,
  start_at TEXT NOT NULL,
  end_at   TEXT NOT NULL
);

CREATE TABLE assignments (
  id       TEXT PRIMARY KEY,
  note_id  TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  slot_id  TEXT NOT NULL REFERENCES slots(id) ON DELETE CASCADE,
  track    INTEGER NOT NULL,                        -- 1..wave.tracks
  meet_url TEXT,                                    -- set by CalendarService after lock
  UNIQUE (slot_id, track)
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

(The shipped fragments 001–006 created an event-level `rooms` table and
room-based assignments; M6 adds a fragment that drops `rooms`, rebuilds
`assignments` as above, and adds `waves.tracks` and
`events.schedule_threshold`. No deployed data exists yet.)

On first run the server creates a default event (`name` from env
`UNCONF_EVENT_NAME`, default "Unconference") and stores its id in `meta`.

### 7.1 Schema evolution

- The server's code creates exactly one table itself:

  ```sql
  CREATE TABLE schema_version (
    version    INTEGER PRIMARY KEY,  -- fragment number
    name       TEXT NOT NULL,        -- fragment file name
    sha256     TEXT NOT NULL,        -- hex SHA-256 of the fragment's bytes
    applied_at TEXT NOT NULL
  );
  ```

  Every other table, index, or data change (including `meta`) arrives as a
  **fragment**: a SQL file in `internal/store/schema/` named
  `NNN_lower_snake_description.sql`, where `NNN` is a three-digit number.
  Fragments are embedded in the binary with `embed.FS`.
- Numbers must run 001, 002, … with no gaps or duplicates. Any other file in
  the directory is an error.
- At startup, before serving, the server:
  1. validates the embedded fragments;
  2. creates `schema_version` if absent;
  3. checks that the recorded rows are exactly fragments 1..n of the binary,
     with identical names and SHA-256 hashes. It **refuses to start** on any
     mismatch: an applied fragment was edited or renamed, the history has
     gaps, or the database has fragments this binary lacks (an older binary
     on a newer database);
  4. applies each pending fragment in its own transaction together with its
     `schema_version` row, so a failed fragment leaves no trace and is
     retried on the next start.
- Applied fragments are immutable. Schema changes are always new fragments.
  `.gitattributes` pins fragment line endings so hashes are identical on
  every checkout.

---

## 8. WebSocket protocol

Endpoint: `GET /ws?since=<seq>` (cookie-authenticated). All frames are JSON text.

### Server → client frames

```jsonc
{ "type": "hello",    "you": {"id","name","role"}, "eventSeq": 1234 }
{ "type": "snapshot", "seq": 1234, "state": { /* full visible state, §8.4 */ } }
{ "type": "event",    "seq": 1235, "event": { "kind": "...", ... } }
{ "type": "ack",      "cmdId": "c-17", "seq": 1235, "result"?: {…} }  // command applied; result only where §8.2 says
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
- A reconnect is caught up by replay only when the missed events fit in half
  the connection's send buffer (512 events); otherwise it gets a snapshot.
  A client claiming a `since` beyond the server's seq (e.g. after a database
  reset) also gets a snapshot.
- Each connection has a 1024-frame send buffer. A client too slow to drain
  it is disconnected rather than stalling the hub; it reconnects and catches
  up like any other reconnect.
- Login runs on the hub too, so a new user is a sequenced `user_joined`
  event and an admin-key promotion a `role_set`. A promoted user's open
  connections receive a fresh snapshot for their new role.
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
| `cast_vote` | `{noteId}` | any who may interact (lifecycle as `create_note`), `voting_open`, note not hidden; **one vote per person per session** — voting again is a no-op (ack, no event); otherwise needs budget remaining (out of budget → `not_allowed_now`) |
| `retract_vote` | `{noteId}` | same gating; removes the user's vote there; none there → `bad_request` |
| `star_note` / `unstar_note` | `{noteId}` | any, in every lifecycle (participants excepted during `setup`, when they can't see the board); stars are personal — resulting events reach only the acting user's connections; starring an already-starred note is a no-op (ack, no event) |
| `post_message` | `{noteId, body, threadId?}` | any, lifecycle `active`; `threadId` must reference a root message on the same note |
| `hide_message` / `unhide_message` | `{messageId}` | mods+ |
| `hide_note` / `unhide_note` | `{noteId}` | mods+ |
| `create_region` | `{label, x, y, w, h, color, z?}` | mods+ |
| `update_region` | `{regionId, label?, x?, y?, w?, h?, color?, z?}` | mods+ |
| `delete_region` | `{regionId}` | mods+ |
| `set_voting` | `{open: bool}` | organizer, not once `done`; setting the current value is a no-op (ack, no event) |
| `set_votes_per_user` | `{n}` (1–20) | organizer, not once `done`; lowering it keeps votes already cast (those users just can't add more) |
| `set_lifecycle` | `{lifecycle}` | organizer, forward-only |
| `create_wave` | `{name, tracks, opensAt?}` (tracks 1–20) | schedulers |
| `update_wave` | `{waveId, name?, tracks?, opensAt?}` | schedulers; `tracks` only while `planned|open`, and not below a track in use |
| `delete_wave` | `{waveId}` | schedulers, wave `planned` |
| `set_wave_status` | `{waveId, status}` | schedulers, forward-only, ≤ 1 wave `open`; lock triggers §4 calendar seam and §4.1 refunds |
| `create_slot` / `delete_slot` | `{waveId, startAt, endAt}` / `{slotId}` | schedulers, wave `planned|open`; `endAt > startAt`, no overlap with the wave's other slots; delete only if the slot is empty |
| `assign_note` | `{noteId, slotId, track}` | schedulers, wave `open`, note visible; replaces any existing assignment of that note **in that wave**; cell must be free |
| `unassign_note` | `{assignmentId}` | schedulers, wave `open` |
| `clear_wave` | `{waveId}` | schedulers, wave `open`: removes all its assignments |
| `suggest_schedule` | `{waveId}` | schedulers, wave `open`; fills empty cells (§4.2); ack carries `{placed, unplaced}` |
| `set_schedule_threshold` | `{n}` (1–20) | schedulers |
| `set_role` | `{userId, role}` | organizer; participant↔moderator only |

Rate limiting: per connection, 20 commands/s sustained, burst 60 (token
bucket); `move_note` drags should be client-throttled to ~15 Hz. Frame size cap
64 KB. Violations → `error{code:"rate_limited"}`; 30 consecutive → close.

Move coalescing: a `move_note` arriving within 50 ms of the note's last
applied move is held; later moves of the same note replace it, and it is
applied when the 50 ms elapses. Every coalesced command is acked with the
seq of the move that was applied (or gets its error).

### 8.3 Events and moderation-aware fan-out

Event kinds mirror commands: `note_created`, `note_updated`, `note_moved`,
`note_deleted`, `note_retagged {noteId, regionId|null}`, `links_set`,
`vote_cast {noteId, byUserId, total}`, `vote_retracted {...}`,
`note_starred` / `note_unstarred {noteId}` (personal, see below),
`message_posted`, `region_created|updated|deleted`, `voting_set`,
`votes_per_user_set`, `lifecycle_set`, `wave_created|updated|deleted`,
`wave_status_set`, `slot_created|deleted`, `note_assigned {assignment}`,
`note_unassigned {assignmentId}`, `assignment_links_set {links: {assignmentId: meetUrl}}`,
`schedule_threshold_set`, `votes_used_set {votesUsed}` (personal, §4.1),
`role_set`, `user_joined`.

A command normally answers with a bare `ack`; `suggest_schedule` is the one
whose ack also carries a `result` (`{placed: n, unplaced: n}`).

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
`byUserId` lets a client update its own remaining budget: clients keep
`votesUsed` (from the snapshot) and derive remaining as
`max(0, votesPerUser − votesUsed)`, so budget changes apply without a new
snapshot. A deleted note's votes are deleted with it, refunding its voters.

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
  "event":   { "id","name","lifecycle","votingOpen","votesPerUser","scheduleThreshold" },
  "users":   [ {"id","name","role"} ],
  "notes":   [ {"id","title","bodyMd","authorId","x","y","color","regionId",
                "voteTotal","voted","starred","hidden"?,"links":[...],"scheduled":bool} ],   // voteTotal = number of voters; voted = this user's vote
  "regions": [ {"id","label","x","y","w","h","color","z"} ],
  "waves":   [ {"id","name","status","opensAt","tracks","slots":[{"id","startAt","endAt"}]} ],
  "assignments": [ {"id","noteId","slotId","track","meetUrl"} ],
  "messages": { "<noteId>": [ {"id","authorId","threadId","body","hidden"?,"createdAt"} ] },
  "me":      { "votesRemaining": n, "votesUsed": n }   // votesUsed per §4.1 (includes votes on notes hidden from this user only until M8's refund rule)
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
- Regions have a 1–60 character label, a `#rrggbb` tint, and a size of at
  least 100×100 board units. A region-edit command emits its
  `region_created|updated|deleted` event first, then one `note_retagged` per
  note whose tag changed, in note-id order. Hidden notes are retagged too,
  but only moderators receive their events.

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

type Breakout struct {
    AssignmentID, NoteID, Title string
    Track      int
    Start, End time.Time
    Attendees  []string // emails of the proposer and voters who gave one
}
type CalendarService interface {
    // Called (outside the hub transaction) when a wave is locked. Returns a
    // meet link per assignment id, stored on the assignment.
    CreateBreakouts(ctx context.Context, waveName string, b []Breakout) (map[string]string, error)
}
// Stub: logs and returns fake meet URLs ("https://meet.example/<assignmentID>").

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
(unscheduled|scheduled|done), wave, slot_start, slot_end, track, meet_url, links
(semicolon-joined), hidden (mods-only export includes it; public export omits
hidden rows entirely)`.

---

## 11. Frontend specification

### Screens

1. **Login** — name (+ optional email, optional admin key under a disclosure).
2. **Board** (default) — the whiteboard.
3. **Schedule** (`#/schedule`, everyone) — per-wave slot × track grids.
   Schedulers get the building view (§11 Scheduling); everyone else the
   attendee view.
4. **Admin** (`#/admin`, tab in the top bar) — organizer: lifecycle, voting toggle + budget,
   user roles. Moderator: hidden-items list (unhide from here), audit log.

Top bar: event name, view tabs, connection indicator (green/amber during
reconnect), votes remaining (when voting open), user name + role badge.
The top bar also shows the event's lifecycle; organizers advance it there
(with a confirm) as well as from the Admin screen.

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
  sort — useful during voting and for accessibility. Groups follow the board's
  reading order (top to bottom, then left to right), with untagged notes last.

### Voting

**One vote per person per session** — a vote means "I want to attend",
which is exactly what conflict-aware scheduling needs (decided 2026-09-25;
replaces stacked dots). Each sticky (and the modal and list rows) shows a
`▲ n` voter count that doubles as a toggle for your own vote, filled when
you've voted. The budget (default 5 sessions) is shown in the top bar;
out of budget, unvoted toggles disable (your votes can still be withdrawn).
When `voting_open` flips off, toggles disable live; tallies stay.

**Votes are public** — every vote event names its voter (§8.3), so anyone can
see who voted for what. Before a user's *first* vote the client shows an
interstitial saying so; nothing is sent until they confirm (Cancel sends
nothing). The acknowledgement is remembered per user in `localStorage`, so a
new browser shows it once more. Retracting never triggers it.

**Hidden notes free their votes** (implemented with moderation, M8): dots on
a hidden note stop counting against their voters' budgets while it is hidden.
The dots themselves are kept, so unhiding restores them — which may leave a
voter over budget, handled like a budget cut.

### Schedule screen

Wave tabs across the top (in creation order, with status badges); each wave
is a grid with **rows = slots** (times in the viewer's local time zone) and
**columns = Track 1…T**. A cell shows the session title (opens the detail
modal), proposer, `▲ voters`, the star toggle, and — once the wave is locked
— a **Join** link to its meeting. An open wave is shown to everyone, live,
labelled *Draft — may change*.

**Attendee view** (everyone) — built for choosing where to be, and for the
"law of two feet" (leave a session that isn't working for you, join another):
- **Happening now / Up next**: a strip above the grid with the current slot's
  sessions (by the viewer's clock) and their Join links, so switching rooms
  is one click; the next slot is previewed below it.
- **Your picks**: cells you voted for, proposed, or starred are highlighted
  (distinct markers), and your own sessions are labelled *You're
  facilitating*.
- **Your conflicts**: a slot holding two or more of your picks gets a notice
  ("2 of your picks at 10:30"). If one is your own session, the notice says
  you'll be facilitating it.

**Scheduler view** (moderators and organizers):
- **Wave setup** (planned/open): name, number of tracks, add/remove slots
  (start/end pickers; slots are usually chosen first, then the track count).
  Advance status: Open → Lock (confirm) → Done.
- **Pool rail**: unscheduled sessions sorted by voters; those at or above
  `scheduleThreshold` first, the rest collapsed below a divider. Repeats
  (already run) are listed separately with a warning.
- **Grid editing**: drag from the pool into an empty cell → `assign_note`;
  drag between cells to move; drag back to the pool → `unassign_note`;
  occupied cells reject drops. **Suggest schedule** fills the empty cells
  (§4.2) and reports "placed 24, 6 didn't fit"; **Clear** empties the wave.
- **Conflict overlay**: each slot row shows its conflict count; hovering a
  cell shows the sessions in the same slot that share interest with it
  ("3 people also want *X*"), and a proposer double-booking is flagged red.
  The same numbers the suggester optimizes.
- Locked/done waves render read-only with Join links.

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
6. **Waves & scheduling** — schema move from rooms to numbered tracks;
   wave/slot CRUD with track counts (schedulers = moderators + organizers),
   one-open-wave rule, the drag-and-drop grid with pool and conflict overlay,
   lock/publish with the Calendar stub run outside the transaction and meet
   links as a follow-up event, vote refunds on lock (§4.1), and the Schedule
   screen's attendee view (happening now / up next, your picks, your
   conflicts).
   ✓ A moderator sets up a wave (4 slots × 6 tracks), drags top-voted notes
   in while participants watch the draft live; the overlay counts conflicts;
   locking shows Join links (fake meet URLs) and gives voters of the
   scheduled sessions their votes back; a participant's schedule view shows
   what's happening now with one-click Join links and flags their conflicts.

   **6b. Schedule suggester** (its own PR, after 6) — `suggest_schedule`
   (§4.2) and the `scheduleThreshold` setting.
   ✓ With 30 eligible sessions and 24 empty cells, it places the 24 most-voted
   and leaves 6 in the pool; it never double-books a proposer; pinned cells
   are untouched; on a crafted input with a known conflict-free arrangement it
   finds zero conflicts; the same input always yields the same schedule.
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

- **Go unit tests:** scheduling — conflict metric, suggester (selection
  order, threshold, proposer constraint, pinned cells, determinism, known
  optimum on crafted inputs), slot validation, vote refunds on lock;
  store CRUD; schema evolution (fresh apply, idempotent
  restart, new fragments, refusal on edited/renamed/missing fragments,
  rollback of a failing fragment, file-name validation); vote budget math; wave/lifecycle
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
- Track/meeting capacity hints; attendance tracking ("I'll attend" RSVPs
  feeding Calendar invites and the suggester's interest sets).
