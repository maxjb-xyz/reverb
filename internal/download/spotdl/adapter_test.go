package spotdl

import (
	"context"
	"os"
	"path/filepath"
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
	// Use a non-Spotify source so the text fallback is exercised here; the Spotify
	// URL path is covered by TestStartUsesSpotifyTrackURLWhenAvailable.
	_, _ = a.Start(context.Background(), core.DownloadRequest{Source: "youtube", Artist: "Daft Punk", Title: "One More Time"}, func(int) {})
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

func TestStartStageProgress(t *testing.T) {
	// --simple-tui emits stage labels (no %); they must map to coarse progress so
	// the ring moves instead of sitting at 0.
	r := &fakeRunner{lines: []string{
		`Processing query: Bread Beatz - Alejandro`,
		`Bread Beatz - Alejandro: Downloading`,
		`Bread Beatz - Alejandro: Embedding metadata`,
		`Bread Beatz - Alejandro: Done`,
		`1/1 complete`,
	}}
	a := newAdapter(t, r)
	var seen []int
	out, err := a.Start(context.Background(), core.DownloadRequest{Artist: "Bread Beatz", Title: "Alejandro"}, func(p int) {
		seen = append(seen, p)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out == "" {
		t.Fatal("expected an output path")
	}
	has := func(v int) bool {
		for _, p := range seen {
			if p == v {
				return true
			}
		}
		return false
	}
	if !has(25) || !has(90) || !has(100) {
		t.Fatalf("expected stage progress to include 25/90/100, got %v", seen)
	}
}

func TestStartUsesSpotifyTrackURLWhenAvailable(t *testing.T) {
	// When Source=="spotify" and ExternalID is set, the trailing query arg must be
	// the Spotify track URL (far more reliable for obscure/classical titles).
	r := &fakeRunner{lines: []string{`Downloaded: ok`}}
	a := newAdapter(t, r)
	_, _ = a.Start(context.Background(), core.DownloadRequest{Source: "spotify", ExternalID: "abc123", Artist: "Ludovico Einaudi", Title: "Una mattina"}, func(int) {})
	n := len(r.gotArgs)
	if n == 0 {
		t.Fatal("no args captured")
	}
	wantURL := "https://open.spotify.com/track/abc123"
	if r.gotArgs[n-1] != wantURL {
		t.Fatalf("trailing query arg: got %q, want %q (Spotify URL path)", r.gotArgs[n-1], wantURL)
	}
}

func TestStartFallsBackToTextQueryForNonSpotify(t *testing.T) {
	// When Source!="spotify" (or ExternalID is empty), the trailing query arg must
	// be the "<artist> - <title>" text fallback.
	r := &fakeRunner{lines: []string{`Downloaded: ok`}}
	a := newAdapter(t, r)
	_, _ = a.Start(context.Background(), core.DownloadRequest{Source: "youtube", ExternalID: "yt123", Artist: "Daft Punk", Title: "One More Time"}, func(int) {})
	n := len(r.gotArgs)
	if n == 0 {
		t.Fatal("no args captured")
	}
	wantQuery := "Daft Punk - One More Time"
	if r.gotArgs[n-1] != wantQuery {
		t.Fatalf("trailing query arg: got %q, want %q (text fallback)", r.gotArgs[n-1], wantQuery)
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

func TestExplainFailure(t *testing.T) {
	cases := []struct {
		name       string
		raw        string
		wantReason []string // lowercased substrings the reason must contain
		wantHint   bool
	}{
		{
			name:       "yt-dlp download error",
			raw:        "AudioProviderError: YT-DLP download error - https://music.youtube.com/watch?v=x",
			wantReason: []string{"yt-dlp", "out of date"},
			wantHint:   true,
		},
		{
			name:       "bare audio provider error",
			raw:        "AudioProviderError: something went wrong",
			wantReason: []string{"yt-dlp"},
			wantHint:   true,
		},
		{
			name:       "lookup error is not found, not staleness",
			raw:        "LookupError: no results found for song: A - T",
			wantReason: []string{"not found"},
			wantHint:   false,
		},
		{
			name:       "unknown failure passes through unchanged",
			raw:        "DownloaderError: No space left on device",
			wantReason: []string{"no space left on device"},
			wantHint:   false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reason, hint := explainFailure(c.raw)
			low := strings.ToLower(reason)
			for _, want := range c.wantReason {
				if !strings.Contains(low, want) {
					t.Errorf("reason %q missing %q", reason, want)
				}
			}
			if c.wantHint && hint == "" {
				t.Error("expected an operator hint for a yt-dlp failure, got none")
			}
			if !c.wantHint && hint != "" {
				t.Errorf("expected no hint, got %q", hint)
			}
		})
	}
}

// TestStartSurfacesActionableYtDlpError asserts the error a failed download
// returns is the classified, actionable reason — not the terse "YT-DLP download
// error -" marker that --simple-tui emits.
func TestStartSurfacesActionableYtDlpError(t *testing.T) {
	r := &fakeRunner{lines: []string{
		`Processing query: A - T`,
		`AudioProviderError: YT-DLP download error - https://music.youtube.com/watch?v=x`,
	}}
	a := newAdapter(t, r)
	_, err := a.Start(context.Background(), core.DownloadRequest{Artist: "A", Title: "T"}, func(int) {})
	if err == nil {
		t.Fatal("expected an error")
	}
	low := strings.ToLower(err.Error())
	if !strings.Contains(low, "yt-dlp") || !strings.Contains(low, "out of date") {
		t.Fatalf("error should be actionable about stale yt-dlp, got: %v", err)
	}
}

func TestStartIncludesYouTubeFallbackProvider(t *testing.T) {
	// --audio youtube-music youtube must always be present so tracks absent from
	// YT-Music (obscure/regional/classical releases) can still be found on YouTube.
	r := &fakeRunner{lines: []string{`Downloaded: ok`}}
	a := newAdapter(t, r)
	_, _ = a.Start(context.Background(), core.DownloadRequest{Artist: "A", Title: "T"}, func(int) {})

	// Find the position of "--audio" and assert "youtube-music" and "youtube" follow
	// in that order, both before the "download" operation.
	audioIdx, ytMusicIdx, ytIdx, dlIdx := -1, -1, -1, -1
	for i, arg := range r.gotArgs {
		switch arg {
		case "--audio":
			audioIdx = i
		case "youtube-music":
			ytMusicIdx = i
		case "youtube":
			ytIdx = i
		case "download":
			dlIdx = i
		}
	}
	if audioIdx < 0 {
		t.Fatalf("--audio not found in args: %v", r.gotArgs)
	}
	if ytMusicIdx != audioIdx+1 {
		t.Fatalf("youtube-music must immediately follow --audio; args: %v", r.gotArgs)
	}
	if ytIdx != audioIdx+2 {
		t.Fatalf("youtube must follow youtube-music in --audio list; args: %v", r.gotArgs)
	}
	if dlIdx < 0 || audioIdx > dlIdx {
		t.Fatalf("--audio must precede the download operation; args: %v", r.gotArgs)
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

func TestStartManualURLWithSpotifyUsesPipeSyntax(t *testing.T) {
	// Case 1: Source=="spotify" + ExternalID + ManualURL → pipe syntax so Spotify
	// metadata is preserved while audio comes from the user-supplied URL.
	r := &fakeRunner{lines: []string{`Downloaded: ok`}}
	a := newAdapter(t, r)
	_, _ = a.Start(context.Background(), core.DownloadRequest{
		Source:     "spotify",
		ExternalID: "abc",
		Artist:     "Ludovico Einaudi",
		Title:      "Una mattina",
		ManualURL:  "https://youtube.com/watch?v=XYZ",
	}, func(int) {})
	n := len(r.gotArgs)
	if n == 0 {
		t.Fatal("no args captured")
	}
	// spotDL requires the order "<audio-url>|<spotify-url>" — spotify URL SECOND.
	// The ManualURL is normalized to a canonical single-video URL before piping.
	want := "https://www.youtube.com/watch?v=XYZ|https://open.spotify.com/track/abc"
	if r.gotArgs[n-1] != want {
		t.Fatalf("trailing query arg: got %q, want %q (pipe syntax)", r.gotArgs[n-1], want)
	}
}

func TestStartManualURLNonSpotifyIsDirectURL(t *testing.T) {
	// Case 2: ManualURL set but Source is not "spotify" → download directly from the
	// manual URL (no pipe, no Spotify lookup). The URL is normalized before use.
	r := &fakeRunner{lines: []string{`Downloaded: ok`}}
	a := newAdapter(t, r)
	manualURL := "https://youtube.com/watch?v=DIRECT"
	_, _ = a.Start(context.Background(), core.DownloadRequest{
		Source:    "youtube",
		Artist:    "Daft Punk",
		Title:     "One More Time",
		ManualURL: manualURL,
	}, func(int) {})
	n := len(r.gotArgs)
	if n == 0 {
		t.Fatal("no args captured")
	}
	// normalizeManualURL canonicalizes the YouTube URL to the www. form.
	wantURL := "https://www.youtube.com/watch?v=DIRECT"
	if r.gotArgs[n-1] != wantURL {
		t.Fatalf("trailing query arg: got %q, want %q (direct manual URL)", r.gotArgs[n-1], wantURL)
	}
}

func TestStartWithoutManualURLBehaviourUnchanged(t *testing.T) {
	// Case 3: no ManualURL → existing behaviour: Spotify URL when available, else
	// "<artist> - <title>" text search.
	r := &fakeRunner{lines: []string{`Downloaded: ok`}}
	a := newAdapter(t, r)
	// Spotify path.
	_, _ = a.Start(context.Background(), core.DownloadRequest{
		Source: "spotify", ExternalID: "abc123", Artist: "Einaudi", Title: "Una mattina",
	}, func(int) {})
	n := len(r.gotArgs)
	wantSpotify := "https://open.spotify.com/track/abc123"
	if r.gotArgs[n-1] != wantSpotify {
		t.Fatalf("no-ManualURL Spotify path: got %q, want %q", r.gotArgs[n-1], wantSpotify)
	}
	// Non-Spotify path.
	r2 := &fakeRunner{lines: []string{`Downloaded: ok`}}
	a2 := newAdapter(t, r2)
	_, _ = a2.Start(context.Background(), core.DownloadRequest{
		Source: "youtube", Artist: "Daft Punk", Title: "One More Time",
	}, func(int) {})
	n2 := len(r2.gotArgs)
	wantText := "Daft Punk - One More Time"
	if r2.gotArgs[n2-1] != wantText {
		t.Fatalf("no-ManualURL text path: got %q, want %q", r2.gotArgs[n2-1], wantText)
	}
}

func TestStartManualURLWithPipeCharIsStripped(t *testing.T) {
	// FIX 3: a "|" inside the user-supplied ManualURL must be stripped before the
	// pipe query is built; otherwise spotDL sees extra pipe tokens and misparses the
	// metadata|audio split. The resulting query must contain exactly ONE "|".
	r := &fakeRunner{lines: []string{`Downloaded: ok`}}
	a := newAdapter(t, r)
	_, _ = a.Start(context.Background(), core.DownloadRequest{
		Source:     "spotify",
		ExternalID: "abc",
		Artist:     "A",
		Title:      "T",
		ManualURL:  "https://youtube.com/watch?v=X|Y|Z", // two extra pipes
	}, func(int) {})
	n := len(r.gotArgs)
	if n == 0 {
		t.Fatal("no args captured")
	}
	query := r.gotArgs[n-1]
	pipes := strings.Count(query, "|")
	if pipes != 1 {
		t.Fatalf("FIX 3: pipe query must have exactly 1 '|' separator, got %d in %q", pipes, query)
	}
}

func TestSpotifyTargetURL(t *testing.T) {
	cases := []struct {
		name string
		req  core.DownloadRequest
		want string
	}{
		{
			name: "album granularity → album URL",
			req:  core.DownloadRequest{Source: "spotify", ExternalID: "ALB", Granularity: core.GranularityAlbum},
			want: "https://open.spotify.com/album/ALB",
		},
		{
			name: "track granularity → track URL",
			req:  core.DownloadRequest{Source: "spotify", ExternalID: "TRK", Granularity: core.GranularityTrack},
			want: "https://open.spotify.com/track/TRK",
		},
		{
			name: "empty granularity defaults to track URL",
			req:  core.DownloadRequest{Source: "spotify", ExternalID: "TRK"},
			want: "https://open.spotify.com/track/TRK",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := spotifyTargetURL(tc.req)
			if got != tc.want {
				t.Fatalf("spotifyTargetURL(%+v) = %q, want %q", tc.req, got, tc.want)
			}
		})
	}
}

func TestStartUsesAlbumURLForAlbumGranularity(t *testing.T) {
	// When Granularity==GranularityAlbum, the trailing query arg must be an album URL.
	r := &fakeRunner{lines: []string{`Downloaded: ok`}}
	a := newAdapter(t, r)
	_, _ = a.Start(context.Background(), core.DownloadRequest{
		Source:      "spotify",
		ExternalID:  "ALB123",
		Artist:      "The Beatles",
		Title:       "Abbey Road",
		Granularity: core.GranularityAlbum,
	}, func(int) {})
	n := len(r.gotArgs)
	if n == 0 {
		t.Fatal("no args captured")
	}
	want := "https://open.spotify.com/album/ALB123"
	if r.gotArgs[n-1] != want {
		t.Fatalf("album granularity: trailing query arg = %q, want %q", r.gotArgs[n-1], want)
	}
}

func TestSupportedGranularitiesTrackAndAlbum(t *testing.T) {
	a := New()
	gs := a.SupportedGranularities()
	if len(gs) != 2 {
		t.Fatalf("SupportedGranularities() = %v, want [track album]", gs)
	}
	has := func(want core.DownloadGranularity) bool {
		for _, g := range gs {
			if g == want {
				return true
			}
		}
		return false
	}
	if !has(core.GranularityTrack) {
		t.Fatalf("SupportedGranularities() missing track, got %v", gs)
	}
	if !has(core.GranularityAlbum) {
		t.Fatalf("SupportedGranularities() missing album, got %v", gs)
	}
}

func TestSpotdlConformance(t *testing.T) {
	// Conformance Start must report progress + return an output path: feed a
	// runner that yields a progress line and a completion line.
	r := &fakeRunner{lines: []string{`Downloading "x": 50%`, `Downloaded: /tmp/music/x.mp3`}}
	a := newAdapter(t, r)
	download.RunConformance(t, a)
}

func TestNormalizeManualURL(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "radio URL stripped to single video",
			input: "https://www.youtube.com/watch?v=jh5u0wmau54&list=RDjh5u0wmau54&start_radio=1&pp=ygUKaGlwIGhvcCB1cw%3D%3D",
			want:  "https://www.youtube.com/watch?v=jh5u0wmau54",
		},
		{
			name:  "youtu.be short URL expanded",
			input: "https://youtu.be/abc123",
			want:  "https://www.youtube.com/watch?v=abc123",
		},
		{
			name:  "already clean YouTube URL unchanged",
			input: "https://www.youtube.com/watch?v=xyz789",
			want:  "https://www.youtube.com/watch?v=xyz789",
		},
		{
			name:  "SoundCloud URL returned unchanged",
			input: "https://soundcloud.com/artist/track",
			want:  "https://soundcloud.com/artist/track",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeManualURL(tc.input)
			if got != tc.want {
				t.Fatalf("normalizeManualURL(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestNormalizeAppliedInStart(t *testing.T) {
	// Integration: verifies that normalizeManualURL is applied inside Start before
	// the pipe query is assembled. A radio URL must arrive at spotDL as a clean
	// single-video URL (list/start_radio params stripped).
	r := &fakeRunner{lines: []string{`Downloaded: ok`}}
	a := newAdapter(t, r)
	_, _ = a.Start(context.Background(), core.DownloadRequest{
		Source:     "spotify",
		ExternalID: "abc123track",
		Artist:     "A",
		Title:      "T",
		ManualURL:  "https://www.youtube.com/watch?v=jh5u0wmau54&list=RDjh5u0wmau54&start_radio=1",
	}, func(int) {})
	n := len(r.gotArgs)
	if n == 0 {
		t.Fatal("no args captured")
	}
	want := "https://www.youtube.com/watch?v=jh5u0wmau54|https://open.spotify.com/track/abc123track"
	if r.gotArgs[n-1] != want {
		t.Fatalf("trailing query arg: got %q, want %q", r.gotArgs[n-1], want)
	}
}

func TestEnsureSpotdlTempDirCreatesDir(t *testing.T) {
	// Redirect the config dir cross-platform: Linux honors XDG_CONFIG_HOME, macOS
	// derives os.UserConfigDir() from HOME (~/Library/Application Support). Set both
	// and compute the expected path from the SAME os.UserConfigDir() the code uses.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	ensureSpotdlTempDir()
	cfg, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir: %v", err)
	}
	want := filepath.Join(cfg, "spotdl", "temp")
	if fi, statErr := os.Stat(want); statErr != nil || !fi.IsDir() {
		t.Fatalf("expected %s to exist as a dir; err=%v", want, statErr)
	}
	// Idempotent + concurrency-safe: a second call must not error or panic.
	ensureSpotdlTempDir()
}
