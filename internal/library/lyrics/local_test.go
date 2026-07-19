package lyrics

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestReadLocal_SidecarWins(t *testing.T) {
	dir := t.TempDir()
	audio := filepath.Join(dir, "song.flac")
	if err := os.WriteFile(audio, []byte("not-audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "song.lrc"), []byte("[00:01.00]Hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, source, ok := ReadLocal(context.Background(), "", audio)
	if !ok || source != "sidecar" || raw != "[00:01.00]Hi" {
		t.Fatalf("got raw=%q source=%q ok=%v", raw, source, ok)
	}
}

func TestReadLocal_NoSidecarNoFFprobe(t *testing.T) {
	dir := t.TempDir()
	audio := filepath.Join(dir, "song.mp3")
	if err := os.WriteFile(audio, []byte("not-audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Point ffprobe at a binary that does not exist: tag step must be skipped.
	if _, _, ok := ReadLocal(context.Background(), filepath.Join(dir, "no-such-ffprobe"), audio); ok {
		t.Fatal("want ok=false with no sidecar and no ffprobe")
	}
}

// extractLyricsTag is the pure part of the ffprobe path: pull a lyrics value
// out of ffprobe's -show_format JSON.
func TestExtractLyricsTag(t *testing.T) {
	cases := []struct {
		name, json, want string
	}{
		{"vorbis LYRICS", `{"format":{"tags":{"LYRICS":"[00:01.00]La"}}}`, "[00:01.00]La"},
		{"id3 lyrics-eng", `{"format":{"tags":{"lyrics-eng":"Plain text"}}}`, "Plain text"},
		{"unsyncedlyrics", `{"format":{"tags":{"UNSYNCEDLYRICS":"Words"}}}`, "Words"},
		{"no tags", `{"format":{"tags":{"title":"x"}}}`, ""},
		{"garbage", `nope`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := extractLyricsTag([]byte(c.json)); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}
