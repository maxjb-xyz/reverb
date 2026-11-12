package discord

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/core"
)

type bytesStreamer struct{ b []byte }

func (s bytesStreamer) Stream(context.Context, string, core.StreamOpts, string) (core.StreamHandle, error) {
	return core.StreamHandle{Body: io.NopCloser(bytes.NewReader(s.b)), StatusCode: 200}, nil
}

func countPackets(t *testing.T, src FrameSource) int {
	t.Helper()
	defer src.Close()
	n := 0
	for {
		_, err := src.Next()
		if errors.Is(err, io.EOF) {
			return n
		}
		if err != nil {
			t.Fatal(err)
		}
		n++
	}
}

// A real library file (MP3) through ffmpeg comes out as 20 ms Opus frames,
// and starting at 2 s of a 3 s track yields about 1 s of them.
func TestFFmpegOpenerTranscodesAndSeeks(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	mp3, err := exec.Command("ffmpeg", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=330:duration=3", "-f", "mp3", "pipe:1").Output()
	if err != nil {
		t.Skipf("ffmpeg can't make mp3: %v", err)
	}
	op := &FFmpegOpener{Library: bytesStreamer{mp3}}
	src, err := op.Open(context.Background(), "t1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if n := countPackets(t, src); n < 145 || n > 160 {
		t.Fatalf("packets from the start = %d, want ~150", n)
	}
	src, err = op.Open(context.Background(), "t1", 2000)
	if err != nil {
		t.Fatal(err)
	}
	if n := countPackets(t, src); n < 45 || n > 60 {
		t.Fatalf("packets from 2 s = %d, want ~50", n)
	}
}

func TestFFmpegOpenerClosesEarly(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	mp3, err := exec.Command("ffmpeg", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=330:duration=30", "-f", "mp3", "pipe:1").Output()
	if err != nil {
		t.Skip()
	}
	src, err := (&FFmpegOpener{Library: bytesStreamer{mp3}}).Open(context.Background(), "t1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := src.Next(); err != nil {
		t.Fatal(err)
	}
	src.Close() // must not hang with ffmpeg mid-stream
	src.Close()
	if _, err := src.Next(); err == nil {
		t.Fatal("frames after close")
	}
}

// When ffmpeg fails (here: the input isn't audio) the source reports an
// error, not a clean end, so the player resumes instead of skipping.
func TestFFmpegFailureIsNotACleanEnd(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	op := &FFmpegOpener{Library: bytesStreamer{bytes.Repeat([]byte("not audio "), 1000)}}
	src, err := op.Open(context.Background(), "t1", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	for i := 0; i < 1000; i++ {
		_, err = src.Next()
		if err != nil {
			break
		}
	}
	if err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want an ffmpeg failure", err)
	}
}
