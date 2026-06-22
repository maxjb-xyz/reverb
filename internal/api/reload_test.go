package api

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/library"
)

// stoppableManager is a fakeManager that records whether Stop was called.
type stoppableManager struct {
	fakeManager
	stopped atomic.Bool
}

func newStoppableManager() *stoppableManager {
	return &stoppableManager{fakeManager: *newFakeManager()}
}
func (m *stoppableManager) Stop() { m.stopped.Store(true) }

// fakeReloader returns a fixed set of services on Reload.
type fakeReloader struct {
	lib   library.LibraryAdapter
	srch  Streamer
	cov   CoverageService
	dl    DownloadManager
	snc   SyncService
	calls atomic.Int32
}

var _ ServiceReloader = (*fakeReloader)(nil)

func (r *fakeReloader) Reload(context.Context) (library.LibraryAdapter, Streamer, CoverageService, DownloadManager, SyncService, error) {
	r.calls.Add(1)
	return r.lib, r.srch, r.cov, r.dl, r.snc, nil
}

func TestReloadSwapsDownloadsAndStopsOld(t *testing.T) {
	oldMgr := newStoppableManager()
	newMgr := newStoppableManager()
	newCov := &fakeCoverage{}
	newSnc := &fakeSync{}

	rl := &fakeReloader{dl: newMgr, cov: newCov, snc: newSnc}
	srv := NewServer(Deps{Downloads: oldMgr, Reload: rl})

	if srv.downloads() != DownloadManager(oldMgr) {
		t.Fatal("expected the old manager before reload")
	}

	if err := srv.reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if rl.calls.Load() != 1 {
		t.Fatalf("expected Reload called once, got %d", rl.calls.Load())
	}
	if srv.downloads() != DownloadManager(newMgr) {
		t.Fatal("expected the new manager to be live after reload")
	}
	if srv.coverage() != CoverageService(newCov) {
		t.Fatal("expected the new coverage service to be live after reload")
	}
	if srv.sync() != SyncService(newSnc) {
		t.Fatal("expected the new sync service to be live after reload")
	}
	if !oldMgr.stopped.Load() {
		t.Fatal("old manager must be Stopped after the swap")
	}
	if newMgr.stopped.Load() {
		t.Fatal("new manager must NOT be Stopped")
	}
}

func TestReloadNilDownloadsDoesNotPanic(t *testing.T) {
	// Old manager present; reload yields no manager → old must be stopped and the
	// live downloads becomes a genuine nil interface (handlers see 503/empty).
	oldMgr := newStoppableManager()
	rl := &fakeReloader{dl: nil}
	srv := NewServer(Deps{Downloads: oldMgr, Reload: rl})

	if err := srv.reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if srv.downloads() != nil {
		t.Fatal("expected nil downloads after reload with no manager")
	}
	if !oldMgr.stopped.Load() {
		t.Fatal("old manager must be Stopped even when the new one is nil")
	}
}

func TestReloadNoReloaderIsNoop(t *testing.T) {
	oldMgr := newStoppableManager()
	srv := NewServer(Deps{Downloads: oldMgr})
	if err := srv.reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if oldMgr.stopped.Load() {
		t.Fatal("no reloader → reload must be a no-op (old manager untouched)")
	}
}
