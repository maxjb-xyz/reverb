package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/maxjb-xyz/reverb/internal/auth"
	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/download"
	"github.com/maxjb-xyz/reverb/internal/events"
	"github.com/maxjb-xyz/reverb/internal/playback"
	"github.com/maxjb-xyz/reverb/internal/request"
)

// wsTopics are the EventBus topics streamed to WS clients.
var wsTopics = []string{
	download.TopicQueued,
	download.TopicProgress,
	download.TopicComplete,
	download.TopicFailed,
	download.TopicLibraryUpdate,
	download.TopicQueueState,
	download.TopicRemoved,
	request.TopicCreated,
	request.TopicUpdated,
	"notification",
}

// wsShouldForward is a pure filter that decides whether to send an event to a
// specific WebSocket connection. It enforces per-user and per-capability routing
// for request events while leaving all other topics unchanged (always forward).
//
//   - request.updated: forward only to the connection whose cu.ID matches the
//     payload's TargetUserID. A malformed (non-RequestEvent) payload returns false.
//   - request.created: forward only to connections with CapManageRequests.
//   - all other topics: always forward (preserves existing download event behavior).
func wsShouldForward(cu auth.CurrentUser, ev events.Event) bool {
	switch ev.Topic {
	case request.TopicUpdated:
		re, ok := ev.Payload.(core.RequestEvent)
		if !ok {
			return false
		}
		return cu.ID == re.TargetUserID
	case request.TopicCreated:
		return cu.Has(auth.CapManageRequests)
	case "notification":
		ne, ok := ev.Payload.(core.NotificationEvent)
		return ok && cu.ID == ne.TargetUserID
	default:
		return true
	}
}

// wsEnvelope is the JSON frame written to the client: {type, payload}.
type wsEnvelope struct {
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

// handleWS upgrades to a WebSocket, subscribes to the EventBus topics, and writes
// each event as a JSON frame. It is a DISTINCT transport from the SSE search
// stream. Auth is enforced by requireAuth (this route is in the protected group),
// so the handshake only succeeds with a valid session cookie/bearer. It returns
// (unsubscribing + closing) when the client disconnects or ctx is done.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if s.deps.Events == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "events unavailable"})
		return
	}
	cu, _ := currentUser(r)
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Same-origin only by default; the SPA is served from the same host.
		InsecureSkipVerify: s.deps.Dev, // dev: allow the Vite origin
	})
	if err != nil {
		return
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	ctx := r.Context()

	// Fan the per-topic subscriptions into one merged channel.
	merged := make(chan events.Event, 64)
	var unsubs []func()
	for _, topic := range wsTopics {
		ch, unsub := s.deps.Events.Subscribe(topic)
		unsubs = append(unsubs, unsub)
		go func(ch <-chan events.Event) {
			for ev := range ch {
				select {
				case merged <- ev:
				case <-ctx.Done():
					return
				}
			}
		}(ch)
	}
	defer func() {
		for _, u := range unsubs {
			u()
		}
	}()

	// The reader goroutine passes client frames to this loop and cancels
	// readCtx on a read error (the client went away).
	readCtx, cancelRead := context.WithCancel(ctx)
	defer cancelRead()
	c.SetReadLimit(wsReadLimit)
	inbound := make(chan wsInbound, 16)
	go func() {
		for {
			var f wsInbound
			if err := wsjson.Read(readCtx, c, &f); err != nil {
				cancelRead()
				return
			}
			select {
			case inbound <- f:
			case <-readCtx.Done():
				return
			}
		}
	}()

	// The playback device this socket registered, once it says hello. Until
	// then (and on servers without a hub) devOut and devDone stay nil, which
	// blocks those select cases forever.
	var dev *playback.Conn
	var devOut <-chan playback.Message
	var devDone <-chan struct{}
	defer func() {
		if dev != nil {
			dev.Close()
		}
	}()

	write := func(v wsEnvelope) bool {
		// Bound each write so a client that stops reading (but keeps the
		// connection open) can't stall the writer indefinitely. On timeout
		// or error we return, letting the defers unsubscribe + close.
		writeCtx, writeCancel := context.WithTimeout(readCtx, 10*time.Second)
		defer writeCancel()
		return wsjson.Write(writeCtx, c, v) == nil
	}

	for {
		select {
		case <-readCtx.Done():
			return
		case ev := <-merged:
			if !wsShouldForward(cu, ev) {
				continue
			}
			if !write(wsEnvelope{Type: ev.Topic, Payload: ev.Payload}) {
				return
			}
		case f := <-inbound:
			switch {
			case f.Type == playback.MsgHello && dev == nil && s.deps.Playback != nil:
				var info playback.DeviceInfo
				if json.Unmarshal(f.Payload, &info) != nil {
					continue
				}
				if d, ok := s.deps.Playback.Connect(ctx, cu.ID, info); ok {
					dev, devOut, devDone = d, d.Out(), d.Done()
				}
			case dev != nil:
				dev.Handle(f.Type, f.Payload)
			}
		case m := <-devOut:
			if !write(wsEnvelope{Type: m.Type, Payload: m.Payload}) {
				return
			}
		case <-devDone:
			// The hub dropped this device (replaced by a newer connection of
			// the same tab, or too far behind). Close so the client reconnects
			// and says hello again.
			return
		}
	}
}

// wsInbound is a frame from the client: {type, payload}.
type wsInbound struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// wsReadLimit caps one client frame. A playback state carries the queue (up
// to playback.MaxQueue tracks of a few hundred bytes each).
const wsReadLimit = 1 << 20
