package lyrics

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// fakeStore is an in-memory Store.
type fakeStore struct {
	rows    map[string]db.Lyric
	upserts int
}

func newFakeStore() *fakeStore { return &fakeStore{rows: map[string]db.Lyric{}} }

func (f *fakeStore) GetLyrics(_ context.Context, key string) (db.Lyric, error) {
	r, ok := f.rows[key]
	if !ok {
		return db.Lyric{}, sql.ErrNoRows
	}
	return r, nil
}

func (f *fakeStore) UpsertLyrics(_ context.Context, arg db.UpsertLyricsParams) error {
	f.upserts++
	f.rows[arg.TrackKey] = db.Lyric{
		TrackKey: arg.TrackKey, Synced: arg.Synced, Body: arg.Body,
		Source: arg.Source, FetchedAt: arg.FetchedAt,
	}
	return nil
}

type fakeFetcher struct {
	raw   string
	found bool
	err   error
	calls int
}

func (f *fakeFetcher) Fetch(context.Context, Query) (string, bool, error) {
	f.calls++
	return f.raw, f.found, f.err
}

func TestService_SidecarBeatsLRCLibAndCaches(t *testing.T) {
	dir := t.TempDir()
	audio := filepath.Join(dir, "a.flac")
	os.WriteFile(audio, []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "a.lrc"), []byte("[00:01.00]Local"), 0o644)
	st, fetcher := newFakeStore(), &fakeFetcher{raw: "[00:01.00]Remote", found: true}
	svc := &Service{Store: st, Client: fetcher}

	got, ok, err := svc.Get(context.Background(), Request{TrackID: "t1", LocalPath: audio})
	if err != nil || !ok || !got.Synced || got.Lines[0].Text != "Local" {
		t.Fatalf("got=%+v ok=%v err=%v", got, ok, err)
	}
	if fetcher.calls != 0 {
		t.Fatal("LRCLIB must not be called when a sidecar exists")
	}
	if st.rows["t1"].Source != "sidecar" {
		t.Fatalf("cached source = %q", st.rows["t1"].Source)
	}
}

func TestService_CacheHitSkipsEverything(t *testing.T) {
	st, fetcher := newFakeStore(), &fakeFetcher{raw: "x", found: true}
	st.rows["t1"] = db.Lyric{TrackKey: "t1", Synced: 1, Body: `[{"timeMs":1000,"text":"Cached"}]`, Source: "lrclib"}
	svc := &Service{Store: st, Client: fetcher}

	got, ok, err := svc.Get(context.Background(), Request{TrackID: "t1"})
	if err != nil || !ok || got.Lines[0].Text != "Cached" {
		t.Fatalf("got=%+v ok=%v err=%v", got, ok, err)
	}
	if fetcher.calls != 0 || st.upserts != 0 {
		t.Fatal("cache hit must not fetch or write")
	}
}

func TestService_LRCLibMissWritesNegativeCache(t *testing.T) {
	st, fetcher := newFakeStore(), &fakeFetcher{found: false}
	svc := &Service{Store: st, Client: fetcher}

	if _, ok, err := svc.Get(context.Background(), Request{TrackID: "t1"}); ok || err != nil {
		t.Fatalf("want miss, got ok=%v err=%v", ok, err)
	}
	if st.rows["t1"].Source != "none" {
		t.Fatalf("miss must negative-cache, got %+v", st.rows["t1"])
	}
	// Second call: served from the negative cache, no refetch.
	if _, ok, _ := svc.Get(context.Background(), Request{TrackID: "t1"}); ok {
		t.Fatal("negative cache must serve a miss")
	}
	if fetcher.calls != 1 {
		t.Fatalf("fetch calls = %d, want 1", fetcher.calls)
	}
}

func TestService_TransientErrorDoesNotNegativeCache(t *testing.T) {
	st, fetcher := newFakeStore(), &fakeFetcher{err: errors.New("boom")}
	svc := &Service{Store: st, Client: fetcher}

	if _, ok, err := svc.Get(context.Background(), Request{TrackID: "t1"}); ok || err != nil {
		t.Fatalf("transient failure must degrade to clean miss: ok=%v err=%v", ok, err)
	}
	if st.upserts != 0 {
		t.Fatal("transient failure must not write a cache row")
	}
}

func TestService_PlainLyricsRoundTrip(t *testing.T) {
	st := newFakeStore()
	svc := &Service{Store: st, Client: &fakeFetcher{raw: "Just words\nno stamps", found: true}}

	got, ok, err := svc.Get(context.Background(), Request{TrackID: "t1"})
	if err != nil || !ok || got.Synced || got.Plain != "Just words\nno stamps" {
		t.Fatalf("got=%+v ok=%v err=%v", got, ok, err)
	}
	// And it reads back identically from the cache.
	got2, ok2, _ := svc.Get(context.Background(), Request{TrackID: "t1"})
	if !ok2 || got2.Plain != got.Plain {
		t.Fatalf("cache round-trip mismatch: %+v", got2)
	}
}

func TestService_NilClientNoPathIsMiss(t *testing.T) {
	svc := &Service{Store: newFakeStore()}
	if _, ok, err := svc.Get(context.Background(), Request{TrackID: "t1"}); ok || err != nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}
