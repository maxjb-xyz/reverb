// Package search defines the SearchSource contract, a conformance suite, and a
// fan-out aggregator that streams per-source results (each pre-matched) over a
// channel of Envelopes. Adapters live in subpackages (e.g. search/spotify).
package search

import (
	"context"

	"github.com/maxjb-xyz/crate/internal/core"
	"github.com/maxjb-xyz/crate/internal/registry"
)

// SearchSource is an external catalog (MVP: Spotify). ISRC/MBID are DATA on the
// returned results, not capabilities.
type SearchSource interface {
	registry.Plugin
	Search(ctx context.Context, q string, t core.EntityType) ([]core.ExternalResult, error)
	GetAlbum(ctx context.Context, externalID string) (core.ExternalAlbum, error)
}

// DiscographyProvider is an OPTIONAL capability (P2 artist pages). Conformance
// does NOT require it; the aggregator/UI detect it via a type assertion.
type DiscographyProvider interface {
	GetArtistDiscography(ctx context.Context, externalID string) ([]core.ExternalAlbum, error)
}

// EnvelopeStatus is the per-source outcome streamed to the client.
type EnvelopeStatus string

const (
	StatusOK      EnvelopeStatus = "ok"
	StatusTimeout EnvelopeStatus = "timeout"
	StatusError   EnvelopeStatus = "error"
)

// Envelope is one per-source result batch. Each Result already carries its Match.
type Envelope struct {
	Source  string                `json:"source"`
	Status  EnvelopeStatus        `json:"status"`
	Results []core.ExternalResult `json:"results"`
	Cursor  string                `json:"cursor,omitempty"`
	Error   string                `json:"error,omitempty"`
}
