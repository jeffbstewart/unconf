# unconf

A web app for running a fully online unconference: a shared whiteboard of
sticky-note session proposals, dot voting, wave-based scheduling into rooms,
per-note chat, and moderation. See [SPEC.md](SPEC.md) for the full design.

## Requirements

- Go 1.25+
- Node.js 20.19+ or 22.12+ (with npm) — required by Vite 8

## Development

```sh
make dev     # Go server on :8080 + Vite dev server (hot reload) on :5173
```

Open http://localhost:5173. Vite proxies `/api` and `/ws` to the Go server.
Go code is not hot-reloaded; restart `make dev` after server changes.

Log in with any name. To log in as an organizer, open "Organizer?" and enter
the admin key, which is `dev` under `make dev` unless `UNCONF_ADMIN_KEY` is set.
State lives in `unconf.db` in the working directory. Delete it to start over.

## Build and run

```sh
make build   # builds the frontend, then one static binary at bin/unconf
UNCONF_ADMIN_KEY=choose-a-secret ./bin/unconf   # serves the app on :8080
```

## Test

```sh
make test    # go vet + go test, then tsc + vitest
UNCONF_LOAD_TEST=1 go test -run TestLoadSanity -v ./internal/server   # full load test: 300 clients × 30 s
```

`make test` includes a short load test (50 WebSocket clients for 3 s). The
full SPEC §14 load (300 clients each moving a note once a second for 30 s,
asserting no dropped events and p99 broadcast latency under 250 ms) is
opt-in because it takes about 30 seconds.

### Manual two-window walkthrough

Run `make dev`, then open http://localhost:5173 in two browser windows, one
of them private or in another browser so each has its own cookie.

**Board (milestone 3)**

1. Window A: log in as `Olga` with admin key `dev`. The top bar shows
   *Organizer* and the event in *Setup*.
2. Window B: log in as `Ada`. The page says the event has not started.
3. A: **Start event** → confirm. B switches to the board by itself.
4. B: double-click the board, type a title, press Enter. The note appears
   in A. Use **+ New session** for a second note.
5. A: drag B's note around. B sees it move live.
6. B: drop one note onto the other. It is nudged to the nearest free spot,
   at the same position in both windows.
7. Double-click a note → the detail modal. As its author (B), **Edit**:
   add Markdown (e.g. a list and a table), a link, and a color, then save.
   A sees the changes. A non-author participant gets no Edit button.
8. Pan (drag the background or scroll), zoom (ctrl/⌘ + scroll or pinch),
   **Fit all**. Reload: the camera is where you left it.
9. Stop `make dev` (Ctrl-C) and start it again. Both windows show an amber
   dot while disconnected, then reconnect with the board intact.

**Regions and personal views (milestone 4)**

1. A (organizer): **▭ Draw region**, drag a rectangle over some notes,
   type a label, press Enter. The region appears in B, and notes whose
   center is inside it are tagged.
2. B: switch to **List** in the view bar. Notes are grouped by region.
   Drag a note into or out of the region on the board (either window), and
   the list regroups.
3. A: click the region's label to select it. Rename it, change its color,
   bring it to the front or back, resize it from the corner handle, or
   delete it. Tags follow each change.
4. B: try **My stickies**, **★ Starred**, **Region ▾**, and search, with
   **Dim** or **Hide**. A's window is unaffected, and B's settings survive a
   reload.
5. B: star a note (☆ on hover). Only B sees the ★. Sort the list view by
   *Starred first*, *Newest*, or *Authored by me first*.
6. B: turn on **⊞ Snap** and drag a note. It lands on the 20-unit grid. A,
   with snap off, can still place notes anywhere.

## Schema changes

The database schema is built entirely from numbered SQL fragments in
`internal/store/schema/` (`001_meta.sql`, `002_events_users.sql`, ...), which
are embedded in the binary. At startup the server applies any fragments the
database hasn't seen and records each one's SHA-256 in `schema_version`.

To change the schema, **add** the next fragment, e.g.
`007_note_capacity.sql`. Never edit or rename a fragment that has been
applied anywhere: the server refuses to start if a recorded hash doesn't
match the binary's fragment. See SPEC.md §7.1.

## Configuration

| Env var | Default | Purpose |
|---|---|---|
| `UNCONF_ADDR` | `:8080` | listen address |
| `UNCONF_DB` | `unconf.db` | SQLite database path |
| `UNCONF_ADMIN_KEY` | *(required)* | logging in with this key grants the organizer role |
| `UNCONF_SESSION_SECRET` | generated, stored in the DB | HMAC key for session cookies |
| `UNCONF_EVENT_NAME` | `Unconference` | name of the event created on first run |

`UNCONF_EXPORT_DIR` arrives with milestone 9 (see SPEC.md §12).

## License

Public domain ([Unlicense](LICENSE)).
