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
	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/download"
	"github.com/maxjb-xyz/reverb/internal/events"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/store"
)

func wsTestServer(t *testing.T) (*httptest.Server, *events.Bus, string) {
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
	bus := events.New()
	srv := NewServer(Deps{
		Auth:       authSvc,
		Events:     bus,
		Search:     registry.NewRegistry("search"),
		Downloader: registry.NewRegistry("downloader"),
	})
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)
	return hs, bus, tok
}

// wsFrame mirrors the {type, payload} envelope the handler writes.
type wsFrame struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func TestWSStreamsPublishedEvents(t *testing.T) {
	hs, bus, tok := wsTestServer(t)
	wsURL := "ws" + hs.URL[len("http"):] + "/api/v1/ws"

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	c, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Cookie": {sessionCookie + "=" + tok}},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	progress := events.Event{Topic: download.TopicProgress, Payload: core.DownloadEvent{
		JobID: "j1", Status: core.DownloadRunning, Progress: 42, Source: "spotify", ExternalID: "sp1",
	}}

	// The handler subscribes asynchronously after the handshake, so an early
	// publish (before the subscription is live) would be missed. Re-publish on a
	// ticker from a goroutine and do ONE read on the full context. We must NOT use
	// a short per-read deadline: under coder/websocket a deadline-cancelled read
	// closes the connection, so the first timeout would kill the socket and every
	// retry would then fail with "use of closed network connection".
	stopPublish := make(chan struct{})
	go func() {
		tk := time.NewTicker(20 * time.Millisecond)
		defer tk.Stop()
		for {
			select {
			case <-stopPublish:
				return
			case <-tk.C:
				bus.Publish(progress)
			}
		}
	}()
	bus.Publish(progress)

	var frame wsFrame
	err = wsjson.Read(ctx, c, &frame)
	close(stopPublish)
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	if frame.Type != download.TopicProgress {
		t.Fatalf("frame type = %q, want %q", frame.Type, download.TopicProgress)
	}
	var ev core.DownloadEvent
	if err := json.Unmarshal(frame.Payload, &ev); err != nil {
		t.Fatal(err)
	}
	if ev.JobID != "j1" || ev.Progress != 42 {
		t.Fatalf("payload = %+v", ev)
	}
}

func TestWSRequiresAuth(t *testing.T) {
	hs, _, _ := wsTestServer(t)
	wsURL := "ws" + hs.URL[len("http"):] + "/api/v1/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// No cookie → handshake should be rejected (401 before upgrade).
	_, _, err := websocket.Dial(ctx, wsURL, nil)
	if err == nil {
		t.Fatal("expected dial to fail without auth")
	}
}
