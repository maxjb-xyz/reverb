package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/maxjb-xyz/reverb/internal/events"
	"github.com/maxjb-xyz/reverb/internal/playback"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/store"
)

func wsPlaybackServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/ws.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	authSvc, tok := seededAuthToken(t, st)
	srv := NewServer(Deps{
		Auth:       authSvc,
		Events:     events.New(),
		Search:     registry.NewRegistry("search"),
		Downloader: registry.NewRegistry("downloader"),
		Playback:   playback.NewHub(nil, nil, nil),
	})
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)
	return hs, tok
}

func dialDevice(t *testing.T, ctx context.Context, hs *httptest.Server, tok, id string) *websocket.Conn {
	t.Helper()
	c, _, err := websocket.Dial(ctx, "ws"+hs.URL[len("http"):]+"/api/v1/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Cookie": {sessionCookie + "=" + tok}},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { c.Close(websocket.StatusNormalClosure, "") })
	c.SetReadLimit(4 << 20) // sessions carry the queue
	if err := wsjson.Write(ctx, c, map[string]any{"type": playback.MsgHello, "payload": playback.DeviceInfo{ID: id, Name: id, Kind: "web"}}); err != nil {
		t.Fatal(err)
	}
	return c
}

// readUntil reads frames until one satisfies ok.
func readUntil(t *testing.T, ctx context.Context, c *websocket.Conn, ok func(wsFrame) bool) wsFrame {
	t.Helper()
	for {
		var f wsFrame
		if err := wsjson.Read(ctx, c, &f); err != nil {
			t.Fatalf("read: %v", err)
		}
		if ok(f) {
			return f
		}
	}
}

func sessionWith(pred func(playback.Session) bool) func(wsFrame) bool {
	return func(f wsFrame) bool {
		if f.Type != playback.MsgSession {
			return false
		}
		var s playback.Session
		return json.Unmarshal(f.Payload, &s) == nil && pred(s)
	}
}

func TestWSPlaybackClaimCommandTransfer(t *testing.T) {
	hs, tok := wsPlaybackServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a := dialDevice(t, ctx, hs, tok, "A")
	readUntil(t, ctx, a, sessionWith(func(s playback.Session) bool { return len(s.Devices) == 1 }))
	b := dialDevice(t, ctx, hs, tok, "B")
	readUntil(t, ctx, b, sessionWith(func(s playback.Session) bool { return len(s.Devices) == 2 }))

	// A starts playing a big queue: the frame is well over the default 32 KiB read limit.
	q := make([]map[string]any, 400)
	for i := range q {
		q[i] = map[string]any{"id": "t" + string(rune('a'+i%26)), "title": "A fairly long song title to pad the frame out", "artist": "Some Artist", "album": "Some Album", "durationMs": 200000}
	}
	state := map[string]any{"queue": q, "index": 3, "positionMs": 1000, "playing": true, "repeat": "off", "volume": 1}
	if err := wsjson.Write(ctx, a, map[string]any{"type": playback.MsgClaim, "payload": state}); err != nil {
		t.Fatal(err)
	}
	readUntil(t, ctx, b, sessionWith(func(s playback.Session) bool {
		return s.ActiveDeviceID == "A" && s.State != nil && len(s.State.Queue) == 400
	}))

	// B's remote control reaches A.
	if err := wsjson.Write(ctx, b, map[string]any{"type": playback.MsgCommand, "payload": map[string]any{"action": "next"}}); err != nil {
		t.Fatal(err)
	}
	f := readUntil(t, ctx, a, func(f wsFrame) bool { return f.Type == playback.MsgCommand })
	var cmd playback.Command
	_ = json.Unmarshal(f.Payload, &cmd)
	if cmd.Action != "next" {
		t.Fatalf("command = %+v", cmd)
	}

	// B takes over.
	if err := wsjson.Write(ctx, b, map[string]any{"type": playback.MsgTransfer, "payload": playback.Transfer{DeviceID: "B", Play: true}}); err != nil {
		t.Fatal(err)
	}
	readUntil(t, ctx, a, func(f wsFrame) bool { return f.Type == playback.MsgDeactivate })
	f = readUntil(t, ctx, b, func(f wsFrame) bool { return f.Type == playback.MsgActivate })
	var act playback.Activate
	_ = json.Unmarshal(f.Payload, &act)
	if !act.Play || act.State == nil || act.State.Index != 3 {
		t.Fatalf("activate = %+v", act)
	}

	// A goes away: B sees one device left.
	a.Close(websocket.StatusNormalClosure, "")
	readUntil(t, ctx, b, sessionWith(func(s playback.Session) bool { return len(s.Devices) == 1 && s.ActiveDeviceID == "B" }))
}

func TestWSWithoutHelloIsNotADevice(t *testing.T) {
	hs, tok := wsPlaybackServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+hs.URL[len("http"):]+"/api/v1/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Cookie": {sessionCookie + "=" + tok}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")
	// A frame before hello is ignored, not fatal.
	_ = wsjson.Write(ctx, c, map[string]any{"type": playback.MsgCommand, "payload": map[string]any{"action": "next"}})
	d := dialDevice(t, ctx, hs, tok, "D")
	readUntil(t, ctx, d, sessionWith(func(s playback.Session) bool { return len(s.Devices) == 1 && s.Devices[0].ID == "D" }))
}
