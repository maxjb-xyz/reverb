// Package spotdl is the spotDL Downloader adapter. It shells out via an injectable
// Runner and parses progress from stdout, DEGRADING GRACEFULLY: an unparseable
// line yields unknown progress (-1), never an error.
//
// VERSION PIN: spotDL output formatting is fragile. The Docker image pins spotDL
// (see deployment docs / docker-compose); if upgrading spotDL, re-verify the
// progress regex below against the new output format.
package spotdl

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/maximusjb/crate/internal/core"
	"github.com/maximusjb/crate/internal/download"
	"github.com/maximusjb/crate/internal/registry"
)

var _ download.Downloader = (*Adapter)(nil)

// progressRe extracts an integer percentage from a stdout line, e.g. "...: 80%".
var progressRe = regexp.MustCompile(`(\d{1,3})\s*%`)

// Adapter implements download.Downloader for spotDL.
type Adapter struct {
	runner    Runner
	outputDir string
	binary    string
}

func New() *Adapter {
	return &Adapter{runner: ExecRunner{}, binary: "spotdl"}
}

// WithRunner injects a Runner (test seam). Call before Init.
func (a *Adapter) WithRunner(r Runner) *Adapter {
	a.runner = r
	return a
}

func (a *Adapter) Type() string { return "downloader" }
func (a *Adapter) Name() string { return "spotdl" }

func (a *Adapter) ConfigSchema() registry.ConfigSchema {
	return registry.ConfigSchema{Fields: []registry.ConfigField{
		{Key: "output_dir", Label: "Output directory", Type: "string", Required: true},
		{Key: "binary_path", Label: "spotDL binary path", Type: "string", Required: false},
	}}
}

func (a *Adapter) Init(cfg map[string]any) error {
	if v, ok := cfg["output_dir"].(string); ok && v != "" {
		a.outputDir = v
	}
	if a.outputDir == "" {
		return fmt.Errorf("spotdl: output_dir is required")
	}
	if v, ok := cfg["binary_path"].(string); ok && v != "" {
		a.binary = v
	}
	if a.runner == nil {
		a.runner = ExecRunner{}
	}
	return nil
}

// TestConnection runs `<binary> --version` to confirm spotDL is present/runnable.
func (a *Adapter) TestConnection(ctx context.Context) error {
	ran := false
	err := a.runner.Run(ctx, a.binary, []string{"--version"}, func(string) { ran = true })
	if err != nil {
		return fmt.Errorf("spotdl --version: %w", err)
	}
	_ = ran
	return nil
}

// CanDownload is a cheap heuristic: spotDL can attempt any track that has at least
// a title and an artist. No network call.
func (a *Adapter) CanDownload(ctx context.Context, req core.DownloadRequest) (bool, error) {
	return req.Title != "" && req.Artist != "", nil
}

// Start shells out to spotDL and streams progress. Unparseable lines degrade to
// unknown progress (onProgress(-1) once), never an error. On success it returns
// the output directory as the path hint (spotDL writes the file under output_dir;
// the scan picks it up — the exact filename is spotDL's concern).
func (a *Adapter) Start(ctx context.Context, req core.DownloadRequest, onProgress func(int)) (string, error) {
	query := strings.TrimSpace(req.Artist + " - " + req.Title)
	args := []string{"download", query, "--output", a.outputDir}

	sawProgress := false
	rerr := a.runner.Run(ctx, a.binary, args, func(line string) {
		if m := progressRe.FindStringSubmatch(line); m != nil {
			if p, err := strconv.Atoi(m[1]); err == nil && p >= 0 && p <= 100 {
				sawProgress = true
				onProgress(p)
				return
			}
		}
		// Unparseable line: ignore (graceful degradation).
	})
	if rerr != nil {
		return "", fmt.Errorf("spotdl download %q: %w", query, rerr)
	}
	if !sawProgress {
		onProgress(-1) // indeterminate: spotDL gave no parseable percentage
	}
	return a.outputDir, nil
}
