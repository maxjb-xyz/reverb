package download

import (
	"context"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/core"
)

// RunConformance exercises the Downloader contract. Call it from each adapter's
// test package with a configured downloader pointed at a FAKE runner (never a
// real download). Start must report at least one progress value and return a
// non-empty output path on success.
func RunConformance(t *testing.T, d Downloader) {
	t.Helper()
	ctx := context.Background()

	t.Run("Plugin/identity", func(t *testing.T) {
		if d.Type() != "downloader" {
			t.Errorf("Type() = %q, want \"downloader\"", d.Type())
		}
		if d.Name() == "" {
			t.Error("Name() must not be empty")
		}
	})

	req := core.DownloadRequest{
		Source: "spotify", ExternalID: "e1", Artist: "Artist", Title: "Song", Album: "Album",
	}

	t.Run("CanDownload/cheap-bool", func(t *testing.T) {
		if _, err := d.CanDownload(ctx, req); err != nil {
			t.Fatalf("CanDownload: %v", err)
		}
	})

	t.Run("Start/reports-progress-and-output", func(t *testing.T) {
		var last = -2
		var calls int
		out, err := d.Start(ctx, req, func(p int) { last = p; calls++ })
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
		if out == "" {
			t.Error("Start returned empty output path on success")
		}
		if calls == 0 {
			t.Error("Start never reported progress")
		}
		_ = last
	})

	t.Run("Start/respects-canceled-ctx", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		// A canceled ctx may error or return promptly; it must not panic or block.
		_, _ = d.Start(cctx, req, func(int) {})
	})
}
