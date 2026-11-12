package playback

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/core"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

type memStore struct {
	mu    sync.Mutex
	saved map[string]*State
}

func (m *memStore) LoadState(_ context.Context, id string) (*State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.saved[id]; ok {
		cp := *s
		return &cp, nil
	}
	return nil, nil
}

func (m *memStore) SaveState(_ context.Context, id string, s *State) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saved[id] = s
	return nil
}

func tr(id string, dur int) Track {
	return Track{Track: core.Track{ID: id, Title: id, DurationMs: dur}}
}

func newTestHub() (*Hub, *clock, *memStore) {
	c := &clock{t: time.UnixMilli(1_000_000)}
	st := &memStore{saved: map[string]*State{}}
	return NewHub(st, c.now, nil), c, st
}

func connect(t *testing.T, h *Hub, user, id string) *Conn {
	t.Helper()
	c, ok := h.Connect(context.Background(), user, DeviceInfo{ID: id, Name: id, Kind: "web"})
	if !ok {
		t.Fatalf("connect %s refused", id)
	}
	return c
}

// drain returns the frames waiting for c.
func drain(c *Conn) []Message {
	var out []Message
	for {
		select {
		case m := <-c.Out():
			out = append(out, m)
		default:
			return out
		}
	}
}

func last(t *testing.T, msgs []Message, typ string) any {
	t.Helper()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Type == typ {
			return msgs[i].Payload
		}
	}
	t.Fatalf("no %s frame in %v", typ, msgs)
	return nil
}

func has(msgs []Message, typ string) bool {
	for _, m := range msgs {
		if m.Type == typ {
			return true
		}
	}
	return false
}

func raw(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func playingState() State {
	return State{Queue: []Track{tr("a", 200_000), tr("b", 180_000)}, Index: 0, PositionMs: 10_000, Playing: true, Repeat: "off", Volume: 0.8}
}

func TestConnectBroadcastsDevicesAndNoneActive(t *testing.T) {
	h, _, _ := newTestHub()
	a := connect(t, h, "u1", "A")
	b := connect(t, h, "u1", "B")
	sess := last(t, drain(a), MsgSession).(Session)
	if len(sess.Devices) != 2 || sess.ActiveDeviceID != "" {
		t.Fatalf("session = %+v", sess)
	}
	if !has(drain(b), MsgSession) {
		t.Fatal("B got no session")
	}
	other := connect(t, h, "u2", "C")
	if s := last(t, drain(other), MsgSession).(Session); len(s.Devices) != 1 {
		t.Fatalf("accounts leaked into each other: %+v", s)
	}
	if has(drain(a), MsgSession) {
		t.Fatal("u1 heard about u2's device")
	}
}

func TestClaimMakesActiveAndBroadcastsState(t *testing.T) {
	h, _, _ := newTestHub()
	a := connect(t, h, "u", "A")
	b := connect(t, h, "u", "B")
	drain(a)
	drain(b)
	a.Handle(MsgClaim, raw(playingState()))
	sess := last(t, drain(b), MsgSession).(Session)
	if sess.ActiveDeviceID != "A" || sess.State == nil || sess.State.Queue[0].ID != "a" || sess.State.UpdatedAt != 1_000_000 {
		t.Fatalf("session = %+v", sess)
	}
}

func TestStateFromInactiveDeviceIsIgnored(t *testing.T) {
	h, _, _ := newTestHub()
	a := connect(t, h, "u", "A")
	b := connect(t, h, "u", "B")
	a.Handle(MsgClaim, raw(playingState()))
	drain(a)
	drain(b)
	st := playingState()
	st.Index = 1
	b.Handle(MsgState, raw(st))
	if has(drain(a), MsgSession) {
		t.Fatal("a stale report from B changed the session")
	}
	if got := h.Snapshot("u").State.Index; got != 0 {
		t.Fatalf("index = %d", got)
	}
}

func TestSecondClaimDeactivatesFirst(t *testing.T) {
	h, _, _ := newTestHub()
	a := connect(t, h, "u", "A")
	b := connect(t, h, "u", "B")
	a.Handle(MsgClaim, raw(playingState()))
	drain(a)
	b.Handle(MsgClaim, raw(playingState()))
	msgs := drain(a)
	if !has(msgs, MsgDeactivate) {
		t.Fatalf("A not told to stop: %v", msgs)
	}
	if s := last(t, msgs, MsgSession).(Session); s.ActiveDeviceID != "B" {
		t.Fatalf("active = %q", s.ActiveDeviceID)
	}
}

func TestCommandRelayedToActiveDeviceOnly(t *testing.T) {
	h, _, _ := newTestHub()
	a := connect(t, h, "u", "A")
	b := connect(t, h, "u", "B")
	c := connect(t, h, "u", "C")
	a.Handle(MsgClaim, raw(playingState()))
	drain(a)
	drain(c)
	b.Handle(MsgCommand, raw(Command{Action: "next"}))
	cmd := last(t, drain(a), MsgCommand).(*Command)
	if cmd.Action != "next" {
		t.Fatalf("cmd = %+v", cmd)
	}
	if has(drain(c), MsgCommand) {
		t.Fatal("a bystander got the command")
	}
	// The active device's own commands and unknown actions are dropped.
	a.Handle(MsgCommand, raw(Command{Action: "next"}))
	b.Handle(MsgCommand, raw(Command{Action: "rm -rf"}))
	b.Handle(MsgCommand, raw(Command{Action: "playTracks", Index: 3, Tracks: []Track{tr("x", 1)}}))
	b.Handle(MsgCommand, raw(Command{Action: "setVolume", Volume: 4}))
	if has(drain(a), MsgCommand) {
		t.Fatal("an invalid command was relayed")
	}
}

func TestCommandWithNoActiveDeviceGoesNowhere(t *testing.T) {
	h, _, _ := newTestHub()
	a := connect(t, h, "u", "A")
	b := connect(t, h, "u", "B")
	drain(a)
	b.Handle(MsgCommand, raw(Command{Action: "play"}))
	if has(drain(a), MsgCommand) {
		t.Fatal("command delivered with nobody active")
	}
}

func TestTransferHandsOverExtrapolatedState(t *testing.T) {
	h, clk, _ := newTestHub()
	a := connect(t, h, "u", "A")
	b := connect(t, h, "u", "B")
	a.Handle(MsgClaim, raw(playingState()))
	drain(a)
	drain(b)
	clk.t = clk.t.Add(5 * time.Second)
	a.Handle(MsgTransfer, raw(Transfer{DeviceID: "B", Play: true}))
	if !has(drain(a), MsgDeactivate) {
		t.Fatal("old device kept playing")
	}
	msgs := drain(b)
	act := last(t, msgs, MsgActivate).(Activate)
	if !act.Play || act.State == nil || act.State.PositionMs != 15_000 {
		t.Fatalf("activate = %+v state=%+v", act, act.State)
	}
	if s := last(t, msgs, MsgSession).(Session); s.ActiveDeviceID != "B" {
		t.Fatalf("active = %q", s.ActiveDeviceID)
	}
}

func TestTransferToUnknownDeviceIgnored(t *testing.T) {
	h, _, _ := newTestHub()
	a := connect(t, h, "u", "A")
	connect(t, h, "u2", "X")
	a.Handle(MsgClaim, raw(playingState()))
	drain(a)
	a.Handle(MsgTransfer, raw(Transfer{DeviceID: "X", Play: true}))
	if has(drain(a), MsgDeactivate) {
		t.Fatal("transferred to another account's device")
	}
}

func TestActiveDisconnectPausesAndSaves(t *testing.T) {
	h, clk, st := newTestHub()
	a := connect(t, h, "u", "A")
	b := connect(t, h, "u", "B")
	a.Handle(MsgClaim, raw(playingState()))
	drain(b)
	clk.t = clk.t.Add(3 * time.Second)
	a.Close()
	a.Close() // idempotent
	select {
	case <-a.Done():
	default:
		t.Fatal("Done not closed")
	}
	sess := last(t, drain(b), MsgSession).(Session)
	if sess.ActiveDeviceID != "" || sess.State == nil || sess.State.Playing || sess.State.PositionMs != 13_000 {
		t.Fatalf("session = %+v state=%+v", sess, sess.State)
	}
	h.Wait()
	st.mu.Lock()
	saved := st.saved["u"]
	st.mu.Unlock()
	if saved == nil || saved.PositionMs != 13_000 || saved.Playing {
		t.Fatalf("saved = %+v", saved)
	}
}

func TestStateSurvivesRestartPaused(t *testing.T) {
	_, _, st := newTestHub()
	s := playingState()
	st.saved["u"] = &s
	h := NewHub(st, nil, nil)
	a := connect(t, h, "u", "A")
	sess := last(t, drain(a), MsgSession).(Session)
	if sess.State == nil || sess.State.Playing || len(sess.State.Queue) != 2 {
		t.Fatalf("state = %+v", sess.State)
	}
}

func TestReconnectWithSameIDReplacesOldConn(t *testing.T) {
	h, _, _ := newTestHub()
	a1 := connect(t, h, "u", "A")
	a1.Handle(MsgClaim, raw(playingState()))
	a2 := connect(t, h, "u", "A")
	select {
	case <-a1.Done():
	default:
		t.Fatal("old connection still registered")
	}
	sess := last(t, drain(a2), MsgSession).(Session)
	if len(sess.Devices) != 1 || sess.ActiveDeviceID != "" || sess.State.Playing {
		t.Fatalf("session = %+v", sess)
	}
	a1.Close() // late close of the replaced socket must not drop the new one
	if s := h.Snapshot("u"); len(s.Devices) != 1 {
		t.Fatalf("devices = %+v", s.Devices)
	}
}

func TestSlowDeviceIsDropped(t *testing.T) {
	h, _, _ := newTestHub()
	slow := connect(t, h, "u", "S")
	a := connect(t, h, "u", "A")
	for i := 0; i < outBuffer+5; i++ {
		st := playingState()
		a.Handle(MsgClaim, raw(st))
		drain(a)
	}
	select {
	case <-slow.Done():
	default:
		t.Fatal("slow device was not dropped")
	}
}

func TestInvalidStateRejected(t *testing.T) {
	h, _, _ := newTestHub()
	a := connect(t, h, "u", "A")
	bad := []State{
		{Queue: []Track{tr("a", 1)}, Index: 5, Repeat: "off"},
		{Queue: []Track{tr("", 1)}, Index: 0, Repeat: "off"},
		{Queue: []Track{tr("a", 1)}, Index: 0, Repeat: "sometimes"},
	}
	for _, s := range bad {
		a.Handle(MsgClaim, raw(s))
	}
	a.Handle(MsgClaim, []byte(`{not json`))
	if s := h.Snapshot("u"); s.ActiveDeviceID != "" {
		t.Fatalf("invalid claim accepted: %+v", s)
	}
}

func TestConnectRejectsBadDevice(t *testing.T) {
	h, _, _ := newTestHub()
	if _, ok := h.Connect(context.Background(), "u", DeviceInfo{ID: ""}); ok {
		t.Fatal("empty id accepted")
	}
	if _, ok := h.Connect(context.Background(), "", DeviceInfo{ID: "x"}); ok {
		t.Fatal("empty user accepted")
	}
	c, _ := h.Connect(context.Background(), "u", DeviceInfo{ID: "x", Kind: "toaster", Name: ""})
	if c.Device.Kind != "web" || c.Device.Name == "" {
		t.Fatalf("device = %+v", c.Device)
	}
}
