package subsonic

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// newSongPathAdapter spins up a fake Subsonic server whose getSong response
// carries the given relative path, and returns an adapter configured with
// localMusicDir. Pass "" for localMusicDir to exercise the disabled case.
func newSongPathAdapter(t *testing.T, songPath, localMusicDir string) *Adapter {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"subsonic-response":{"status":"ok","version":"1.16.1","song":{"id":"t1","title":"Song","path":%q}}}`, songPath)
	}))
	t.Cleanup(srv.Close)
	a := New().WithHTTPClient(srv.Client())
	if err := a.Init(map[string]any{"url": srv.URL, "username": "u", "password": "p"}); err != nil {
		t.Fatal(err)
	}
	if localMusicDir != "" {
		a.WithLocalMusicDir(localMusicDir)
	}
	return a
}

func TestLocalTrackPath_ReturnsResolvedPathWhenFileExists(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a", "b.mp3"), []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newSongPathAdapter(t, "a/b.mp3", dir)
	got, ok := a.LocalTrackPath("t1")
	if !ok {
		t.Fatal("expected ok=true")
	}
	want := filepath.Join(dir, "a", "b.mp3")
	if got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}

func TestLocalTrackPath_MissingFileReturnsFalse(t *testing.T) {
	dir := t.TempDir()
	a := newSongPathAdapter(t, "a/missing.mp3", dir)
	if _, ok := a.LocalTrackPath("t1"); ok {
		t.Fatal("expected ok=false for a missing file")
	}
}

func TestLocalTrackPath_EmptyMusicDirReturnsFalse(t *testing.T) {
	a := newSongPathAdapter(t, "a/b.mp3", "")
	if _, ok := a.LocalTrackPath("t1"); ok {
		t.Fatal("expected ok=false when localMusicDir is unset")
	}
}

func TestLocalTrackPath_PathTraversalReturnsFalse(t *testing.T) {
	dir := t.TempDir()
	// Create the file the traversal path would resolve to, OUTSIDE dir, to prove
	// the guard rejects it even when the target file genuinely exists.
	parent := filepath.Dir(dir)
	evil := filepath.Join(parent, "evil.mp3")
	if err := os.WriteFile(evil, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(evil) })

	a := newSongPathAdapter(t, "../evil.mp3", dir)
	if _, ok := a.LocalTrackPath("t1"); ok {
		t.Fatal("expected ok=false for a path-traversal attempt")
	}
}
