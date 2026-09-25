package server

// The hub (SPEC §8.1) is a single goroutine that owns every write to the
// database, the global event sequence, the ring buffer of recent events,
// and the set of connected clients. Everything that touches that state is
// a closure run on the hub goroutine, so none of it needs locks.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/jeffbstewart/unconf/internal/domain"
	"github.com/jeffbstewart/unconf/internal/store"
)

const (
	defaultRingSize = 10000
	// A reconnecting client is caught up by replay only if that fits
	// comfortably in its send buffer; otherwise it gets a snapshot.
	maxReplay = sendBuffer / 2
	// moveInterval spaces applied moves of one note: ≤ 20 per second.
	moveInterval = 50 * time.Millisecond
)

// outEvent is one sequenced event, pre-encoded per audience.
type outEvent struct {
	seq      int64
	forPart  []byte // frame for participants; nil = not delivered
	forMod   []byte // frame for moderators and organizers; nil = not delivered
	onlyUser string // if set, delivered only to this user's connections
}

func (e *outEvent) frameFor(c *conn) []byte {
	if e.onlyUser != "" && e.onlyUser != c.user.ID {
		return nil
	}
	if c.user.Role.AtLeast(domain.RoleModerator) {
		return e.forMod
	}
	return e.forPart
}

// ring is a fixed-capacity buffer of the most recent events.
type ring struct {
	buf   []outEvent
	start int // index of the oldest event
	n     int
}

func newRing(size int) *ring { return &ring{buf: make([]outEvent, size)} }

func (r *ring) push(e outEvent) {
	if r.n < len(r.buf) {
		r.buf[(r.start+r.n)%len(r.buf)] = e
		r.n++
		return
	}
	r.buf[r.start] = e
	r.start = (r.start + 1) % len(r.buf)
}

// after returns the events with seq > since, or ok=false if some of them
// have already been evicted.
func (r *ring) after(since, current int64) ([]outEvent, bool) {
	if since == current {
		return nil, true
	}
	if r.n == 0 || since > current || r.buf[r.start].seq > since+1 {
		return nil, false
	}
	var out []outEvent
	for i := 0; i < r.n; i++ {
		if e := r.buf[(r.start+i)%len(r.buf)]; e.seq > since {
			out = append(out, e)
		}
	}
	return out, true
}

// noteGeo caches what overlap resolution and region tagging need about
// each note.
type noteGeo struct {
	x, y     float64
	hidden   bool
	regionID string
}

// pendingMove is a coalesced move waiting for its note's next move slot.
type pendingMove struct {
	c       *conn
	payload movePayload
	cmdIDs  map[*conn][]string // every command this move answers
}

// Hub serializes all state changes.
type Hub struct {
	st      *store.Store
	eventID string
	ctx     context.Context
	ops     chan func()
	done    chan struct{}

	// Owned by the hub goroutine.
	seq       int64
	ring      *ring
	clients   map[*conn]struct{}
	lifecycle domain.Lifecycle
	geo       map[string]*noteGeo
	regions   map[string]domain.RegionShape
	lastMove  map[string]time.Time
	pending   map[string]*pendingMove
}

func newHub(ctx context.Context, st *store.Store, eventID string, ringSize int) (*Hub, error) {
	if ringSize <= 0 {
		ringSize = defaultRingSize
	}
	h := &Hub{
		st: st, eventID: eventID, ctx: ctx,
		ops:      make(chan func(), 256),
		done:     make(chan struct{}),
		ring:     newRing(ringSize),
		clients:  map[*conn]struct{}{},
		geo:      map[string]*noteGeo{},
		regions:  map[string]domain.RegionShape{},
		lastMove: map[string]time.Time{},
		pending:  map[string]*pendingMove{},
	}
	var err error
	if h.seq, err = st.Seq(ctx); err != nil {
		return nil, err
	}
	ev, err := st.Event(ctx, eventID)
	if err != nil {
		return nil, err
	}
	h.lifecycle = domain.Lifecycle(ev.Lifecycle)
	notes, err := st.Notes(ctx, eventID)
	if err != nil {
		return nil, err
	}
	for _, n := range notes {
		h.geo[n.ID] = &noteGeo{n.X, n.Y, n.Hidden(), n.RegionID}
	}
	regions, err := st.Regions(ctx, eventID)
	if err != nil {
		return nil, err
	}
	for _, r := range regions {
		h.regions[r.ID] = regionShape(r)
	}
	return h, nil
}

// run processes operations until ctx is cancelled.
func (h *Hub) run() {
	defer close(h.done)
	for {
		select {
		case op := <-h.ops:
			op()
		case <-h.ctx.Done():
			for c := range h.clients {
				c.kill("server shutting down")
			}
			return
		}
	}
}

var errHubStopped = errors.New("hub stopped")

// post queues op for the hub goroutine without waiting for it to run.
func (h *Hub) post(op func()) error {
	select {
	case h.ops <- op:
		return nil
	case <-h.done:
		return errHubStopped
	}
}

// call runs op on the hub goroutine and waits for it to finish.
func (h *Hub) call(ctx context.Context, op func()) error {
	finished := make(chan struct{})
	if err := h.post(func() { op(); close(finished) }); err != nil {
		return err
	}
	select {
	case <-finished:
		return nil
	case <-h.done:
		return errHubStopped
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Register adds a connection and brings it up to date: hello, then either a
// replay of the events after since or a full snapshot.
func (h *Hub) Register(ctx context.Context, c *conn, since int64) error {
	var err error
	callErr := h.call(ctx, func() {
		// Re-read the user: role may have changed since the HTTP handshake.
		u, e := h.st.UserByID(h.ctx, c.user.ID)
		if e != nil {
			err = e
			return
		}
		c.user = u
		h.clients[c] = struct{}{}
		c.enqueue(helloFrame(c.user, h.seq))
		if since > 0 {
			if evs, ok := h.ring.after(since, h.seq); ok && len(evs) <= maxReplay {
				for i := range evs {
					if f := evs[i].frameFor(c); f != nil {
						c.enqueue(f)
					}
				}
				return
			}
		}
		h.sendSnapshot(c)
	})
	if callErr != nil {
		return callErr
	}
	return err
}

// Unregister removes a connection.
func (h *Hub) Unregister(c *conn) {
	_ = h.post(func() { delete(h.clients, c) })
}

// Submit queues a client command.
func (h *Hub) Submit(c *conn, f clientFrame) {
	_ = h.post(func() { h.handleCommand(c, f) })
}

func (h *Hub) sendSnapshot(c *conn) {
	snap, err := h.buildSnapshot(c.user)
	if err != nil {
		log.Printf("hub: snapshot for %s: %v", c.user.ID, err)
		c.kill("internal error")
		return
	}
	c.enqueue(snapshotFrame(h.seq, snap))
}

// change is the result of applying a command: events to publish and cache
// updates to make once the transaction has committed.
type change struct {
	events []eventSpec
	after  func()
}

// eventSpec describes one event and who receives it.
type eventSpec struct {
	part, mod E      // body per audience; nil = that audience skips it
	onlyUser  string // personal events (stars)
}

// toAll is an event every connection receives.
func toAll(ev E) eventSpec { return eventSpec{part: ev, mod: ev} }

// commit runs fn in a transaction that also advances the persisted seq by
// the number of events fn returns, then publishes them. On error nothing is
// published and no cache changes.
func (h *Hub) commit(fn func(tx store.Tx) (change, *domain.CmdError)) (int64, *domain.CmdError) {
	var ch change
	var cmdErr *domain.CmdError
	err := h.st.InTx(h.ctx, func(tx store.Tx) error {
		ch, cmdErr = fn(tx)
		if cmdErr != nil {
			return cmdErr
		}
		if len(ch.events) == 0 {
			return nil
		}
		return tx.SetSeq(h.ctx, h.seq+int64(len(ch.events)))
	})
	if cmdErr != nil {
		if cmdErr.Code == domain.CodeInternal {
			log.Printf("hub: %v", cmdErr.Cause)
		}
		return 0, cmdErr
	}
	if err != nil {
		log.Printf("hub: transaction: %v", err)
		return 0, internalErr
	}
	if ch.after != nil {
		ch.after()
	}
	for _, spec := range ch.events {
		h.publish(spec)
	}
	return h.seq, nil
}

// publish assigns the next seq to spec, records it, and fans it out.
func (h *Hub) publish(spec eventSpec) {
	h.seq++
	ev := outEvent{seq: h.seq, onlyUser: spec.onlyUser}
	if spec.part != nil {
		ev.forPart = eventFrame(h.seq, spec.part)
	}
	if spec.mod != nil {
		ev.forMod = eventFrame(h.seq, spec.mod)
	}
	h.ring.push(ev)
	for c := range h.clients {
		if f := ev.frameFor(c); f != nil {
			c.enqueue(f)
		}
	}
}

func (h *Hub) handleCommand(c *conn, f clientFrame) {
	if f.CmdID == "" {
		c.enqueue(errorFrame("", domain.BadRequest("cmdId is required")))
		return
	}
	actor := domain.Actor{UserID: c.user.ID, Role: c.user.Role}
	if f.Cmd == "move_note" {
		h.handleMove(c, actor, f)
		return
	}
	handler, ok := commandHandlers[f.Cmd]
	if !ok {
		c.enqueue(errorFrame(f.CmdID, domain.BadRequest("unknown command %q", f.Cmd)))
		return
	}
	seq, cmdErr := h.commit(func(tx store.Tx) (change, *domain.CmdError) {
		return handler(h, tx, actor, f.Payload)
	})
	if cmdErr != nil {
		c.enqueue(errorFrame(f.CmdID, cmdErr))
		return
	}
	c.enqueue(ackFrame(f.CmdID, seq))
}

// handleMove applies a move now, or coalesces it with later moves of the
// same note so each note moves at most 20 times a second (SPEC §8.2).
func (h *Hub) handleMove(c *conn, actor domain.Actor, f clientFrame) {
	var p movePayload
	if err := decodePayload(f.Payload, &p); err != nil {
		c.enqueue(errorFrame(f.CmdID, err))
		return
	}
	if pm, ok := h.pending[p.NoteID]; ok {
		pm.c, pm.payload = c, p
		pm.cmdIDs[c] = append(pm.cmdIDs[c], f.CmdID)
		return
	}
	pm := &pendingMove{c: c, payload: p, cmdIDs: map[*conn][]string{c: {f.CmdID}}}
	wait := moveInterval - time.Since(h.lastMove[p.NoteID])
	if wait <= 0 {
		h.applyMove(actor, pm)
		return
	}
	h.pending[p.NoteID] = pm
	time.AfterFunc(wait, func() {
		_ = h.post(func() {
			delete(h.pending, p.NoteID)
			h.applyMove(domain.Actor{UserID: pm.c.user.ID, Role: pm.c.user.Role}, pm)
		})
	})
}

func (h *Hub) applyMove(actor domain.Actor, pm *pendingMove) {
	h.lastMove[pm.payload.NoteID] = time.Now()
	seq, cmdErr := h.commit(func(tx store.Tx) (change, *domain.CmdError) {
		return h.moveNote(tx, actor, pm.payload)
	})
	for c, ids := range pm.cmdIDs {
		for _, id := range ids {
			if cmdErr != nil {
				c.enqueue(errorFrame(id, cmdErr))
			} else {
				c.enqueue(ackFrame(id, seq))
			}
		}
	}
}

// Login resumes or creates a user by name (asserted identity, SPEC §6) on
// the hub, so new users and admin-key promotions are sequenced events like
// any other change.
func (h *Hub) Login(ctx context.Context, name, email string, asOrganizer bool) (store.User, error) {
	var u store.User
	var err error
	callErr := h.call(ctx, func() { u, err = h.login(name, email, asOrganizer) })
	if callErr != nil {
		return store.User{}, callErr
	}
	return u, err
}

func (h *Hub) login(name, email string, asOrganizer bool) (store.User, error) {
	var u store.User
	var promoted bool
	var loginErr error
	_, cmdErr := h.commit(func(tx store.Tx) (change, *domain.CmdError) {
		existing, err := tx.UserByName(h.ctx, h.eventID, name)
		switch {
		case errors.Is(err, store.ErrNotFound):
			u = store.User{
				ID: domain.NewID(), EventID: h.eventID, Name: name, Email: email,
				Role: domain.RoleParticipant, CreatedAt: domain.Timestamp(time.Now()),
			}
			if asOrganizer {
				u.Role = domain.RoleOrganizer
			}
			if loginErr = tx.CreateUser(h.ctx, u); loginErr != nil {
				return change{}, internalErr
			}
			if asOrganizer {
				if loginErr = auditRoleChange(h.ctx, tx, h.eventID, u.ID, "", u.Role); loginErr != nil {
					return change{}, internalErr
				}
			}
			return change{events: []eventSpec{toAll(E{"kind": "user_joined", "user": toUserJSON(u)})}}, nil
		case err != nil:
			loginErr = err
			return change{}, internalErr
		}
		u = existing
		if email != "" && email != u.Email {
			if loginErr = tx.SetUserEmail(h.ctx, u.ID, email); loginErr != nil {
				return change{}, internalErr
			}
			u.Email = email
		}
		if asOrganizer && u.Role != domain.RoleOrganizer {
			if loginErr = tx.SetUserRole(h.ctx, u.ID, domain.RoleOrganizer); loginErr != nil {
				return change{}, internalErr
			}
			if loginErr = auditRoleChange(h.ctx, tx, h.eventID, u.ID, u.Role, domain.RoleOrganizer); loginErr != nil {
				return change{}, internalErr
			}
			u.Role = domain.RoleOrganizer
			promoted = true
			return change{events: []eventSpec{toAll(E{"kind": "role_set", "userId": u.ID, "role": u.Role})}}, nil
		}
		return change{}, nil
	})
	if cmdErr != nil {
		if loginErr == nil {
			loginErr = cmdErr
		}
		return store.User{}, fmt.Errorf("login %q: %w", name, loginErr)
	}
	if promoted {
		h.refreshRole(u)
	}
	return u, nil
}

// refreshRole updates a user's open connections after a role change and
// resends their snapshot, since what they may see has changed.
func (h *Hub) refreshRole(u store.User) {
	for c := range h.clients {
		if c.user.ID == u.ID {
			c.user.Role = u.Role
			h.sendSnapshot(c)
		}
	}
}

var internalErr = &domain.CmdError{Code: domain.CodeInternal, Message: "internal error"}

// wait blocks until the hub goroutine exits (after its context is cancelled).
func (h *Hub) wait() { <-h.done }

// clientCount reports the number of registered connections (for tests).
func (h *Hub) clientCount() int {
	var n int
	_ = h.call(context.Background(), func() { n = len(h.clients) })
	return n
}
