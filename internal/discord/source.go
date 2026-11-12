package discord

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/maxjb-xyz/reverb/internal/core"
)

// A FrameSource yields one track as 20 ms Opus packets.
type FrameSource interface {
	// Next returns the next packet, io.EOF at the end of the track.
	Next() ([]byte, error)
	Close()
}

// SourceOpener starts a track at startMs.
type SourceOpener interface {
	Open(ctx context.Context, trackID string, startMs int64) (FrameSource, error)
}

// Streamer is the slice of the library the bot needs: the audio itself.
type Streamer interface {
	Stream(ctx context.Context, trackID string, opts core.StreamOpts, rangeHeader string) (core.StreamHandle, error)
}

// FFmpegOpener streams a library track through ffmpeg into 48 kHz stereo
// Opus, the only format Discord voice accepts. ffmpeg is already in the image
// (spotDL needs it), so the bot needs no native code.
type FFmpegOpener struct {
	Library Streamer
	// Path to ffmpeg; "ffmpeg" (from PATH) when empty.
	Path string
	// Bitrate in kbit/s; 128 when zero. Discord caps most channels at 64-96.
	BitrateK int
}

// Open implements SourceOpener.
func (f *FFmpegOpener) Open(ctx context.Context, trackID string, startMs int64) (FrameSource, error) {
	if f.Library == nil {
		return nil, errors.New("no library")
	}
	ctx, cancel := context.WithCancel(ctx)
	h, err := f.Library.Stream(ctx, trackID, core.StreamOpts{}, "")
	if err != nil {
		cancel()
		return nil, fmt.Errorf("stream %s: %w", trackID, err)
	}
	if h.StatusCode >= 400 {
		h.Body.Close()
		cancel()
		return nil, fmt.Errorf("stream %s: status %d", trackID, h.StatusCode)
	}
	path := f.Path
	if path == "" {
		path = "ffmpeg"
	}
	br := f.BitrateK
	if br <= 0 {
		br = 128
	}
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	if startMs > 0 {
		// After -i on a pipe this decodes and drops up to the start point.
		args = append(args, "-i", "pipe:0", "-ss", strconv.FormatFloat(float64(startMs)/1000, 'f', 3, 64))
	} else {
		args = append(args, "-i", "pipe:0")
	}
	args = append(args,
		"-vn", "-map", "0:a:0",
		"-ar", "48000", "-ac", "2",
		"-c:a", "libopus", "-b:a", strconv.Itoa(br)+"k",
		"-frame_duration", "20", "-application", "audio",
		"-f", "ogg", "pipe:1",
	)
	cmd := exec.CommandContext(ctx, path, args...)
	// Once ffmpeg exits, don't wait long on the copy of the upstream body
	// into its stdin.
	cmd.WaitDelay = 2 * time.Second
	// -nostdin only stops ffmpeg reading keys; pipe:0 still reads Stdin.
	cmd.Stdin = h.Body
	out, err := cmd.StdoutPipe()
	if err != nil {
		h.Body.Close()
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		h.Body.Close()
		cancel()
		return nil, fmt.Errorf("start ffmpeg: %w", err)
	}
	return &ffmpegSource{cmd: cmd, body: h.Body, cancel: cancel, ogg: newOggReader(out)}, nil
}

type ffmpegSource struct {
	cmd     *exec.Cmd
	body    io.ReadCloser
	cancel  context.CancelFunc
	ogg     *oggReader
	once    sync.Once
	waitErr error
	closed  atomic.Bool
}

var errSourceClosed = errors.New("source closed")

func (s *ffmpegSource) Next() ([]byte, error) {
	if s.closed.Load() {
		return nil, errSourceClosed
	}
	pkt, err := s.ogg.Next()
	if errors.Is(err, io.EOF) {
		// A clean end of the Ogg stream still means a cut-off track if
		// ffmpeg failed (the upstream request broke).
		if werr := s.wait(); werr != nil && !s.closed.Load() {
			return nil, fmt.Errorf("ffmpeg: %w", werr)
		}
	}
	return pkt, err
}

func (s *ffmpegSource) wait() error {
	s.once.Do(func() { s.waitErr = s.cmd.Wait() })
	return s.waitErr
}

func (s *ffmpegSource) Close() {
	s.closed.Store(true)
	s.cancel() // kills ffmpeg and aborts the upstream request
	s.body.Close()
	_ = s.wait()
}
