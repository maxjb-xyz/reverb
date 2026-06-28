// Package download defines the Downloader contract, a conformance suite, the
// dedup key, and the Manager (queue/workers/dedup-join/fallback/scan-debounce/
// cancel/retry). Adapters live in subpackages (e.g. download/spotdl).
package download

import (
	"context"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/registry"
)

// EventBus topics published by the Manager.
const (
	TopicQueued        = "download.queued"
	TopicProgress      = "download.progress"
	TopicComplete      = "download.complete"
	TopicFailed        = "download.failed"
	TopicLibraryUpdate = "library.updated"
	TopicRemoved       = "download.removed"
	TopicQueueState    = "download.queue"
)

// Downloader acquires an external track. CanDownload is a cheap heuristic for the
// fallback chain; Start performs the actual (blocking) download.
type Downloader interface {
	registry.Plugin

	// SupportedGranularities returns the SET of granularities this downloader is
	// capable of operating at. spotDL supports {track, album}; Lidarr supports
	// {album}. The returned slice must be non-empty and contain only valid constants.
	// Whether a particular granularity is ACTIVE for a given instance is controlled
	// by the Order map on DownloaderEntry — this method declares capability only.
	SupportedGranularities() []core.DownloadGranularity

	// CanDownload is a cheap capability heuristic (no network download).
	CanDownload(ctx context.Context, req core.DownloadRequest) (bool, error)

	// Start runs the download to completion. It reports progress via onProgress
	// (0-100, or -1 when unknown) and returns the output path on success. ctx is
	// cancelable: when canceled, an in-flight download must abort promptly.
	Start(ctx context.Context, req core.DownloadRequest, onProgress func(int)) (outputPath string, err error)
}

// DownloaderEntry pairs a Downloader with a per-instance enabled-granularity→order
// map. Only granularities present as keys in Order are active for this instance;
// the value is the sort key used by pick/pickAfter (ascending = higher priority).
// The default resolution (Task 2: config parsing) sets Order[g] = int(inst.Priority)
// for every g in plugin.SupportedGranularities().
type DownloaderEntry struct {
	Downloader Downloader
	Order      map[core.DownloadGranularity]int
}

// AsyncDownloader is an OPTIONAL capability. An adapter implementing it hands the
// request to an external manager (e.g. Lidarr) and reports progress by polling,
// instead of blocking in Start. The Manager detects it via a type assertion and
// runs such jobs on the reconciler lane (never pinning a worker). Detected for the
// admin UI via the registry capability probe "async".
type AsyncDownloader interface {
	// Submit hands req to the external system and returns a ref to track it. Must
	// NOT block on completion. An error means the request couldn't be placed (e.g.
	// album not found) → the job fails.
	Submit(ctx context.Context, req core.DownloadRequest) (ref string, err error)

	// Poll reports the current state of a submitted job. State == DownloadCompleted
	// means the files were imported into the library folder (the Manager then runs
	// the normal scan + rematch). State == DownloadFailed carries Error. Otherwise
	// the job is still running; Progress is 0-100 or -1 (unknown).
	Poll(ctx context.Context, ref string) (AsyncStatus, error)

	// CancelAsync best-effort abandons the external job.
	CancelAsync(ctx context.Context, ref string) error
}

// AsyncStatus is the polled state of an async download.
type AsyncStatus struct {
	State    core.DownloadStatus
	Progress int
	Error    string
}
