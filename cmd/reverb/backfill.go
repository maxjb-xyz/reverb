package main

import (
	"context"
	"log"
	"time"
)

const (
	backfillDefaultInterval = 500 * time.Millisecond
	backfillDefaultTimeout  = 5 * time.Minute
)

// waitReadyThenBackfill polls ready until true (or ctx is done / the default
// 5-minute timeout elapses), then runs backfill once. The bundled library
// reports ready when Navidrome is serving; re-running the backfill then
// re-resolves download jobs whose first (boot-race) backfill attempt ran
// before Navidrome was up.
func waitReadyThenBackfill(ctx context.Context, ready func() bool, backfill func()) {
	waitReadyThenBackfillEvery(ctx, ready, backfill, backfillDefaultInterval, backfillDefaultTimeout)
}

// waitReadyThenBackfillEvery is the parameterised form used in tests. It polls
// ready every interval; gives up after timeout; returns immediately if ctx is
// done. Runs backfill exactly once, only if ready returned true.
func waitReadyThenBackfillEvery(ctx context.Context, ready func() bool, backfill func(), interval, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	t := time.NewTicker(interval)
	defer t.Stop()

	for {
		if ready() {
			log.Printf("download backfill: bundled library ready — running post-ready backfill")
			backfill()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if time.Now().After(deadline) {
				log.Printf("download backfill: timed out waiting for bundled library to become ready — post-ready backfill skipped")
				return
			}
		}
	}
}
