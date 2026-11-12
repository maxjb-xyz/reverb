package main

import (
	"context"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/store"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

func TestDiscoverRoots_MusicFolderAndDownloaderOutputs(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/roots.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, in := range []db.CreateAdapterInstanceParams{
		{ID: "a", Type: "downloader", Name: "spotdl", Enabled: 1, ConfigJson: `{"output_dir":"/srv/dl/"}`},
		{ID: "b", Type: "library", Name: "subsonic", Enabled: 1, ConfigJson: `{"output_dir":"/not/a/downloader"}`},
		{ID: "c", Type: "downloader", Name: "lidarr", Enabled: 1, ConfigJson: `{}`},
	} {
		if err := st.Q().CreateAdapterInstance(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	env := map[string]string{"REVERB_DOWNLOAD_DIR": "/music"}
	got := discoverRoots(ctx, st.Q(), func(k string) string { return env[k] })
	if len(got) != 2 || got[0] != "/music" || got[1] != "/srv/dl" {
		t.Fatalf("roots = %v", got)
	}
}
