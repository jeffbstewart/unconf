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

**Voting (milestone 5)**

1. A (organizer): open the **Admin** tab, set *Votes per person* to 3, and
   **Open voting**. B's top bar shows *3 / 3 votes left*.
2. B: click a note's **▲ 0** toggle. The first time, a notice explains that
   votes are public; confirm it. A sees **▲ 1** live, and B's toggle turns
   blue. Clicking it again withdraws the vote.
3. B: vote for three sessions. The remaining toggles disable, since it's one
   vote per session with a budget of 3. The server enforces this too.
4. B: in **List** with *Ranking (votes)*, notes reorder live as votes move.
5. A: **Close voting**. B's toggles and budget disappear, while the tallies
   stay. Change the budget and reopen: B's remaining count reflects the new
   budget.

**Scheduling (milestone 6)**

Open a third window (C) and log in as a new name.

1. A (organizer): **Admin → People** and make C a *moderator*. Moderators
   and organizers are the schedulers. With voting open, have B vote for a
   few sessions.
2. C: **Schedule → + New wave**, give it 6 tracks. Add a few 45-minute
   slots, starting the first a few minutes in the past so "Happening now"
   has something to show. Then **Open for scheduling**. B (Schedule tab)
   sees the grid live, labelled *Draft*, but gets no editing controls.
3. C: drag sessions from the **Unscheduled** rail into cells, or click one
   and then click an empty cell. Put two sessions B voted for in the same
   slot: C's slot shows *⚠ n conflicts*, hovering a card explains who
   overlaps, and B sees *2 of your picks*. **Move** or drag one to another
   slot and the conflict clears. Two sessions by the same proposer in one
   slot are outlined red.
4. C: **Lock & publish** (confirm). Every cell gets a **Join** link (fake
   `meet.example` URLs from the Calendar stub; see the server log). B's
   votes on those sessions come back (top bar), and their vote toggles
   become plain tallies.
5. B: **Happening now** lists the current slot's sessions with one-click
   **Join**, and **Up next** shows the next slot.

