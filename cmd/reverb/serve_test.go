package main

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestServeWithShutdown_RunsHookOnStop(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.NewServeMux()}
	stop := make(chan struct{})
	hookRan := make(chan struct{})

	go func() {
		_ = serveWithShutdown(srv, ln, stop, func(context.Context) error {
			close(hookRan)
			return nil
		})
	}()

	time.Sleep(20 * time.Millisecond)
	close(stop)

	select {
	case <-hookRan:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown hook did not run after stop")
	}
}

func TestNewHTTPServer_UsesConnectionLimits(t *testing.T) {
	handler := http.NewServeMux()
	srv := newHTTPServer(handler)

	if srv.Handler != handler {
		t.Fatal("handler was not configured")
	}
	if srv.ReadHeaderTimeout != 10*time.Second {
		t.Fatalf("ReadHeaderTimeout = %v, want 10s", srv.ReadHeaderTimeout)
	}
	if srv.ReadTimeout != 30*time.Second {
		t.Fatalf("ReadTimeout = %v, want 30s", srv.ReadTimeout)
	}
	if srv.IdleTimeout != 2*time.Minute {
		t.Fatalf("IdleTimeout = %v, want 2m", srv.IdleTimeout)
	}
	if srv.WriteTimeout != 0 {
		t.Fatalf("WriteTimeout = %v, want 0 for SSE/WebSocket support", srv.WriteTimeout)
	}
}
