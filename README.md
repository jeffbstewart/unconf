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
```

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
