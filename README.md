# unconf

A web app for running a fully online unconference: a shared whiteboard of
sticky-note session proposals, dot voting, wave-based scheduling into rooms,
per-note chat, and moderation. See [SPEC.md](SPEC.md) for the full design.

## Requirements

- Go 1.22+
- Node.js 20.19+ or 22.12+ (with npm) — required by Vite 8

## Development

```sh
make dev     # Go server on :8080 + Vite dev server (hot reload) on :5173
```

Open http://localhost:5173. Vite proxies `/api` and `/ws` to the Go server.
Go code is not hot-reloaded; restart `make dev` after server changes.

## Build and run

```sh
make build   # builds the frontend, then one static binary at bin/unconf
./bin/unconf # serves the app on :8080
```

## Test

```sh
make test    # go vet + go test, then tsc + vitest
```

## Configuration

| Env var | Default | Purpose |
|---|---|---|
| `UNCONF_ADDR` | `:8080` | listen address |

More settings arrive with later milestones (see SPEC.md §12).

## License

Public domain ([Unlicense](LICENSE)).
