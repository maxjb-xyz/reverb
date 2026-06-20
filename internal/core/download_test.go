package core

import (
	"encoding/json"
	"testing"
)

func TestDownloadJobJSONRoundTrip(t *testing.T) {
	in := DownloadJob{
		ID: "j1", DedupKey: "dk1", Status: DownloadRunning, Progress: 42,
		Error: "", OutputPath: "/music/x.mp3", LibraryTrackID: "t1",
		DownloaderName: "spotdl", Priority: 0, Attempts: 1,
		Source: "spotify", ExternalID: "sp1", PlayWhenReady: true,
		CreatedAt: 100, StartedAt: 110, FinishedAt: 0,
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out DownloadJob
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.ID != "j1" || out.Status != DownloadRunning || out.Progress != 42 || out.LibraryTrackID != "t1" {
		t.Fatalf("round trip mismatch: %+v", out)
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	for _, k := range []string{"id", "dedupKey", "status", "progress", "outputPath", "libraryTrackId", "downloaderName", "externalId", "playWhenReady", "createdAt"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("expected camelCase key %q, got %v", k, m)
		}
	}
}

func TestDownloadStatusConstants(t *testing.T) {
	if DownloadQueued != "queued" || DownloadRunning != "running" || DownloadCompleted != "completed" ||
		DownloadFailed != "failed" || DownloadCanceled != "canceled" {
		t.Fatal("download status constant drift")
	}
}

func TestDownloadEventCamelCase(t *testing.T) {
	b, _ := json.Marshal(DownloadEvent{
		JobID: "j1", DedupKey: "dk", Status: DownloadCompleted, Progress: 100,
		Source: "spotify", ExternalID: "sp1", LibraryTrackID: "t1", ArtistID: "ar1", AlbumID: "al1",
	})
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	for _, k := range []string{"jobId", "dedupKey", "status", "progress", "source", "externalId", "libraryTrackId", "artistId", "albumId"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("DownloadEvent missing camelCase key %q: %v", k, m)
		}
	}
}

func TestLibraryUpdatedEventCamelCase(t *testing.T) {
	b, _ := json.Marshal(LibraryUpdatedEvent{ArtistIDs: []string{"ar1"}, AlbumIDs: []string{"al1"}})
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if _, ok := m["artistIds"]; !ok {
		t.Fatalf("missing artistIds: %v", m)
	}
	if _, ok := m["albumIds"]; !ok {
		t.Fatalf("missing albumIds: %v", m)
	}
}
