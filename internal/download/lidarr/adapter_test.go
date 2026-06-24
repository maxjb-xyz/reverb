package lidarr

import (
	"context"
	"strings"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/download"
)

// compile-time: the adapter is a Downloader AND an AsyncDownloader.
var _ download.Downloader = (*Adapter)(nil)
var _ download.AsyncDownloader = (*Adapter)(nil)

func newTestAdapter(t *testing.T, doer *fakeDoer) *Adapter {
	t.Helper()
	a := New()
	if err := a.Init(map[string]any{
		"url": "http://lidarr:8686", "api_key": "k", "root_folder": "/music",
		"quality_profile_id": float64(1), "metadata_profile_id": float64(1),
	}); err != nil {
		t.Fatal(err)
	}
	a.client = NewClientFor(a, doer) // inject the fake Doer (test seam)
	return a
}

func TestCanDownloadIsFalseOptInOnly(t *testing.T) {
	a := New()
	ok, _ := a.CanDownload(context.Background(), core.DownloadRequest{Artist: "A", Title: "T", Album: "Al"})
	if ok {
		t.Fatal("Lidarr CanDownload must be false (opt-in only)")
	}
}

func TestSubmitResolvesAddsMonitorsSearches(t *testing.T) {
	doer := &fakeDoer{routes: map[string]string{
		"GET /api/v1/album/lookup": `[{"title":"Discovery","foreignAlbumId":"mb-al","artist":{"artistName":"Daft Punk","foreignArtistId":"mb-ar"}}]`,
		"GET /api/v1/artist":       `[]`,
		"POST /api/v1/artist":      `{"id":7,"artistName":"Daft Punk","foreignArtistId":"mb-ar"}`,
		"GET /api/v1/album":        `[{"id":42,"title":"Discovery","foreignAlbumId":"mb-al"}]`,
		"PUT /api/v1/album/monitor": `{}`,
		"POST /api/v1/command":     `{"id":1}`,
	}}
	a := newTestAdapter(t, doer)
	ref, err := a.Submit(context.Background(), core.DownloadRequest{Artist: "Daft Punk", Album: "Discovery", Title: "One More Time"})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if ref != "42" {
		t.Fatalf("ref = %q, want 42 (the Lidarr album id)", ref)
	}
	// Artist added UNMONITORED (no discography grab).
	body := doer.lastBodies["POST /api/v1/artist"]
	if !strings.Contains(body, `"monitored":false`) || !strings.Contains(body, `"monitor":"none"`) {
		t.Fatalf("artist must be added unmonitored, body = %s", body)
	}
	// Only the target album searched.
	if !strings.Contains(doer.lastBodies["POST /api/v1/command"], "AlbumSearch") {
		t.Fatal("expected AlbumSearch command")
	}
}

func TestSubmitNoMatchFails(t *testing.T) {
	doer := &fakeDoer{routes: map[string]string{"GET /api/v1/album/lookup": `[]`}}
	a := newTestAdapter(t, doer)
	_, err := a.Submit(context.Background(), core.DownloadRequest{Artist: "Nobody", Album: "Nothing", Title: "X"})
	if err == nil {
		t.Fatal("Submit with no lookup match must error")
	}
}

func TestPollMapsImportedToCompleted(t *testing.T) {
	doer := &fakeDoer{routes: map[string]string{
		"GET /api/v1/album/42": `{"id":42,"statistics":{"trackCount":12,"trackFileCount":12}}`,
		"GET /api/v1/queue":    `{"records":[]}`,
	}}
	a := newTestAdapter(t, doer)
	st, err := a.Poll(context.Background(), "42")
	if err != nil {
		t.Fatal(err)
	}
	if st.State != core.DownloadCompleted {
		t.Fatalf("state = %s, want completed", st.State)
	}
}

func TestPollMapsDownloadingToRunningProgress(t *testing.T) {
	doer := &fakeDoer{routes: map[string]string{
		"GET /api/v1/album/42": `{"id":42,"statistics":{"trackCount":12,"trackFileCount":0}}`,
		"GET /api/v1/queue":    `{"records":[{"albumId":42,"size":100,"sizeleft":40,"status":"downloading","trackedDownloadStatus":"ok"}]}`,
	}}
	a := newTestAdapter(t, doer)
	st, _ := a.Poll(context.Background(), "42")
	if st.State != core.DownloadRunning || st.Progress != 60 {
		t.Fatalf("state/progress = %s/%d, want running/60", st.State, st.Progress)
	}
}
