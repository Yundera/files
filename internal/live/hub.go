// Package live pushes state changes to connected browsers.
//
// Ported from packages/maison's internal/live. Same envelope, same subscribe
// protocol, same slow-client policy — so a fix in either app ports to the other
// by inspection. One endpoint (GET /ws) multiplexes named channels rather than
// opening a socket per concern, because a file manager already holds a socket
// per upload and per media stream and browsers cap connections per origin.
package live

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Channels. Each is its own subscription so a page showing no jobs pays nothing
// for job traffic.
const (
	// ChannelJobs carries copy/move/delete progress and completion.
	ChannelJobs = "jobs"
	// ChannelDir announces that a directory's contents changed, so an open
	// listing can refresh itself. Carries the path, not the new listing: the
	// client re-reads only if it is actually looking at that directory.
	ChannelDir = "dir"
	// ChannelTrash announces that the trash changed.
	ChannelTrash = "trash"
)

// Envelope is the wire format in both directions.
//
// Client -> server: {"type":"subscribe","channel":"jobs"} / "unsubscribe".
// Server -> client: Type and Channel both name the channel, Data is the payload.
type Envelope struct {
	Type    string          `json:"type"`
	Channel string          `json:"channel,omitempty"`
	ID      string          `json:"id,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Hub fans out to every subscribed client.
type Hub struct {
	mu      sync.Mutex
	clients map[*client]struct{}

	// Snapshot producers, assigned by the server. A client that subscribes gets
	// the current state immediately rather than waiting for the next change —
	// otherwise a page opened mid-copy shows no progress bar until the next tick.
	// Nil is tolerated: an unwired channel simply sends no snapshot.
	JobsSnapshot  func() any
	TrashSnapshot func() any
}

func NewHub() *Hub {
	return &Hub{clients: make(map[*client]struct{})}
}

type client struct {
	conn *websocket.Conn
	send chan []byte
	mu   sync.Mutex
	subs map[string]bool
}

func (c *client) subscribed(ch string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.subs[ch]
}

// trySend drops rather than blocks.
//
// A browser that has stopped reading must not be able to stall the hub and with
// it every other client. Progress messages are snapshots of current state, so a
// dropped one is superseded by the next; the throttled broadcaster always sends
// a trailing update, which is what guarantees the FINAL state still arrives.
func (c *client) trySend(msg []byte) {
	select {
	case c.send <- msg:
	default:
	}
}

// Broadcast marshals once and fans out to subscribers of ch.
func (h *Hub) Broadcast(channel string, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		return
	}
	msg, err := json.Marshal(Envelope{Type: channel, Channel: channel, Data: raw})
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if c.subscribed(channel) {
			c.trySend(msg)
		}
	}
}

// BroadcastLazy calls produce only when somebody is listening, so building an
// expensive payload costs nothing on an idle box.
func (h *Hub) BroadcastLazy(channel string, produce func() any) {
	if !h.anySubscribed(channel) {
		return
	}
	h.Broadcast(channel, produce())
}

func (h *Hub) anySubscribed(channel string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if c.subscribed(channel) {
			return true
		}
	}
	return false
}

func (h *Hub) add(c *client) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) remove(c *client) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
	close(c.send)
}

// ServeWS upgrades and runs the two pumps for one client.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	// OriginPatterns "*" because the app is reached on several hostnames (the
	// gateway domain plus two nip.io/sslip.io spellings) and sits behind a gate
	// that has already authenticated the request. The socket exposes no mutation
	// — it is subscribe-only — so origin is not the control that matters here.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
	if err != nil {
		return
	}
	c := &client{conn: conn, send: make(chan []byte, 32), subs: map[string]bool{}}
	h.add(c)
	defer func() {
		h.remove(c)
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go c.writePump(ctx)
	c.readPump(ctx, h)
}

func (c *client) writePump(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-c.send:
			if !ok {
				return
			}
			wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := c.conn.Write(wctx, websocket.MessageText, msg)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

func (c *client) readPump(ctx context.Context, h *Hub) {
	for {
		_, data, err := c.conn.Read(ctx)
		if err != nil {
			return
		}
		var env Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			continue
		}
		switch env.Type {
		case "subscribe":
			c.mu.Lock()
			c.subs[env.Channel] = true
			c.mu.Unlock()
			h.sendSnapshot(c, env.Channel)
		case "unsubscribe":
			c.mu.Lock()
			delete(c.subs, env.Channel)
			c.mu.Unlock()
		}
	}
}

func (h *Hub) sendSnapshot(c *client, channel string) {
	var produce func() any
	switch channel {
	case ChannelJobs:
		produce = h.JobsSnapshot
	case ChannelTrash:
		produce = h.TrashSnapshot
	}
	if produce == nil {
		return
	}
	raw, err := json.Marshal(produce())
	if err != nil {
		return
	}
	msg, err := json.Marshal(Envelope{Type: channel, Channel: channel, Data: raw})
	if err != nil {
		log.Printf("live: snapshot for %s: %v", channel, err)
		return
	}
	c.trySend(msg)
}

// Throttle returns a function that runs fn at most once per d, always running a
// trailing call so the final state is never the one that got dropped.
//
// 300ms is maison's cadence for exactly this, and it is what makes a copy of ten
// thousand small files cost ~3 broadcasts per second rather than ten thousand.
func Throttle(d time.Duration, fn func()) func() {
	var (
		mu      sync.Mutex
		pending bool
		last    time.Time
	)
	return func() {
		mu.Lock()
		if time.Since(last) >= d {
			last = time.Now()
			mu.Unlock()
			fn() // called unlocked: fn may broadcast, which takes the hub lock
			return
		}
		if pending {
			mu.Unlock()
			return
		}
		pending = true
		wait := d - time.Since(last)
		mu.Unlock()

		// The trailing call. Without it the last update of a burst — the one
		// that says "done" — is the one most likely to be swallowed.
		time.AfterFunc(wait, func() {
			mu.Lock()
			pending = false
			last = time.Now()
			mu.Unlock()
			fn()
		})
	}
}
