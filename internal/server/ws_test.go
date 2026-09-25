package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// frame is a decoded server → client frame.
type frame map[string]any

func (f frame) str(k string) string { s, _ := f[k].(string); return s }
func (f frame) seq() int64          { n, _ := f["seq"].(float64); return int64(n) }
func (f frame) event() frame        { e, _ := f["event"].(map[string]any); return e }
func (f frame) kind() string        { return f.event().str("kind") }

// wsClient is an in-process WebSocket client with a logged-in identity.
type wsClient struct {
	t       *testing.T
	conn    *websocket.Conn
	frames  chan frame
	closed  chan struct{}
	nextID  atomic.Int64
	lastSeq int64
}

// loginWS logs a new browser in as name (optionally with the admin key).
func loginWS(t *testing.T, ts *httptest.Server, name string, admin bool) *client {
	t.Helper()
	c := newClient(t, ts)
	body := fmt.Sprintf(`{"name":%q}`, name)
	if admin {
		body = fmt.Sprintf(`{"name":%q,"adminKey":%q}`, name, testAdminKey)
	}
	c.login(body)
	return c
}

func dial(t *testing.T, ts *httptest.Server, c *client, since int64) *wsClient {
	t.Helper()
	u, _ := url.Parse(ts.URL)
	header := http.Header{}
	for _, ck := range c.http.Jar.Cookies(u) {
		header.Add("Cookie", ck.Name+"="+ck.Value)
	}
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?since=" + strconv.FormatInt(since, 10)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	conn.SetReadLimit(16 << 20)
	w := &wsClient{t: t, conn: conn, frames: make(chan frame, 100000), closed: make(chan struct{})}
	go func() {
		defer close(w.closed)
		for {
			_, data, err := conn.Read(context.Background())
			if err != nil {
				return
			}
			var f frame
			if err := json.Unmarshal(data, &f); err != nil {
				t.Errorf("bad frame %s", data)
				return
			}
			w.frames <- f
		}
	}()
	t.Cleanup(func() { conn.Close(websocket.StatusNormalClosure, "") })
	return w
}

// next returns the next frame, failing after a timeout.
func (w *wsClient) next() frame {
	w.t.Helper()
	select {
	case f := <-w.frames:
		if s := f.seq(); f.str("type") == "event" || f.str("type") == "snapshot" {
			w.lastSeq = s
		}
		return f
	case <-time.After(5 * time.Second):
		w.t.Fatal("timed out waiting for a frame")
		return nil
	}
}

// expect returns the next frame, which must have the given type.
func (w *wsClient) expect(typ string) frame {
	w.t.Helper()
	f := w.next()
	if f.str("type") != typ {
		w.t.Fatalf("got %v, want a %s frame", f, typ)
	}
	return f
}

// expectEvent returns the next frame, which must be an event of kind.
func (w *wsClient) expectEvent(kind string) frame {
	w.t.Helper()
	f := w.expect("event")
	if f.kind() != kind {
		w.t.Fatalf("got event %v, want %s", f.event(), kind)
	}
	return f
}

// quiet asserts that no frame arrives for a short while.
func (w *wsClient) quiet() {
	w.t.Helper()
	select {
	case f := <-w.frames:
		w.t.Fatalf("unexpected frame %v", f)
	case <-time.After(150 * time.Millisecond):
	}
}

// send issues a command and returns its cmdId.
func (w *wsClient) send(cmd string, payload any) string {
	w.t.Helper()
	id := "c-" + strconv.FormatInt(w.nextID.Add(1), 10)
	b, _ := json.Marshal(map[string]any{"type": "cmd", "cmdId": id, "cmd": cmd, "payload": payload})
	if err := w.conn.Write(context.Background(), websocket.MessageText, b); err != nil {
		w.t.Fatalf("write: %v", err)
	}
	return id
}

// do sends a command, expects exactly one event of kind to this client,
// then the ack; returns the event body.
func (w *wsClient) do(cmd string, payload any, kind string) frame {
	w.t.Helper()
	id := w.send(cmd, payload)
	ev := w.expectEvent(kind)
	ack := w.expect("ack")
	if ack.str("cmdId") != id || ack.seq() != ev.seq() {
		w.t.Fatalf("ack %v does not match event seq %d", ack, ev.seq())
	}
	return ev.event()
}

// sync discards everything queued for this client so far. It sends an
// unknown command through the hub: the hub handles commands in order, so
// its error reply comes after every frame published before it.
func (w *wsClient) sync() {
	w.t.Helper()
	id := w.send("__sync__", map[string]any{})
	for {
		f := w.next()
		if f.str("type") == "error" && f.str("cmdId") == id {
			return
		}
	}
}

// fail sends a command and expects an error frame with code.
func (w *wsClient) fail(cmd string, payload any, code string) string {
	w.t.Helper()
	id := w.send(cmd, payload)
	f := w.expect("error")
	if f.str("cmdId") != id || f.str("code") != code {
		w.t.Fatalf("%s: got %v, want error %s", cmd, f, code)
	}
	return f.str("message")
}

// connectFresh dials with since=0 and consumes hello + snapshot.
func connectFresh(t *testing.T, ts *httptest.Server, c *client) (*wsClient, frame) {
	t.Helper()
	w := dial(t, ts, c, 0)
	w.expect("hello")
	snap := w.expect("snapshot")
	return w, snap["state"].(map[string]any)
}

// startEvent logs in an organizer and moves the event to active.
func startEvent(t *testing.T, ts *httptest.Server) *wsClient {
	t.Helper()
	org, _ := connectFresh(t, ts, loginWS(t, ts, "Org", true))
	org.do("set_lifecycle", map[string]any{"lifecycle": "active"}, "lifecycle_set")
	return org
}

func TestWSRequiresSession(t *testing.T) {
	_, ts := setup(t)
	_, res, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(ts.URL, "http")+"/ws", nil)
	if err == nil || res == nil || res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401, got %v %v", res, err)
	}
}

func TestHelloAndSnapshot(t *testing.T) {
	_, ts := setup(t)
	w := dial(t, ts, loginWS(t, ts, "Ada", false), 0)
	hello := w.expect("hello")
	you := hello["you"].(map[string]any)
	if you["name"] != "Ada" || you["role"] != "participant" {
		t.Fatalf("hello: %v", hello)
	}
	state := frame(w.expect("snapshot")["state"].(map[string]any))
	ev := state["event"].(map[string]any)
	if ev["name"] != "Test Camp" || ev["lifecycle"] != "setup" {
		t.Fatalf("event: %v", ev)
	}
	for _, key := range []string{"users", "notes", "regions", "waves", "assignments", "messages", "me"} {
		if _, ok := state[key]; !ok {
			t.Errorf("snapshot missing %q", key)
		}
	}
	if n := len(state["users"].([]any)); n != 1 {
		t.Fatalf("users: %d", n)
	}
}

func TestTwoClientsSeeEachOthersChanges(t *testing.T) {
	_, ts := setup(t)
	org := startEvent(t, ts)
	a, _ := connectFresh(t, ts, loginWS(t, ts, "Ada", false))
	b, _ := connectFresh(t, ts, loginWS(t, ts, "Bob", false))
	org.expectEvent("user_joined")
	org.expectEvent("user_joined")
	a.expectEvent("user_joined") // Bob

	created := a.do("create_note", map[string]any{"title": "Go generics", "bodyMd": "**why**", "x": 100, "y": 50}, "note_created")
	note := created["note"].(map[string]any)
	id := note["id"].(string)
	if note["title"] != "Go generics" || note["x"] != 100.0 || note["color"] != "yellow" || note["authorId"] == "" {
		t.Fatalf("created: %v", note)
	}
	for _, w := range []*wsClient{b, org} {
		if got := w.expectEvent("note_created"); got.event()["note"].(map[string]any)["id"] != id {
			t.Fatalf("other window: %v", got)
		}
	}

	// Bob drags Ada's note; everyone sees the move.
	moved := b.do("move_note", map[string]any{"noteId": id, "x": 400, "y": 300}, "note_moved")
	if moved["x"] != 400.0 || moved["y"] != 300.0 {
		t.Fatalf("moved: %v", moved)
	}
	a.expectEvent("note_moved")
	org.expectEvent("note_moved")

	// Only the author (or a moderator) may edit; Bob may not.
	b.fail("update_note", map[string]any{"noteId": id, "title": "mine now"}, "forbidden")
	upd := a.do("update_note", map[string]any{"noteId": id, "color": "blue"}, "note_updated")
	if upd["color"] != "blue" || upd["title"] != "Go generics" {
		t.Fatalf("update: %v", upd)
	}
	b.expectEvent("note_updated")

	links := a.do("set_links", map[string]any{"noteId": id, "links": []map[string]string{
		{"title": "Slides", "url": "https://example.com/deck", "kind": "slides"},
	}}, "links_set")
	if l := links["links"].([]any); len(l) != 1 || l[0].(map[string]any)["id"] == "" {
		t.Fatalf("links: %v", links)
	}
	b.expectEvent("links_set")

	b.fail("delete_note", map[string]any{"noteId": id}, "forbidden")
	a.do("delete_note", map[string]any{"noteId": id}, "note_deleted")
	b.expectEvent("note_deleted")
	org.expectEvent("note_updated")
	org.expectEvent("links_set")
	org.expectEvent("note_deleted")

	// Every window saw the same strictly increasing sequence.
	if a.lastSeq != b.lastSeq || b.lastSeq != org.lastSeq {
		t.Fatalf("seq mismatch: %d %d %d", a.lastSeq, b.lastSeq, org.lastSeq)
	}
}

func TestOverlapResolvedForEveryone(t *testing.T) {
	_, ts := setup(t)
	org := startEvent(t, ts)
	a, _ := connectFresh(t, ts, loginWS(t, ts, "Ada", false))
	org.expectEvent("user_joined")

	first := a.do("create_note", map[string]any{"title": "one", "x": 0, "y": 0}, "note_created")["note"].(map[string]any)
	org.expectEvent("note_created")
	second := a.do("create_note", map[string]any{"title": "two", "x": 0, "y": 0}, "note_created")["note"].(map[string]any)
	org.expectEvent("note_created")
	if second["x"] != 0.0 || (second["y"] != 120.0 && second["y"] != -120.0) {
		t.Fatalf("second note not nudged off the first: %v", second)
	}

	// Dropping the second note onto the first nudges it; the other window
	// sees the same resolved position.
	moved := a.do("move_note", map[string]any{"noteId": second["id"], "x": 10, "y": 10}, "note_moved")
	seen := org.expectEvent("note_moved").event()
	if moved["x"] != seen["x"] || moved["y"] != seen["y"] {
		t.Fatalf("windows disagree: %v vs %v", moved, seen)
	}
	x, y := moved["x"].(float64), moved["y"].(float64)
	if x < 180 && x > -180 && y < 120 && y > -120 {
		t.Fatalf("still overlapping the first note at %v,%v (first at %v)", x, y, first)
	}
}

func TestLifecycleGating(t *testing.T) {
	_, ts := setup(t)
	org, _ := connectFresh(t, ts, loginWS(t, ts, "Org", true))
	p, _ := connectFresh(t, ts, loginWS(t, ts, "Pat", false))
	org.expectEvent("user_joined")

	p.fail("create_note", map[string]any{"title": "early", "x": 0, "y": 0}, "not_allowed_now")
	org.do("create_note", map[string]any{"title": "dressing the board", "x": 0, "y": 0}, "note_created")
	p.expectEvent("note_created")
	p.fail("set_lifecycle", map[string]any{"lifecycle": "active"}, "forbidden")

	org.do("set_lifecycle", map[string]any{"lifecycle": "active"}, "lifecycle_set")
	p.expectEvent("lifecycle_set")
	p.do("create_note", map[string]any{"title": "now", "x": 500, "y": 0}, "note_created")
	org.expectEvent("note_created")

	org.fail("set_lifecycle", map[string]any{"lifecycle": "setup"}, "not_allowed_now")
	org.do("set_lifecycle", map[string]any{"lifecycle": "done"}, "lifecycle_set")
	p.expectEvent("lifecycle_set")
	org.fail("create_note", map[string]any{"title": "late", "x": 0, "y": 900}, "not_allowed_now")
}

func TestBadCommands(t *testing.T) {
	_, ts := setup(t)
	org := startEvent(t, ts)
	org.fail("launch_rockets", map[string]any{}, "bad_request")
	org.fail("create_note", map[string]any{"title": "", "x": 0, "y": 0}, "bad_request")
	org.fail("create_note", map[string]any{"title": "no coords"}, "bad_request")
	org.fail("create_note", map[string]any{"title": "x", "x": 0, "y": 0, "bogus": 1}, "bad_request")
	org.fail("create_note", map[string]any{"title": "x", "x": 1e12, "y": 0}, "bad_request")
	org.fail("create_note", map[string]any{"title": "x", "x": 0, "y": 0, "color": "red"}, "bad_request")
	org.fail("move_note", map[string]any{"noteId": "nope", "x": 0, "y": 0}, "bad_request")
	org.fail("set_links", map[string]any{"noteId": "nope", "links": []any{}}, "bad_request")
}

func TestReconnectReplaysOrSnapshots(t *testing.T) {
	s := newTestServerRing(t, builtDist, 4)
	ts := httptest.NewServer(s)
	t.Cleanup(ts.Close)
	orgClient := loginWS(t, ts, "Org", true)
	org, _ := connectFresh(t, ts, orgClient)
	org.do("set_lifecycle", map[string]any{"lifecycle": "active"}, "lifecycle_set")
	mark := org.lastSeq

	org.do("create_note", map[string]any{"title": "a", "x": 0, "y": 0}, "note_created")
	org.do("create_note", map[string]any{"title": "b", "x": 400, "y": 0}, "note_created")

	// Within the ring: replay exactly the missed events, no snapshot.
	w := dial(t, ts, orgClient, mark)
	if h := w.expect("hello"); int64(h["eventSeq"].(float64)) != org.lastSeq {
		t.Fatalf("hello eventSeq: %v", h)
	}
	if w.expectEvent("note_created").seq() != mark+1 || w.expectEvent("note_created").seq() != mark+2 {
		t.Fatal("replay out of order")
	}
	w.quiet()

	// Up to date: nothing to replay.
	w2 := dial(t, ts, orgClient, org.lastSeq)
	w2.expect("hello")
	w2.quiet()

	// Fall out of the 4-event ring: a snapshot replaces replay.
	for i := 0; i < 5; i++ {
		org.do("create_note", map[string]any{"title": "c", "x": float64(800 + 200*i), "y": 0}, "note_created")
	}
	w3 := dial(t, ts, orgClient, mark)
	w3.expect("hello")
	snap := w3.expect("snapshot")
	if snap.seq() != org.lastSeq || len(snap["state"].(map[string]any)["notes"].([]any)) != 7 {
		t.Fatalf("snapshot seq %d (want %d) notes %v", snap.seq(), org.lastSeq, snap)
	}

	// A client claiming to be ahead of the server (e.g. after a DB reset)
	// also gets a snapshot.
	w4 := dial(t, ts, orgClient, org.lastSeq+100)
	w4.expect("hello")
	w4.expect("snapshot")
}

func TestRestartPreservesBoardAndSeq(t *testing.T) {
	s := newTestServer(t, builtDist)
	ts := httptest.NewServer(s)
	orgClient := loginWS(t, ts, "Org", true)
	org, _ := connectFresh(t, ts, orgClient)
	org.do("set_lifecycle", map[string]any{"lifecycle": "active"}, "lifecycle_set")
	org.do("create_note", map[string]any{"title": "survives", "x": 0, "y": 0}, "note_created")
	lastSeq := org.lastSeq
	ts.Close()
	s.Close()
	select {
	case <-org.closed: // the hub disconnects clients on shutdown
	case <-time.After(5 * time.Second):
		t.Fatal("client not disconnected on shutdown")
	}

	// A new server on the same database, as after a process restart.
	s2, err := New(s.cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s2.Close)
	ts2 := httptest.NewServer(s2)
	t.Cleanup(ts2.Close)
	// Same browser, same cookie (jars key cookies by host, not port).
	c := orgClient

	// The ring is empty after a restart, so a client that is behind gets a
	// snapshot; seq numbering continues where it left off.
	w := dial(t, ts2, c, lastSeq-1)
	hello := w.expect("hello")
	if int64(hello["eventSeq"].(float64)) != lastSeq {
		t.Fatalf("seq not persisted: %v, want %d", hello, lastSeq)
	}
	snap := w.expect("snapshot")
	state := snap["state"].(map[string]any)
	notes := state["notes"].([]any)
	if len(notes) != 1 || notes[0].(map[string]any)["title"] != "survives" || state["event"].(map[string]any)["lifecycle"] != "active" {
		t.Fatalf("board not preserved: %v", state)
	}
	w.do("create_note", map[string]any{"title": "after", "x": 600, "y": 0}, "note_created")
	if w.lastSeq != lastSeq+1 {
		t.Fatalf("seq after restart = %d, want %d", w.lastSeq, lastSeq+1)
	}
}

func TestHiddenNotesFiltered(t *testing.T) {
	s, ts := setup(t)
	org := startEvent(t, ts)
	modClient := loginWS(t, ts, "Mo", false)
	pClient := loginWS(t, ts, "Pat", false)
	org.expectEvent("user_joined")
	org.expectEvent("user_joined")
	mo, _ := s.cfg.Store.UserByName(context.Background(), s.cfg.EventID, "Mo")
	modID := mo.ID
	s.cfg.Store.SetUserRole(context.Background(), modID, "moderator")

	visible := org.do("create_note", map[string]any{"title": "fine", "x": 0, "y": 0}, "note_created")["note"].(map[string]any)
	hidden := org.do("create_note", map[string]any{"title": "nsfw", "x": 0, "y": 0}, "note_created")["note"].(map[string]any)
	// Hide directly in the database until moderation commands land (M8),
	// then restart the hub so its geometry cache sees it.
	if err := s.cfg.Store.HideNote(context.Background(), hidden["id"].(string), modID, "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s2, err := New(s.cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s2.Close)
	ts2 := httptest.NewServer(s2)
	t.Cleanup(ts2.Close)

	p, pState := connectFresh(t, ts2, pClient)
	mod, modState := connectFresh(t, ts2, modClient)
	pNotes := pState["notes"].([]any)
	if len(pNotes) != 1 || pNotes[0].(map[string]any)["id"] != visible["id"] {
		t.Fatalf("participant snapshot leaks hidden note: %v", pNotes)
	}
	modNotes := modState["notes"].([]any)
	if len(modNotes) != 2 || modNotes[1].(map[string]any)["hidden"] != true {
		t.Fatalf("moderator snapshot should flag the hidden note: %v", modNotes)
	}

	// To a participant the hidden note does not exist.
	p.fail("update_note", map[string]any{"noteId": hidden["id"], "title": "x"}, "bad_request")
	p.fail("move_note", map[string]any{"noteId": hidden["id"], "x": 0, "y": 0}, "bad_request")
	// A moderator can edit it, but participants never hear about it.
	mod.do("update_note", map[string]any{"noteId": hidden["id"], "bodyMd": "redacted"}, "note_updated")
	p.quiet()
	mod.fail("move_note", map[string]any{"noteId": hidden["id"], "x": 0, "y": 0}, "not_allowed_now")

	// Hidden notes don't block placement: creating a note exactly on the
	// hidden one is not nudged by it (only by the visible note at 0,0).
	created := p.do("create_note", map[string]any{"title": "here", "x": 0, "y": hidden["y"]}, "note_created")["note"].(map[string]any)
	if created["y"] != hidden["y"] {
		t.Fatalf("placement revealed the hidden note: %v vs %v", created, hidden)
	}
	mod.expectEvent("note_created")
}

func TestMovesAreCoalesced(t *testing.T) {
	_, ts := setup(t)
	org := startEvent(t, ts)
	watcher, _ := connectFresh(t, ts, loginWS(t, ts, "Wat", false))
	org.expectEvent("user_joined")
	id := org.do("create_note", map[string]any{"title": "drag me", "x": 0, "y": 0}, "note_created")["note"].(map[string]any)["id"]
	watcher.expectEvent("note_created")

	const moves = 12
	var ids []string
	for i := 1; i <= moves; i++ {
		ids = append(ids, org.send("move_note", map[string]any{"noteId": id, "x": float64(i * 20), "y": 0}))
	}
	acked := map[string]bool{}
	var events []frame
	for len(acked) < moves {
		f := org.next()
		switch f.str("type") {
		case "ack":
			acked[f.str("cmdId")] = true
		case "event":
			events = append(events, f)
		default:
			t.Fatalf("unexpected %v", f)
		}
	}
	for _, cid := range ids {
		if !acked[cid] {
			t.Fatalf("move %s never acked", cid)
		}
	}
	if len(events) >= moves {
		t.Fatalf("%d moves produced %d events; expected coalescing", moves, len(events))
	}
	last := events[len(events)-1].event()
	if last["x"] != float64(moves*20) {
		t.Fatalf("final position %v, want x=%d", last, moves*20)
	}
	for range events {
		watcher.expectEvent("note_moved")
	}
}

func TestRateLimit(t *testing.T) {
	_, ts := setup(t)
	w, _ := connectFresh(t, ts, loginWS(t, ts, "Spam", false))
	ping := []byte(`{"type":"ping"}`)
	for i := 0; i < int(rateBurst)+maxViolations+10; i++ {
		if err := w.conn.Write(context.Background(), websocket.MessageText, ping); err != nil {
			break // closed by the server
		}
	}
	pongs, limited := 0, 0
	for {
		select {
		case f := <-w.frames:
			switch {
			case f.str("type") == "pong":
				pongs++
			case f.str("code") == "rate_limited":
				limited++
			}
			continue
		case <-w.closed:
		case <-time.After(5 * time.Second):
			t.Fatal("connection not closed after repeated violations")
		}
		break
	}
	if pongs < int(rateBurst) || limited == 0 {
		t.Fatalf("pongs=%d limited=%d", pongs, limited)
	}
}

func TestLoginEventsAndPromotion(t *testing.T) {
	_, ts := setup(t)
	pat := loginWS(t, ts, "Pat", false)
	patWS, _ := connectFresh(t, ts, pat)

	loginWS(t, ts, "Newcomer", false)
	joined := patWS.expectEvent("user_joined").event()["user"].(map[string]any)
	if joined["name"] != "Newcomer" || joined["role"] != "participant" {
		t.Fatalf("user_joined: %v", joined)
	}
	loginWS(t, ts, "newcomer", false) // resume: no event
	patWS.quiet()

	// Pat logs in elsewhere with the admin key: the open window learns the
	// new role and gets a fresh snapshot for it.
	loginWS(t, ts, "Pat", true)
	set := patWS.expectEvent("role_set").event()
	if set["role"] != "organizer" {
		t.Fatalf("role_set: %v", set)
	}
	patWS.expect("snapshot")
	patWS.do("set_lifecycle", map[string]any{"lifecycle": "active"}, "lifecycle_set")
}
