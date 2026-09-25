package server

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// loadClient is a bare WebSocket client whose reader counts moves.
type loadClient struct {
	ws      *websocket.Conn
	noteID  chan string // receives this client's own note id once created
	x       float64
	moves   atomic.Int64
	lat     []time.Duration // owned by the reader goroutine until done
	done    chan struct{}
	readErr error
}

// TestLoadSanity (SPEC §14): clients each move their own note once a
// second; every client must receive every move, and p99 latency from send
// to receipt must stay under 250 ms.
//
// By default a short version runs (50 clients, 3 s). UNCONF_LOAD_TEST=1
// runs the full spec'd load: 300 clients for 30 seconds.
func TestLoadSanity(t *testing.T) {
	clients, seconds := 50, 3
	if os.Getenv("UNCONF_LOAD_TEST") != "" {
		clients, seconds = 300, 30
	} else if testing.Short() {
		t.Skip("load test skipped in -short mode")
	}

	_, ts := setup(t)
	startEvent(t, ts)

	var sent sync.Map // "noteID/y" → time.Time the move was sent
	key := func(id string, y float64) string { return fmt.Sprintf("%s/%g", id, y) }
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	u, _ := url.Parse(ts.URL)

	lcs := make([]*loadClient, clients)
	for i := range lcs {
		c := loginWS(t, ts, fmt.Sprintf("user%03d", i), false)
		header := http.Header{}
		for _, ck := range c.http.Jar.Cookies(u) {
			header.Add("Cookie", ck.Name+"="+ck.Value)
		}
		conn, _, err := websocket.Dial(context.Background(), wsURL, &websocket.DialOptions{HTTPHeader: header})
		if err != nil {
			t.Fatal(err)
		}
		conn.SetReadLimit(16 << 20)
		t.Cleanup(func() { conn.CloseNow() })
		lc := &loadClient{ws: conn, noteID: make(chan string, 1), x: float64(i) * 400, done: make(chan struct{})}
		lcs[i] = lc
		go func() {
			defer close(lc.done)
			var me string
			for {
				_, data, err := conn.Read(context.Background())
				if err != nil {
					lc.readErr = err
					return
				}
				now := time.Now()
				var f struct {
					Type  string `json:"type"`
					You   struct{ ID string }
					Event struct {
						Kind   string
						NoteID string
						Y      float64
						Note   struct{ ID, AuthorID string }
					}
				}
				if err := json.Unmarshal(data, &f); err != nil {
					lc.readErr = err
					return
				}
				switch {
				case f.Type == "hello":
					me = f.You.ID
				case f.Event.Kind == "note_created" && f.Event.Note.AuthorID == me:
					lc.noteID <- f.Event.Note.ID
				case f.Event.Kind == "note_moved":
					lc.moves.Add(1)
					if v, ok := sent.Load(key(f.Event.NoteID, f.Event.Y)); ok {
						lc.lat = append(lc.lat, now.Sub(v.(time.Time)))
					}
				}
			}
		}()
		send(t, conn, "create_note", map[string]any{"title": "load", "x": lc.x, "y": 0})
	}
	ids := make([]string, clients)
	for i, lc := range lcs {
		select {
		case ids[i] = <-lc.noteID:
		case <-time.After(10 * time.Second):
			t.Fatalf("client %d never saw its note", i)
		}
	}

	// Each client moves its note once a second from a random phase; y is
	// unique per move so receivers can look up the send time.
	start := time.Now()
	var wg sync.WaitGroup
	for i, lc := range lcs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(time.Duration(rand.IntN(1000)) * time.Millisecond)
			for k := 1; k <= seconds; k++ {
				tick := time.Now()
				y := float64(k * 200)
				sent.Store(key(ids[i], y), time.Now())
				send(t, lc.ws, "move_note", map[string]any{"noteId": ids[i], "x": lc.x, "y": y})
				time.Sleep(time.Second - time.Since(tick))
			}
		}()
	}
	wg.Wait()

	want := int64(clients * seconds)
	deadline := time.Now().Add(10 * time.Second)
	for _, lc := range lcs {
		for lc.moves.Load() < want && time.Now().Before(deadline) && lc.readErr == nil {
			time.Sleep(10 * time.Millisecond)
		}
	}
	elapsed := time.Since(start)
	for _, lc := range lcs {
		lc.ws.CloseNow()
		<-lc.done
	}

	var all []time.Duration
	for i, lc := range lcs {
		if got := lc.moves.Load(); got != want {
			t.Errorf("client %d received %d moves, want %d (read error: %v)", i, got, want, lc.readErr)
		}
		all = append(all, lc.lat...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })
	if len(all) == 0 {
		t.Fatal("no latency samples")
	}
	p50, p99, maxLat := all[len(all)/2], all[len(all)*99/100], all[len(all)-1]
	t.Logf("%d clients × %d moves in %v: %d deliveries, p50 %v, p99 %v, max %v",
		clients, seconds, elapsed.Round(time.Millisecond), len(all), p50, p99, maxLat)
	if p99 >= 250*time.Millisecond {
		t.Errorf("p99 broadcast latency %v ≥ 250ms", p99)
	}
}

func send(t *testing.T, conn *websocket.Conn, cmd string, payload any) {
	b, _ := json.Marshal(map[string]any{"type": "cmd", "cmdId": "x", "cmd": cmd, "payload": payload})
	if err := conn.Write(context.Background(), websocket.MessageText, b); err != nil {
		t.Errorf("write %s: %v", cmd, err)
	}
}
