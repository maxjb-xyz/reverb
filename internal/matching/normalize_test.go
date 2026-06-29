package matching

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// fixtureFile is the shared corpus schema (also used by matching_test.go).
type fixtureFile struct {
	Cases []fixtureCase `json:"cases"`
}
type fixtureTrack struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Album      string `json:"album"`
	DurationMs int    `json:"durationMs"`
	ISRC       string `json:"isrc"`
	MBID       string `json:"mbid"`
}
type fixtureExpect struct {
	Status         string `json:"status"`
	LibraryTrackID string `json:"libraryTrackId"`
	Method         string `json:"method"`
}
type fixtureNormalize struct {
	Title  string `json:"title"`
	Artist string `json:"artist"`
}
type fixtureCase struct {
	Name      string            `json:"name"`
	External  fixtureTrack      `json:"external"`
	Library   []fixtureTrack    `json:"library"`
	Expect    fixtureExpect     `json:"expect"`
	Normalize *fixtureNormalize `json:"normalize"`
}

func loadFixtures(t *testing.T) []fixtureCase {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("testdata", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no fixtures found")
	}
	var all []fixtureCase
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		var ff fixtureFile
		if err := json.Unmarshal(b, &ff); err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		all = append(all, ff.Cases...)
	}
	return all
}

func TestNormalizeAgainstFixtures(t *testing.T) {
	for _, c := range loadFixtures(t) {
		if c.Normalize == nil {
			continue
		}
		if got := Normalize(c.External.Title); got != c.Normalize.Title {
			t.Errorf("%s: Normalize(title)=%q want %q", c.Name, got, c.Normalize.Title)
		}
		if got := Normalize(c.External.Artist); got != c.Normalize.Artist {
			t.Errorf("%s: Normalize(artist)=%q want %q", c.Name, got, c.Normalize.Artist)
		}
	}
}

// TestNormalizeQualifierForms documents how Normalize treats the three
// version-qualifier forms. Fingerprint relies on this contract: paren form
// keeps its parens, dash/bracket forms yield a bare token.
func TestNormalizeQualifierForms(t *testing.T) {
	// Paren form: parentheses are preserved by Normalize.
	if got := Normalize("Song (Live)"); got != "song (live)" {
		t.Errorf("paren form: got %q", got)
	}
	// Dash form: dash is stripped, yielding a bare "live" token.
	if got := Normalize("Song - Live"); got != "song live" {
		t.Errorf("dash form: got %q", got)
	}
	// Bracket form: brackets are stripped, yielding a bare "live" token.
	if got := Normalize("Song [Live]"); got != "song live" {
		t.Errorf("bracket form: got %q", got)
	}
}

func TestNormalizeEdgeCases(t *testing.T) {
	cases := map[string]string{
		"":                         "",
		"   ":                      "",
		"Hello,  World!!":          "hello world",
		"Salt & Sea":               "salt and sea",
		"Movement Pt. 1":           "movement part 1",
		"Sunrise (feat. Aluna)":    "sunrise",
		"Echoes ft. K":             "echoes",
		"Skyline featuring Mara":   "skyline",
		"Wanderer (Remaster 2011)": "wanderer (remaster 2011)",
		"Björk":                    "bjork",
		"Jóga":                     "joga",
		// Regression: feat/ft must not strip mid-word (word-boundary guard).
		"Daft Punk": "daft punk",
		"Drift":     "drift",
		"Gift":      "gift",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q)=%q want %q", in, got, want)
		}
	}
	// Cyrillic preserved (lowercased).
	if got := Normalize("Кукушка"); got != "кукушка" {
		t.Errorf("cyrillic: %q", got)
	}
	// Symmetry: feat group removed identically wherever it appears.
	if Normalize("Sunrise (feat. Aluna)") != Normalize("Sunrise") {
		t.Error("feat stripping not symmetric")
	}
}
