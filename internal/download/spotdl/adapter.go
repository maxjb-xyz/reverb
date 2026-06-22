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
	"log"
	"regexp"
	"strconv"
	"strings"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/download"
	"github.com/maxjb-xyz/reverb/internal/registry"
)

var _ download.Downloader = (*Adapter)(nil)

// progressRe extracts an integer percentage from a stdout line, e.g. "...: 80%".
var progressRe = regexp.MustCompile(`(\d{1,3})\s*%`)

// failureRe matches the fatal spotDL errors that mean no file was produced even
// though the process exits 0 (per-song failures don't change the exit code).
var failureRe = regexp.MustCompile(`AudioProviderError|YT-DLP download error|LookupError|DownloaderError`)

// stageProgress maps spotDL's --simple-tui STAGE labels to coarse progress. When
// piped, spotDL prints stages ("...: Downloading", "...: Embedding metadata",
// "...: Done") rather than a percentage, so there is no per-% to parse — these
// give honest, monotonic movement instead of a stuck ring. A real "NN%" line, if
// one ever appears (e.g. under a PTY), still wins via progressRe.
var stageProgress = []struct {
	re  *regexp.Regexp
	pct int
}{
	{regexp.MustCompile(`(?i):\s*Downloading\b`), 25},
	{regexp.MustCompile(`(?i):\s*Converting\b`), 60},
	{regexp.MustCompile(`(?i):\s*Embedding\b`), 90},
	{regexp.MustCompile(`(?i):\s*Done\b`), 100},
}

// Adapter implements download.Downloader for spotDL.
type Adapter struct {
	runner       Runner
	outputDir    string
	binary       string
	clientID     string
	clientSecret string
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
		{Key: "client_id", Label: "Spotify Client ID", Type: "string", Required: false},
		{Key: "client_secret", Label: "Spotify Client Secret", Type: "string", Required: false, Secret: true},
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
	// Optional own Spotify app credentials — spotDL's bundled/shared client gets
	// rate-limited (429 + long backoff); using your own avoids that.
	if v, ok := cfg["client_id"].(string); ok {
		a.clientID = v
	}
	if v, ok := cfg["client_secret"].(string); ok {
		a.clientSecret = v
	}
	if a.runner == nil {
		a.runner = ExecRunner{}
	}
	return nil
}

// TestConnection runs `<binary> --version` to confirm spotDL is present/runnable.
func (a *Adapter) TestConnection(ctx context.Context) error {
	err := a.runner.Run(ctx, a.binary, []string{"--version"}, func(string) {})
	if err != nil {
		return fmt.Errorf("spotdl --version: %w", err)
	}
	return nil
}

// CanDownload is a cheap heuristic: spotDL can attempt any track that has at least
// a title and an artist. No network call.
func (a *Adapter) CanDownload(ctx context.Context, req core.DownloadRequest) (bool, error) {
	return req.Title != "" && req.Artist != "", nil
}

// redactArgs renders args for logging with the --client-secret value masked.
func redactArgs(args []string) string {
	out := make([]string, len(args))
	copy(out, args)
	for i := 1; i < len(out); i++ {
		if out[i-1] == "--client-secret" {
			out[i] = "****"
		}
	}
	return strings.Join(out, " ")
}

// Start shells out to spotDL and streams progress. Unparseable lines degrade to
// unknown progress (onProgress(-1) once), never an error. On success it returns
// the output directory as the path hint (spotDL writes the file under output_dir;
// the scan picks it up — the exact filename is spotDL's concern).
func (a *Adapter) Start(ctx context.Context, req core.DownloadRequest, onProgress func(int)) (string, error) {
	// Prefer a Spotify track URL when available: spotDL fetches exact metadata via
	// the configured client creds and matches YouTube far more reliably than a
	// free-form text query — essential for obscure/long classical titles that
	// YouTube Music text search fails to find (e.g. LookupError on Einaudi,
	// Purcell "Dido and Aeneas" arrangements, etc.).
	// Fallback: "<artist> - <title>" text search for non-Spotify sources or missing IDs.
	var query string
	if req.Source == "spotify" && req.ExternalID != "" {
		query = "https://open.spotify.com/track/" + req.ExternalID
	} else {
		query = strings.TrimSpace(req.Artist + " - " + req.Title)
	}
	// spotDL's CLI is `spotdl [options] <operation> <query>`. It does NOT accept a
	// "--" end-of-options separator (it reports it as an unrecognized argument), so
	// every option must come BEFORE the "download" operation, query trailing.
	//
	// --output is a FILENAME TEMPLATE, not just a directory. A bare directory is
	// unreliable — spotDL falls back to its default (the current working
	// directory), which is why downloads "completed" yet never appeared in the
	// output dir. Give it an explicit "<dir>/{artists} - {title}.{output-ext}"
	// template so the file is written into outputDir with a sane name.
	//
	// --simple-tui makes spotDL emit plain, pipe-friendly progress lines; its rich
	// TUI is suppressed when stdout is not a terminal (our case), which is why the
	// terminal shows a progress bar but our captured output didn't.
	outputTemplate := strings.TrimRight(a.outputDir, "/") + "/{artists} - {title}.{output-ext}"
	args := []string{}
	if a.clientID != "" && a.clientSecret != "" {
		args = append(args, "--client-id", a.clientID, "--client-secret", a.clientSecret)
	}
	// Prefer YouTube Music but fall back to plain YouTube when a track is absent
	// from YT-Music's catalog (common for obscure/regional/classical releases).
	args = append(args, "--audio", "youtube-music", "youtube")
	args = append(args, "--simple-tui", "--output", outputTemplate, "download", query)

	log.Printf("spotdl: exec %s %s", a.binary, redactArgs(args))

	sawProgress := false
	var failure string // first fatal error line spotDL emitted, if any
	rerr := a.runner.Run(ctx, a.binary, args, func(line string) {
		// Echo spotDL's own output (stdout+stderr) so a slow/stuck/failing
		// download is diagnosable from the Reverb logs.
		if s := strings.TrimSpace(line); s != "" {
			log.Printf("spotdl> %s", s)
		}
		// spotDL exits 0 even when a song fails to download (it just logs the
		// error and moves on), so the exit code alone would report a non-existent
		// file as a success. Detect the fatal markers and surface them as an error.
		if failure == "" && failureRe.MatchString(line) {
			failure = strings.TrimSpace(line)
		}
		if m := progressRe.FindStringSubmatch(line); m != nil {
			if p, err := strconv.Atoi(m[1]); err == nil && p >= 0 && p <= 100 {
				sawProgress = true
				onProgress(p)
				return
			}
		}
		// No percentage — fall back to stage-based progress.
		for _, st := range stageProgress {
			if st.re.MatchString(line) {
				sawProgress = true
				onProgress(st.pct)
				return
			}
		}
		// Unparseable line: ignore (graceful degradation).
	})
	if rerr != nil {
		log.Printf("spotdl: %q failed: %v", query, rerr)
		return "", fmt.Errorf("spotdl download %q: %w", query, rerr)
	}
	if failure != "" {
		log.Printf("spotdl: %q failed: %s", query, failure)
		return "", fmt.Errorf("spotdl download %q: %s", query, failure)
	}
	if !sawProgress {
		onProgress(-1) // indeterminate: spotDL gave no parseable percentage
	}
	log.Printf("spotdl: %q finished (output_dir=%s)", query, a.outputDir)
	return a.outputDir, nil
}
