// Package download defines the Downloader contract, a conformance suite, the
// dedup key, and the Manager (queue/workers/dedup-join/fallback/scan-debounce/
// cancel/retry). Adapters live in subpackages (e.g. download/spotdl).
package download

import (
	"context"

	"github.com/maximusjb/crate/internal/core"
	"github.com/maximusjb/crate/internal/registry"
)

// EventBus topics published by the Manager.
const (
	TopicQueued        = "download.queued"
	TopicProgress      = "download.progress"
	TopicComplete      = "download.complete"
	TopicFailed        = "download.failed"
	TopicLibraryUpdate = "library.updated"
)

// Downloader acquires an external track. CanDownload is a cheap heuristic for the
// fallback chain; Start performs the actual (blocking) download.
type Downloader interface {
	registry.Plugin

	// CanDownload is a cheap capability heuristic (no network download).
	CanDownload(ctx context.Context, req core.DownloadRequest) (bool, error)

	// Start runs the download to completion. It reports progress via onProgress
	// (0-100, or -1 when unknown) and returns the output path on success. ctx is
	// cancelable: when canceled, an in-flight download must abort promptly.
	Start(ctx context.Context, req core.DownloadRequest, onProgress func(int)) (outputPath string, err error)
}
