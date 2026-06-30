package scrobble

import (
	"context"
	"errors"
)

// Track holds the metadata for a single track.
type Track struct {
	Title      string
	Artist     string
	Album      string
	DurationMs int
}

// ScrobblePlay pairs a Track with a unix-seconds timestamp (when playback started).
type ScrobblePlay struct {
	Track
	PlayedAt int64 // unix seconds
}

// Creds carries the three Last.fm credentials.  AuthURL/CompleteAuth only
// need APIKey+APISecret; NowPlaying/Scrobble also need SessionKey.
type Creds struct {
	APIKey     string
	APISecret  string
	SessionKey string
}

// Scrobbler is the provider-agnostic scrobbling interface.
// Task 3 (service/worker) depends on these exact method signatures.
type Scrobbler interface {
	// NowPlaying updates the "now playing" status on the provider.
	NowPlaying(ctx context.Context, c Creds, t Track) error

	// Scrobble submits one or more completed plays to the provider.
	// Returns the number of plays the provider accepted.
	Scrobble(ctx context.Context, c Creds, plays []ScrobblePlay) (accepted int, err error)

	// AuthURL starts the OAuth-style token flow: it fetches a request token
	// from the provider and returns the URL the user must visit plus the token
	// (needed for the subsequent CompleteAuth call).
	AuthURL(ctx context.Context, c Creds) (authURL, token string, err error)

	// CompleteAuth exchanges the approved token for a session key + username.
	CompleteAuth(ctx context.Context, c Creds, token string) (sessionKey, username string, err error)
}

// ErrAuth is returned when the provider rejects the caller's credentials
// (e.g. Last.fm error code 9 — invalid/expired session key).
var ErrAuth = errors.New("scrobble: provider rejected credentials")
