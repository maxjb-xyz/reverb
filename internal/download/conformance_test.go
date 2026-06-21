package download

import (
	"context"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/registry"
)

type fakeDownloader struct{}

func (fakeDownloader) Type() string                             { return "downloader" }
func (fakeDownloader) Name() string                             { return "fake" }
func (fakeDownloader) ConfigSchema() registry.ConfigSchema      { return registry.ConfigSchema{} }
func (fakeDownloader) Init(cfg map[string]any) error            { return nil }
func (fakeDownloader) TestConnection(ctx context.Context) error { return nil }
func (fakeDownloader) CanDownload(ctx context.Context, req core.DownloadRequest) (bool, error) {
	return req.Title != "", nil
}
func (fakeDownloader) Start(ctx context.Context, req core.DownloadRequest, onProgress func(int)) (string, error) {
	onProgress(50)
	onProgress(100)
	return "/out/" + req.ExternalID + ".mp3", nil
}

func TestFakeDownloaderConformance(t *testing.T) {
	RunConformance(t, fakeDownloader{})
}
