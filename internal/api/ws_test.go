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
	"github.com/maximusjb/crate/internal/auth"
	"github.com/maximusjb/crate/internal/core"
	"github.com/maximusjb/crate/internal/download"
	"github.com/maximusjb/crate/internal/events"
	"github.com/maximusjb/crate/internal/registry"
	"github.com/maximusjb/crate/internal/store"
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
	authSvc := auth.NewService(st.Q(), time.Now)
	if err := authSvc.SetAdminPassword(context.Background(), "pw"); err != nil {
		t.Fatal(err)
	}
	tok, _ := authSvc.CreateSession(context.Background())
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

	// Give the handler a moment to subscribe, then publish a progress event.
	time.Sleep(50 * time.Millisecond)
	bus.Publish(events.Event{Topic: download.TopicProgress, Payload: core.DownloadEvent{
		JobID: "j1", Status: core.DownloadRunning, Progress: 42, Source: "spotify", ExternalID: "sp1",
	}})

	var frame wsFrame
	if err := wsjson.Read(ctx, c, &frame); err != nil {
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
