package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/maxjb-xyz/reverb/internal/analysis"
	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/library"
	"github.com/maxjb-xyz/reverb/internal/search"
)

// libraryAudio opens a library song for the analyzer: straight from disk when
// the library is the built-in one, otherwise through the library's stream in
// its original format.
func libraryAudio(live func() (library.LibraryAdapter, []search.SearchSource)) analysis.AudioSource {
	return func(ctx context.Context, id, suffix string) (string, io.ReadCloser, error) {
		lib, _ := live()
		if lib == nil {
			return "", nil, errors.New("no library")
		}
		if p, ok := lib.(localPaths); ok {
			if path, ok := p.LocalTrackPath(id); ok {
				f, err := os.Open(path)
				if err == nil {
					return filepath.Base(path), f, nil
				}
			}
		}
		h, err := lib.Stream(ctx, id, core.StreamOpts{Format: "raw"}, "")
		if err != nil {
			return "", nil, err
		}
		name := "song"
		if suffix != "" {
			name += "." + suffix
		}
		return name, h.Body, nil
	}
}
