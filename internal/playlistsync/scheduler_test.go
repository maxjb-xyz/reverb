package playlistsync

import (
	"context"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/core"
)

func TestSchedulerTickSyncsDue(t *testing.T) {
	src := &fakeSource{playlists: map[string]core.ExternalPlaylist{
		"PL": {Source: "spotify", ExternalID: "PL", Name: "P", Tracks: []core.ExternalResult{track("t1")}},
	}}
	store := newMemStore()
	svc := NewService(src, fakeMatcher{}, &fakeDownloader{}, store, func() int64 { return 1000 }, seqID())
	det, _ := svc.Import(context.Background(), "spotify:playlist:PL", false)
	// enable daily sync, last_synced far in the past → due
	_ = svc.UpdateSettings(context.Background(), det.ID, true, 60, false)
	store.setLastSynced(det.ID, 0) // long ago
	src.syncCount = 0
	sch := NewScheduler(svc, time.Minute)
	sch.tick(context.Background())
	if src.syncCount == 0 {
		t.Fatal("scheduler tick should have re-fetched the due playlist")
	}
}
