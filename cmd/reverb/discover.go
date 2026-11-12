package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/maxjb-xyz/reverb/internal/discover"
	"github.com/maxjb-xyz/reverb/internal/library/embedded"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// localPaths is the optional library capability that maps a track to its file.
type localPaths interface {
	LocalTrackPath(id string) (string, bool)
}

// newDiscoverService wires Discover Weekly to the live download manager and
// library. Removal is limited to the music and download folders.
func newDiscoverService(q *db.Queries, eng discover.Engine, r *serviceReloader) *discover.Service {
	live := r.servicesProvider()
	downloads := r.downloadsProvider()
	return discover.New(q, eng, discover.Options{
		Downloads: func() discover.Downloader {
			if m := downloads(); m != nil {
				return m
			}
			return nil
		},
		Remove: func(path string) error {
			return discover.SafeRemover(discoverRoots(context.Background(), q, os.Getenv))(path)
		},
		Locate: func(id string) (string, bool) {
			lib, _ := live()
			if p, ok := lib.(localPaths); ok {
				return p.LocalTrackPath(id)
			}
			return "", false
		},
		Rescan: func(ctx context.Context) {
			lib, _ := live()
			if lib == nil {
				return
			}
			if err := lib.StartScan(ctx); err != nil {
				log.Printf("discover: rescan after cleanup: %v", err)
			}
		},
	}, time.Now, uuid.NewString)
}

// discoverRoots lists the folders Discover may delete its own downloads from:
// the music folder and every downloader's output folder.
func discoverRoots(ctx context.Context, q *db.Queries, getenv func(string) string) []string {
	roots := []string{embedded.MusicDir(getenv)}
	insts, err := q.ListAdapterInstances(ctx)
	if err != nil {
		return roots
	}
	for _, in := range insts {
		if in.Type != "downloader" || in.ConfigJson == "" {
			continue
		}
		var cfg map[string]any
		if json.Unmarshal([]byte(in.ConfigJson), &cfg) != nil {
			continue
		}
		if d, ok := cfg["output_dir"].(string); ok && d != "" {
			roots = append(roots, filepath.Clean(d))
		}
	}
	return roots
}
