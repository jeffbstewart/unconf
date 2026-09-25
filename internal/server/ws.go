package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/jeffbstewart/unconf/internal/domain"
	"github.com/jeffbstewart/unconf/internal/store"
)

const (
	sendBuffer    = 1024
	maxFrameBytes = 64 << 10
	writeTimeout  = 10 * time.Second
	pingInterval  = 25 * time.Second

	// Token bucket per connection (SPEC §8.2).
	rateLimit = 20.0 // commands per second, sustained
	rateBurst = 60.0
	// Consecutive rate-limit violations before the connection is closed.
	maxViolations = 30
)

// conn is one WebSocket client. user is owned by the hub goroutine after
// registration; send is safe from any goroutine.
type conn struct {
	user     store.User
	send     chan []byte
	cancel   context.CancelFunc
	killOnce sync.Once
	ws       *websocket.Conn
}

// enqueue queues a frame without blocking. A client too slow to drain its
// buffer is disconnected; it will reconnect and catch up via replay or
// snapshot.
func (c *conn) enqueue(frame []byte) {
	select {
	case c.send <- frame:
	default:
		c.kill("too slow to keep up")
	}
}

// kill closes the connection once, from any goroutine.
func (c *conn) kill(reason string) {
	c.killOnce.Do(func() {
		c.cancel()
		if c.ws != nil {
			go c.ws.Close(websocket.StatusPolicyViolation, reason)
		}
	})
}

// tokenBucket is a simple rate limiter; not safe for concurrent use.
type tokenBucket struct {
	tokens float64
	last   time.Time
}

func (b *tokenBucket) allow(now time.Time) bool {
	if b.last.IsZero() {
		b.tokens, b.last = rateBurst, now
	}
	b.tokens = min(rateBurst, b.tokens+now.Sub(b.last).Seconds()*rateLimit)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// handleWS serves GET /ws?since=<seq>.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	var since int64
	if v := r.URL.Query().Get("since"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "since must be a non-negative integer")
			return
		}
		since = n
	}
	ws, err := websocket.Accept(w, r, nil) // default: same-origin only
	if err != nil {
		return // Accept has written the HTTP error
	}
	ws.SetReadLimit(maxFrameBytes)

	ctx, cancel := context.WithCancel(s.ctx)
	c := &conn{user: userFrom(r.Context()), send: make(chan []byte, sendBuffer), cancel: cancel, ws: ws}
	defer c.kill("closed")

	go c.writeLoop(ctx)
	if err := s.hub.Register(ctx, c, since); err != nil {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, errHubStopped) {
			log.Printf("ws: register %s: %v", c.user.ID, err)
		}
		ws.Close(websocket.StatusInternalError, "registration failed")
		return
	}
	defer s.hub.Unregister(c)
	c.readLoop(ctx, s.hub)
}

func (c *conn) writeLoop(ctx context.Context) {
	ping := time.NewTicker(pingInterval)
	defer ping.Stop()
	for {
		select {
		case frame := <-c.send:
			wctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.ws.Write(wctx, websocket.MessageText, frame)
			cancel()
			if err != nil {
				c.kill("write failed")
				return
			}
		case <-ping.C:
			pctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.ws.Ping(pctx)
			cancel()
			if err != nil {
				c.kill("ping failed")
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func (c *conn) readLoop(ctx context.Context, h *Hub) {
	var bucket tokenBucket
	violations := 0
	for {
		typ, data, err := c.ws.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) == websocket.StatusMessageTooBig {
				log.Printf("ws: %s sent an oversized frame", c.user.ID)
			}
			return
		}
		var f clientFrame
		if typ != websocket.MessageText || json.Unmarshal(data, &f) != nil {
			c.enqueue(errorFrame("", domain.BadRequest("frames must be JSON text")))
			continue
		}
		if !bucket.allow(time.Now()) {
			violations++
			if violations >= maxViolations {
				c.kill("rate limit exceeded")
				return
			}
			c.enqueue(errorFrame(f.CmdID, &domain.CmdError{Code: domain.CodeRateLimited, Message: "too many commands; slow down"}))
			continue
		}
		violations = 0
		switch f.Type {
		case "ping":
			c.enqueue(pongFrame)
		case "cmd":
			h.Submit(c, f)
		default:
			c.enqueue(errorFrame(f.CmdID, domain.BadRequest("unknown frame type %q", f.Type)))
		}
	}
}
