package main

import (
	"context"
	"encoding/json"
	"log"

	"github.com/google/uuid"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// seedBundledDownloader ensures the bundled spotDL downloader is present as a
// configured instance, so downloads work with zero manual setup (the image ships
// spotDL + ffmpeg). It runs only when NO downloader instance exists yet, so a
// user who has configured their own downloader is respected. output_dir comes
// from REVERB_DOWNLOAD_DIR (the Docker image sets it to /music), defaulting to
// ./downloads for bare runs. Best-effort: any error is logged, never fatal.
func seedBundledDownloader(ctx context.Context, q *db.Queries, getenv func(string) string) {
	instances, err := q.ListAdapterInstances(ctx)
	if err != nil {
		return
	}
	for _, inst := range instances {
		if inst.Type == "downloader" {
			return // already have a downloader — nothing to seed
		}
	}

	dir := getenv("REVERB_DOWNLOAD_DIR")
	if dir == "" {
		dir = "./downloads"
	}
	cfg, _ := json.Marshal(map[string]any{"output_dir": dir})

	if err := q.CreateAdapterInstance(ctx, db.CreateAdapterInstanceParams{
		ID:         uuid.NewString(),
		Type:       "downloader",
		Name:       "spotdl",
		Enabled:    1,
		Priority:   0,
		ConfigJson: string(cfg),
	}); err != nil {
		log.Printf("could not seed bundled spotdl downloader: %v", err)
		return
	}
	log.Printf("seeded bundled spotdl downloader (output_dir=%s)", dir)
}
