// internal/coverage/canonical.go
package coverage

import (
	"regexp"
	"sort"
	"strings"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/matching"
)

var editionMarkers = []string{"deluxe", "remaster", "expanded", "anniversary", "explicit", "special edition"}

// editionParenRe matches a trailing parenthetical that contains an edition marker.
var editionParenRe = regexp.MustCompile(`\s*\([^)]*\)\s*$`)

// groupKey returns a stable dedup key: Kind + normalized title with trailing
// edition parentheticals stripped, so "Kid A", "Kid A (Deluxe Edition)", and
// "Kid A (Remastered)" all hash to the same bucket.
func groupKey(kind, name string) string {
	l := strings.ToLower(name)
	for _, m := range editionMarkers {
		if strings.Contains(l, m) {
			// Strip the trailing paren group and re-normalize.
			stripped := editionParenRe.ReplaceAllString(name, "")
			return kind + "\x00" + matching.Normalize(stripped)
		}
	}
	return kind + "\x00" + matching.Normalize(name)
}

// editionScore counts edition markers in a title (lower = closer to standard).
func editionScore(name string) int {
	l := strings.ToLower(name)
	n := 0
	for _, m := range editionMarkers {
		if strings.Contains(l, m) {
			n++
		}
	}
	return n
}

// Canonicalize collapses duplicate releases to one standard edition per title and
// returns the artist-page skeleton sorted Albums-first then newest-first.
func Canonicalize(albums []core.ExternalAlbum) []core.DiscographyAlbum {
	type group struct {
		best  core.ExternalAlbum
		score int
	}
	groups := map[string]*group{}
	for _, al := range albums {
		key := groupKey(al.Kind, al.Name)
		sc := editionScore(al.Name)
		g, ok := groups[key]
		if !ok {
			groups[key] = &group{best: al, score: sc}
			continue
		}
		// Prefer fewer markers; tie → earlier year; tie → more tracks.
		better := sc < g.score ||
			(sc == g.score && al.Year > 0 && (g.best.Year == 0 || al.Year < g.best.Year)) ||
			(sc == g.score && al.Year == g.best.Year && al.TotalTracks > g.best.TotalTracks)
		if better {
			g.best, g.score = al, sc
		}
	}
	out := make([]core.DiscographyAlbum, 0, len(groups))
	for _, g := range groups {
		out = append(out, core.DiscographyAlbum{
			Source: g.best.Source, ExternalID: g.best.ExternalID, Name: g.best.Name,
			CoverURL: g.best.CoverURL, Year: g.best.Year, Kind: g.best.Kind, TotalTracks: g.best.TotalTracks,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind == "album" // albums before singles
		}
		return out[i].Year > out[j].Year // newest first
	})
	return out
}
