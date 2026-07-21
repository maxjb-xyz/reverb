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

// FailureClass categorizes why a Downloader.Start call failed, so the Manager
// can decide whether a burst of failures warrants pacing back off and whether
// a given failure is worth automatically retrying. Adapters that can tell
// these apart (e.g. spotDL parsing yt-dlp's error text) return a
// ClassifiedError; adapters that can't return a plain error, which the
// Manager treats as ClassUnknown.
type FailureClass string

const (
	// ClassRateLimited means the provider returned a rate-limit response (e.g.
	// HTTP 429). Worth backing off and retrying later.
	ClassRateLimited FailureClass = "rate_limited"
	// ClassBotChallenge means the provider is demanding proof of a real,
	// authenticated session (e.g. YouTube's "sign in to confirm you're not a
	// bot"). Worth backing off and retrying later — resolved long-term by
	// configuring authenticated cookies.
	ClassBotChallenge FailureClass = "bot_challenge"
	// ClassUnavailable means the specific media is unavailable (removed,
	// region-locked, private). Retrying will not help.
	ClassUnavailable FailureClass = "unavailable"
	// ClassNoMatch means the provider's search found nothing for this query.
	// Retrying will not help.
	ClassNoMatch FailureClass = "no_match"
	// ClassSpotifyAPIError means the failure originated from the Spotify API
	// (metadata lookup), not the audio provider. Retrying will not help.
	ClassSpotifyAPIError FailureClass = "spotify_api_error"
	// ClassUnknown is the fallback for any failure the adapter could not
	// classify further. Never retried automatically; never triggers pacing.
	ClassUnknown FailureClass = "unknown"
)

// ClassifiedError wraps a downloader failure with the FailureClass that
// explains it, so the Manager can drive adaptive pacing and auto-retry
// without parsing error strings itself.
type ClassifiedError struct {
	Class FailureClass
	Err   error
}

func (e ClassifiedError) Error() string { return e.Err.Error() }
func (e ClassifiedError) Unwrap() error { return e.Err }
