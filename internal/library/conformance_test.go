package library

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/registry"
)

// fakeAdapter is a minimal in-memory LibraryAdapter that satisfies the contract.
type fakeAdapter struct{}

func (fakeAdapter) Type() string                             { return "library" }
func (fakeAdapter) Name() string                             { return "fake" }
func (fakeAdapter) ConfigSchema() registry.ConfigSchema      { return registry.ConfigSchema{} }
func (fakeAdapter) Init(cfg map[string]any) error            { return nil }
func (fakeAdapter) TestConnection(ctx context.Context) error { return nil }

func (fakeAdapter) Search(ctx context.Context, q string, types []core.EntityType) (core.SearchResults, error) {
	return core.SearchResults{
		Tracks:  []core.Track{{ID: "t1", Title: "Song"}},
		Albums:  []core.Album{{ID: "al1", Name: "Album"}},
		Artists: []core.Artist{{ID: "ar1", Name: "Artist"}},
	}, nil
}
func (fakeAdapter) GetArtist(ctx context.Context, id string) (core.Artist, error) {
	return core.Artist{ID: id, Name: "Artist", Albums: []core.Album{{ID: "al1", Name: "Album"}}}, nil
}
func (fakeAdapter) GetAlbum(ctx context.Context, id string) (core.Album, error) {
	return core.Album{ID: id, Name: "Album", Tracks: []core.Track{{ID: "t1", Title: "Song"}}}, nil
}
func (fakeAdapter) GetPlaylists(ctx context.Context) ([]core.Playlist, error) {
	return []core.Playlist{{ID: "p1", Name: "Mix"}}, nil
}
func (fakeAdapter) Stream(ctx context.Context, trackID string, opts core.StreamOpts, rangeHeader string) (core.StreamHandle, error) {
	return core.StreamHandle{
		Body:          io.NopCloser(strings.NewReader("audio-bytes")),
		ContentType:   "audio/mpeg",
		ContentLength: 11,
		AcceptRanges:  "bytes",
		StatusCode:    200,
	}, nil
}
func (fakeAdapter) CoverArt(ctx context.Context, id string, size int) (core.CoverArt, error) {
	return core.CoverArt{Body: io.NopCloser(strings.NewReader("img")), ContentType: "image/jpeg"}, nil
}
func (fakeAdapter) StartScan(ctx context.Context) error { return nil }
func (fakeAdapter) ScanStatus(ctx context.Context) (core.ScanStatus, error) {
	return core.ScanStatus{}, nil
}

func TestFakeAdapterConformance(t *testing.T) {
	RunConformance(t, fakeAdapter{})
}
