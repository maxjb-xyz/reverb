package playback

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"sync"
	"time"
)

// Store keeps each account's last state so it survives a restart. Load returns
// (nil, nil) when nothing is saved.
type Store interface {
	LoadState(ctx context.Context, userID string) (*State, error)
	SaveState(ctx context.Context, userID string, s *State) error
}

// outBuffer is how many frames may wait for a slow device before it is dropped.
const outBuffer = 64

// Hub holds the sessions of every account with a device connected.
type Hub struct {
	store Store
	now   func() time.Time
	log   *slog.Logger

	mu       sync.Mutex
	sessions map[string]*session

	// Saves run off the hub lock; seq keeps a slow older write from landing
	// after a newer one.
	saveMu  sync.Mutex
	saveSeq uint64
	saved   map[string]uint64
	saves   sync.WaitGroup
	// stopped (guarded by mu) turns saves off once Shutdown has written the
	// final states, so no save starts while Shutdown waits.
	stopped      bool
	shutdownOnce sync.Once
}

type session struct {
	userID string
	conns  map[*Conn]struct{}
	// active is the active device's connection, nil when none.
	active *Conn
	state  *State
	// dirty marks a state not yet saved.
	dirty bool
}

// Conn is one connected device. Read frames for the device from Out; stop
// reading when Done closes. Call Close when the device goes away.
type Conn struct {
	hub    *Hub
	userID string
	Device DeviceInfo

	out    chan Message
	done   chan struct{}
	closed bool // guarded by hub.mu
}

// NewHub makes a hub. store may be nil (state is then kept in memory only).
func NewHub(store Store, now func() time.Time, log *slog.Logger) *Hub {
	if now == nil {
		now = time.Now
	}
	if log == nil {
		log = slog.Default()
	}
	return &Hub{store: store, now: now, log: log, sessions: map[string]*session{}, saved: map[string]uint64{}}
}

// Out carries frames for the device.
func (c *Conn) Out() <-chan Message { return c.out }

// Done closes when the hub drops the device: it was replaced by a newer
// connection with the same ID, fell too far behind, or was closed.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Connect registers a device for userID. A device already connected with the
// same ID is replaced (a reloaded tab, a reconnect before the old socket
// noticed it was gone). A new connection is never active: a device that is
// playing claims the session with MsgClaim.
func (h *Hub) Connect(ctx context.Context, userID string, dev DeviceInfo) (*Conn, bool) {
	if userID == "" || !sanitizeDevice(&dev) {
		return nil, false
	}
	h.ensureLoaded(ctx, userID)

	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.sessionLocked(userID)
	for old := range s.conns {
		if old.Device.ID == dev.ID {
			h.dropLocked(s, old)
		}
	}
	if len(s.conns) >= MaxDevices {
		return nil, false
	}
	c := &Conn{hub: h, userID: userID, Device: dev, out: make(chan Message, outBuffer), done: make(chan struct{})}
	s.conns[c] = struct{}{}
	h.broadcastLocked(s)
	return c, true
}

// ensureLoaded reads the saved state of an account with no session yet.
func (h *Hub) ensureLoaded(ctx context.Context, userID string) {
	h.mu.Lock()
	_, ok := h.sessions[userID]
	h.mu.Unlock()
	if ok || h.store == nil {
		return
	}
	st, err := h.store.LoadState(ctx, userID)
	if err != nil {
		h.log.Warn("playback: load state", "err", err)
		return
	}
	if st != nil && !sanitizeState(st) {
		st = nil
	}
	if st != nil {
		st.Playing = false // nothing is playing after a restart
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.sessions[userID]; !ok {
		s := h.sessionLocked(userID)
		s.state = st
	}
}

func (h *Hub) sessionLocked(userID string) *session {
	s, ok := h.sessions[userID]
	if !ok {
		s = &session{userID: userID, conns: map[*Conn]struct{}{}}
		h.sessions[userID] = s
	}
	return s
}

// Close removes the device. Safe to call more than once.
func (c *Conn) Close() {
	h := c.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.sessions[c.userID]
	if !ok {
		return
	}
	if _, ok := s.conns[c]; !ok {
		return
	}
	h.dropLocked(s, c)
	h.broadcastLocked(s)
}

// dropLocked removes c from s. If c was active the session keeps its state,
// paused at the extrapolated position, with no active device.
func (h *Hub) dropLocked(s *session, c *Conn) {
	delete(s.conns, c)
	if s.active == c {
		s.active = nil
		if s.state != nil {
			s.state = h.settledLocked(s.state)
			s.state.Playing = false
			s.dirty = true
		}
		h.saveLocked(s)
	}
	if !c.closed {
		c.closed = true
		close(c.done)
	}
}

// settledLocked returns a copy of st with the position moved on by the time
// since it was reported, if it was playing.
func (h *Hub) settledLocked(st *State) *State {
	cp := *st
	now := h.now().UnixMilli()
	if cp.Playing && cp.UpdatedAt > 0 && now > cp.UpdatedAt {
		cp.PositionMs += now - cp.UpdatedAt
		if cp.Index >= 0 && cp.Index < len(cp.Queue) {
			if d := int64(cp.Queue[cp.Index].DurationMs); d > 0 && cp.PositionMs > d {
				cp.PositionMs = d
			}
		}
	}
	cp.UpdatedAt = now
	return &cp
}

// Handle processes one frame from the device. payload is the frame's raw JSON
// payload. Unknown or invalid frames are ignored.
func (c *Conn) Handle(msgType string, payload json.RawMessage) {
	switch msgType {
	case MsgState, MsgClaim:
		var st State
		if json.Unmarshal(payload, &st) != nil || !sanitizeState(&st) {
			return
		}
		c.hub.report(c, &st, msgType == MsgClaim)
	case MsgCommand:
		var cmd Command
		if json.Unmarshal(payload, &cmd) != nil || !sanitizeCommand(&cmd) {
			return
		}
		c.hub.command(c, &cmd)
	case MsgTransfer:
		var t Transfer
		if json.Unmarshal(payload, &t) != nil {
			return
		}
		c.hub.transfer(c, t)
	}
}

// State reports state from an in-process device (same as a MsgState frame).
func (c *Conn) State(st State) {
	if sanitizeState(&st) {
		c.hub.report(c, &st, false)
	}
}

// Claim makes an in-process device active with st (same as a MsgClaim frame).
func (c *Conn) Claim(st State) {
	if sanitizeState(&st) {
		c.hub.report(c, &st, true)
	}
}

// Command sends a remote control action from an in-process device.
func (c *Conn) Command(cmd Command) {
	if sanitizeCommand(&cmd) {
		c.hub.command(c, &cmd)
	}
}

// Transfer moves playback from an in-process device.
func (c *Conn) Transfer(t Transfer) { c.hub.transfer(c, t) }

func (h *Hub) report(c *Conn, st *State, claim bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.sessions[c.userID]
	if !ok || c.closed {
		return
	}
	if s.active != c {
		if !claim {
			return // a stale report from a device that is no longer active
		}
		if old := s.active; old != nil {
			h.sendLocked(s, old, Message{Type: MsgDeactivate, Payload: struct{}{}})
		}
		s.active = c
	}
	st.UpdatedAt = h.now().UnixMilli()
	s.state = st
	s.dirty = true
	h.broadcastLocked(s)
}

func (h *Hub) command(c *Conn, cmd *Command) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.sessions[c.userID]
	if !ok || c.closed || s.active == nil || s.active == c {
		return
	}
	h.sendLocked(s, s.active, Message{Type: MsgCommand, Payload: cmd})
}

func (h *Hub) transfer(c *Conn, t Transfer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.sessions[c.userID]
	if !ok || c.closed {
		return
	}
	var target *Conn
	for cc := range s.conns {
		if cc.Device.ID == t.DeviceID {
			target = cc
		}
	}
	if target == nil {
		return
	}
	var st *State
	if s.state != nil {
		st = h.settledLocked(s.state)
	}
	if old := s.active; old != nil && old != target {
		h.sendLocked(s, old, Message{Type: MsgDeactivate, Payload: struct{}{}})
	}
	s.active = target
	if st != nil {
		st.Playing = t.Play && len(st.Queue) > 0
		s.state = st
		s.dirty = true
	}
	h.sendLocked(s, target, Message{Type: MsgActivate, Payload: Activate{State: st, Play: t.Play}})
	h.broadcastLocked(s)
}

// sendLocked queues a frame for c. A device too far behind is dropped rather
// than allowed to stall everyone else.
func (h *Hub) sendLocked(s *session, c *Conn, m Message) {
	if c.closed {
		return
	}
	select {
	case c.out <- m:
	default:
		h.log.Warn("playback: device fell behind; dropping", "device", c.Device.ID)
		h.dropLocked(s, c)
	}
}

func (h *Hub) sessionViewLocked(s *session) Session {
	view := Session{Devices: []DeviceInfo{}, ServerTime: h.now().UnixMilli()}
	for c := range s.conns {
		view.Devices = append(view.Devices, c.Device)
	}
	sort.Slice(view.Devices, func(i, j int) bool {
		if view.Devices[i].Name != view.Devices[j].Name {
			return view.Devices[i].Name < view.Devices[j].Name
		}
		return view.Devices[i].ID < view.Devices[j].ID
	})
	if s.active != nil {
		view.ActiveDeviceID = s.active.Device.ID
	}
	if s.state != nil {
		cp := *s.state
		view.State = &cp
	}
	return view
}

func (h *Hub) broadcastLocked(s *session) {
	view := h.sessionViewLocked(s)
	// Copy the set: sendLocked may drop a device from it.
	conns := make([]*Conn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	for _, c := range conns {
		h.sendLocked(s, c, Message{Type: MsgSession, Payload: view})
	}
}

// Snapshot returns the session of userID as devices see it.
func (h *Hub) Snapshot(userID string) Session {
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.sessions[userID]
	if !ok {
		return Session{Devices: []DeviceInfo{}, ServerTime: h.now().UnixMilli()}
	}
	return h.sessionViewLocked(s)
}

// saveLocked writes s's state if it changed. The write runs in the
// background so a slow disk never holds the hub lock.
func (h *Hub) saveLocked(s *session) {
	if h.stopped {
		return
	}
	if h.store == nil || !s.dirty || s.state == nil {
		s.dirty = false
		return
	}
	st := h.settledLocked(s.state)
	s.dirty = false
	userID := s.userID
	h.saveSeq++
	seq := h.saveSeq
	h.saves.Add(1)
	go func() {
		defer h.saves.Done()
		h.saveMu.Lock()
		defer h.saveMu.Unlock()
		if h.saved[userID] > seq {
			return // a newer state is already written
		}
		h.saved[userID] = seq
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := h.store.SaveState(ctx, userID, st); err != nil {
			h.log.Warn("playback: save state", "err", err)
		}
	}()
}

// Flush starts saving every changed state. Sessions stay in memory (they are
// small, one per account) so a reconnect never reads a state still being
// written.
func (h *Hub) Flush() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, s := range h.sessions {
		h.saveLocked(s)
	}
}

// Wait blocks until saves already started have finished.
func (h *Hub) Wait() { h.saves.Wait() }

// Shutdown writes every changed state, waits for the writes, and turns saving
// off. Safe to call more than once and from several goroutines.
func (h *Hub) Shutdown() {
	h.shutdownOnce.Do(func() {
		h.mu.Lock()
		for _, s := range h.sessions {
			h.saveLocked(s)
		}
		h.stopped = true
		h.mu.Unlock()
		h.saves.Wait()
	})
}

// Run saves changed states every interval until ctx ends, then once more.
func (h *Hub) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			h.Shutdown()
			return
		case <-t.C:
			h.Flush()
		}
	}
}
