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
