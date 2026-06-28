package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestWaitReadyThenBackfill_CallsBackfillOnceWhenReadyBecomesTrue verifies that
// when ready returns false twice then true, backfill is called exactly once.
func TestWaitReadyThenBackfill_CallsBackfillOnceWhenReadyBecomesTrue(t *testing.T) {
	var calls atomic.Int32

	var readyCalls atomic.Int32
	readyFn := func() bool {
		n := readyCalls.Add(1)
		return n >= 3 // false on calls 1 and 2; true on call 3+
	}

	backfillFn := func() { calls.Add(1) }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	waitReadyThenBackfillEvery(ctx, readyFn, backfillFn, 5*time.Millisecond, 2*time.Second)

	if got := calls.Load(); got != 1 {
		t.Fatalf("backfill call count: got %d, want 1", got)
	}
}

// TestWaitReadyThenBackfill_NeverCallsBackfillIfCtxCanceled verifies that
// cancelling ctx before ready is true prevents backfill from ever running.
func TestWaitReadyThenBackfill_NeverCallsBackfillIfCtxCanceled(t *testing.T) {
	var calls atomic.Int32

	readyFn := func() bool { return false } // never ready
	backfillFn := func() { calls.Add(1) }

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		waitReadyThenBackfillEvery(ctx, readyFn, backfillFn, 5*time.Millisecond, 2*time.Second)
	}()

	// Cancel the context after a short delay to let a few poll ticks pass.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("waitReadyThenBackfillEvery did not return after ctx cancel")
	}

	if got := calls.Load(); got != 0 {
		t.Fatalf("backfill call count: got %d, want 0 (ctx was canceled)", got)
	}
}

// TestWaitReadyThenBackfill_NeverCallsBackfillIfTimeout verifies that when
// ready never returns true and the timeout elapses, backfill is not called.
func TestWaitReadyThenBackfill_NeverCallsBackfillIfTimeout(t *testing.T) {
	var calls atomic.Int32

	readyFn := func() bool { return false }
	backfillFn := func() { calls.Add(1) }

	ctx := context.Background()
	waitReadyThenBackfillEvery(ctx, readyFn, backfillFn, 5*time.Millisecond, 30*time.Millisecond)

	if got := calls.Load(); got != 0 {
		t.Fatalf("backfill call count: got %d, want 0 (timed out)", got)
	}
}

// TestWaitReadyThenBackfill_CallsBackfillImmediatelyIfAlreadyReady verifies
// that when ready is true on the very first check, backfill fires without delay.
func TestWaitReadyThenBackfill_CallsBackfillImmediatelyIfAlreadyReady(t *testing.T) {
	var calls atomic.Int32

	readyFn := func() bool { return true }
	backfillFn := func() { calls.Add(1) }

	ctx := context.Background()
	waitReadyThenBackfillEvery(ctx, readyFn, backfillFn, 5*time.Millisecond, 2*time.Second)

	if got := calls.Load(); got != 1 {
		t.Fatalf("backfill call count: got %d, want 1", got)
	}
}
