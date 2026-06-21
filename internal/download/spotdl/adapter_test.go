package spotdl

import (
	"context"
	"strings"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/download"
)

// fakeRunner replays canned stdout lines (incl. one malformed line) and records
// the command it was asked to run. It never shells out.
type fakeRunner struct {
	lines   []string
	gotName string
	gotArgs []string
	runErr  error
}

func (f *fakeRunner) Run(ctx context.Context, name string, args []string, onLine func(string)) error {
	f.gotName = name
	f.gotArgs = args
	for _, l := range f.lines {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		onLine(l)
	}
	return f.runErr
}

func newAdapter(t *testing.T, r Runner) *Adapter {
	t.Helper()
	a := New().WithRunner(r)
	if err := a.Init(map[string]any{"output_dir": "/tmp/music", "binary_path": "spotdl"}); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestIdentityAndSchema(t *testing.T) {
	a := New()
	if a.Type() != "downloader" || a.Name() != "spotdl" {
		t.Fatalf("identity: %q/%q", a.Type(), a.Name())
	}
	keys := map[string]bool{}
	for _, f := range a.ConfigSchema().Fields {
		keys[f.Key] = true
	}
	if !keys["output_dir"] {
		t.Error("schema missing output_dir")
	}
	if !keys["binary_path"] {
		t.Error("schema missing binary_path")
	}
}

func TestCanDownloadHeuristic(t *testing.T) {
	a := newAdapter(t, &fakeRunner{})
	ok, err := a.CanDownload(context.Background(), core.DownloadRequest{Artist: "A", Title: "T"})
	if err != nil || !ok {
		t.Fatalf("CanDownload(complete req) = %v,%v want true,nil", ok, err)
	}
	ok, _ = a.CanDownload(context.Background(), core.DownloadRequest{})
	if ok {
		t.Fatal("CanDownload(empty req) should be false")
	}
}

func TestStartParsesProgressAndDegradesGracefully(t *testing.T) {
	// Realistic spotDL output incl. a malformed line that must NOT error.
	r := &fakeRunner{lines: []string{
		`Found 1 song`,
		`Downloading "A - T": 25%`,
		`THIS IS A MALFORMED LINE WITH NO PERCENT`,
		`Downloading "A - T": 80%`,
		`Downloaded "A - T": /tmp/music/A - T.mp3`,
	}}
	a := newAdapter(t, r)

	var seen []int
	out, err := a.Start(context.Background(), core.DownloadRequest{Source: "spotify", ExternalID: "e1", Artist: "A", Title: "T"}, func(p int) {
		seen = append(seen, p)
	})
	if err != nil {
		t.Fatalf("Start errored on malformed line (must degrade): %v", err)
	}
	if out == "" {
		t.Fatal("Start returned empty output path")
	}
	// At least the 25 and 80 progress values were parsed; the malformed line is ignored.
	has := func(v int) bool {
		for _, p := range seen {
			if p == v {
				return true
			}
		}
		return false
	}
	if !has(25) || !has(80) {
		t.Fatalf("expected parsed progress 25 and 80, got %v", seen)
	}
}

func TestStartUnknownProgressIsNotAnError(t *testing.T) {
	// No parseable percentage at all → progress reported as -1 (indeterminate),
	// success still returns an output path (the URL/query forms the spotdl arg).
	r := &fakeRunner{lines: []string{`some opaque output`, `more opaque output`}}
	a := newAdapter(t, r)
	out, err := a.Start(context.Background(), core.DownloadRequest{Source: "spotify", ExternalID: "e2", Artist: "A", Title: "T"}, func(int) {})
	if err != nil {
		t.Fatalf("unknown progress must not error: %v", err)
	}
	if out == "" {
		t.Fatal("expected a non-empty output path even with unknown progress")
	}
}

func TestStartPassesOutputDirAndQuery(t *testing.T) {
	r := &fakeRunner{lines: []string{`Downloaded: ok`}}
	a := newAdapter(t, r)
	_, _ = a.Start(context.Background(), core.DownloadRequest{Source: "spotify", ExternalID: "e1", Artist: "Daft Punk", Title: "One More Time"}, func(int) {})
	if r.gotName != "spotdl" {
		t.Fatalf("binary = %q, want spotdl", r.gotName)
	}
	joined := ""
	for _, a := range r.gotArgs {
		joined += a + " "
	}
	if !strings.Contains(joined, "/tmp/music") {
		t.Fatalf("output dir not passed in args: %v", r.gotArgs)
	}
	if !strings.Contains(joined, "Daft Punk") && !strings.Contains(joined, "One More Time") {
		t.Fatalf("search query not passed in args: %v", r.gotArgs)
	}
}

func TestStartPassesSpotifyCredentials(t *testing.T) {
	r := &fakeRunner{lines: []string{`Downloaded: ok`}}
	a := New().WithRunner(r)
	if err := a.Init(map[string]any{
		"output_dir": "/tmp/music", "binary_path": "spotdl",
		"client_id": "myid", "client_secret": "mysecret",
	}); err != nil {
		t.Fatal(err)
	}
	_, _ = a.Start(context.Background(), core.DownloadRequest{Artist: "A", Title: "T"}, func(int) {})

	idIdx, secIdx, dlIdx := -1, -1, -1
	for i, arg := range r.gotArgs {
		switch arg {
		case "--client-id":
			if i+1 < len(r.gotArgs) && r.gotArgs[i+1] == "myid" {
				idIdx = i
			}
		case "--client-secret":
			if i+1 < len(r.gotArgs) && r.gotArgs[i+1] == "mysecret" {
				secIdx = i
			}
		case "download":
			dlIdx = i
		}
	}
	if idIdx < 0 || secIdx < 0 {
		t.Fatalf("credentials not passed: %v", r.gotArgs)
	}
	if idIdx > dlIdx || secIdx > dlIdx {
		t.Fatalf("credentials must precede the download operation: %v", r.gotArgs)
	}
}

func TestStartOmitsCredentialsWhenUnset(t *testing.T) {
	r := &fakeRunner{lines: []string{`Downloaded: ok`}}
	a := newAdapter(t, r) // output_dir + binary only, no creds
	_, _ = a.Start(context.Background(), core.DownloadRequest{Artist: "A", Title: "T"}, func(int) {})
	if strings.Contains(strings.Join(r.gotArgs, " "), "--client-") {
		t.Fatalf("credentials should be omitted when unset: %v", r.gotArgs)
	}
}

func TestStartArgStructure(t *testing.T) {
	// Regression: spotDL has NO "--" separator (it rejects it). Options (incl.
	// --output) precede the "download" operation; the query is the trailing arg.
	r := &fakeRunner{lines: []string{`Downloaded: ok`}}
	a := newAdapter(t, r)
	_, _ = a.Start(context.Background(), core.DownloadRequest{Artist: "A", Title: "T"}, func(int) {})

	outIdx, dlIdx := -1, -1
	for i, arg := range r.gotArgs {
		if arg == "--" {
			t.Fatalf("must not pass a -- separator (spotDL rejects it): %v", r.gotArgs)
		}
		switch arg {
		case "--output":
			outIdx = i
		case "download":
			dlIdx = i
		}
	}
	if outIdx < 0 || dlIdx < 0 || outIdx > dlIdx {
		t.Fatalf("--output must precede the download operation: %v", r.gotArgs)
	}
	if n := len(r.gotArgs); n == 0 || r.gotArgs[n-1] != "A - T" {
		t.Fatalf("query must be the trailing arg: %v", r.gotArgs)
	}
	// --output must be a filename TEMPLATE under the output dir (a bare dir is
	// unreliable and spotDL falls back to its CWD).
	outVal := r.gotArgs[outIdx+1]
	if !strings.HasPrefix(outVal, "/tmp/music/") || !strings.Contains(outVal, "{output-ext}") {
		t.Fatalf("--output must be a template under the output dir, got %q", outVal)
	}
}

func TestStartTreatsAudioErrorAsFailure(t *testing.T) {
	// spotDL exits 0 but logs AudioProviderError when the audio download fails
	// (e.g. YouTube needs Deno). Start must surface that as an error so the job is
	// marked failed, not falsely "completed" with no file.
	r := &fakeRunner{lines: []string{
		`Processing query: A - T`,
		`AudioProviderError: YT-DLP download error - https://music.youtube.com/watch?v=x`,
	}}
	a := newAdapter(t, r)
	out, err := a.Start(context.Background(), core.DownloadRequest{Artist: "A", Title: "T"}, func(int) {})
	if err == nil {
		t.Fatalf("expected an error when spotDL logs AudioProviderError, got out=%q, nil err", out)
	}
	if out != "" {
		t.Fatalf("expected empty path on failure, got %q", out)
	}
}

func TestRedactArgsMasksSecret(t *testing.T) {
	got := redactArgs([]string{"--client-id", "id", "--client-secret", "supersecret", "download"})
	if strings.Contains(got, "supersecret") {
		t.Fatalf("secret leaked into log line: %q", got)
	}
	if !strings.Contains(got, "--client-secret ****") {
		t.Fatalf("secret not masked: %q", got)
	}
}

func TestSpotdlConformance(t *testing.T) {
	// Conformance Start must report progress + return an output path: feed a
	// runner that yields a progress line and a completion line.
	r := &fakeRunner{lines: []string{`Downloading "x": 50%`, `Downloaded: /tmp/music/x.mp3`}}
	a := newAdapter(t, r)
	download.RunConformance(t, a)
}
