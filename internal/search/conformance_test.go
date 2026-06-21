package search

import (
	"context"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/registry"
)

type fakeSource struct{}

func (fakeSource) Type() string                             { return "search" }
func (fakeSource) Name() string                             { return "fake" }
func (fakeSource) ConfigSchema() registry.ConfigSchema      { return registry.ConfigSchema{} }
func (fakeSource) Init(cfg map[string]any) error            { return nil }
func (fakeSource) TestConnection(ctx context.Context) error { return nil }
func (fakeSource) Search(ctx context.Context, q string, t core.EntityType) ([]core.ExternalResult, error) {
	return []core.ExternalResult{{Source: "fake", ExternalID: "e1", Title: "Song", Type: t}}, nil
}
func (fakeSource) GetAlbum(ctx context.Context, externalID string) (core.ExternalAlbum, error) {
	return core.ExternalAlbum{Source: "fake", ExternalID: externalID, Name: "Album", Tracks: []core.ExternalResult{}}, nil
}

func TestFakeSourceConformance(t *testing.T) {
	RunConformance(t, fakeSource{})
}
