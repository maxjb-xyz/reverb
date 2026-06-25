package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"
)

// serveWithShutdown serves until `stop` is closed, then gracefully shuts the
// HTTP server down and runs onShutdown (e.g. to SIGTERM the Navidrome child).
func serveWithShutdown(srv *http.Server, ln net.Listener, stop <-chan struct{}, onShutdown func(context.Context) error) error {
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	select {
	case err := <-errCh:
		return err
	case <-stop:
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	if onShutdown != nil {
		_ = onShutdown(ctx)
	}
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
