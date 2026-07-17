package matching

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
)

// versionMarkers are recording qualifiers that distinguish versions of the same
// song. They are extracted from the RAW title (before Normalize) so that paren,
// dash, and bracket forms — "(Live)", "- Live", "[Live]" — all converge.
var versionMarkers = []string{
	"live", "acoustic", "remaster", "remastered", "deluxe",
	"edit", "remix", "demo", "instrumental", "radio",
}

// Fingerprint returns a stable, backend-independent identity key for a track:
//
//	sha256( normArtist ␟ normTitleBase ␟ normAlbum ␟ durationBucket ␟ sortedMarkers )
//
// It reuses Normalize so it can never disagree with the matcher's own identity
// call. Version qualifiers are extracted from the raw title (before Normalize)
// so that paren/dash/bracket forms converge to the same fingerprint.
func Fingerprint(title, artist, album string, durationMs int) string {
	// Extract markers from the raw title so all three qualifier forms are seen
	// identically: "Song (Live)", "Song - Live", "Song [Live]" all yield ["live"].
	markers := extractMarkersRaw(title)

	// Build the normalised title base: Normalize first, then strip any marker
	// tokens that survived (including paren-wrapped forms like "(live)").
	// Padding with spaces handles start/end-of-string boundary cases cleanly.
	nt := " " + Normalize(title) + " "
	for _, m := range markers {
		// Remove bare token (space-bounded), e.g. " live " → " "
		nt = strings.ReplaceAll(nt, " "+m+" ", " ")
		// Remove paren form, e.g. "(live)" — may appear mid-string
		nt = strings.ReplaceAll(nt, "("+m+")", "")
	}
	nt = strings.Join(strings.Fields(nt), " ")

	bucket := durationMs / 5000

	var b strings.Builder
	// Key on the PRIMARY artist so external metadata ("Egzod") and a library tag
	// carrying the full credit ("Egzod; Maestro Chives; Neoni") converge on one
	// identity. PrimaryArtist only splits unambiguous separators, so names like
	// AC/DC — and therefore their persisted fingerprints — are unchanged.
	b.WriteString(Normalize(PrimaryArtist(artist)))
	b.WriteByte('\x1f')
	b.WriteString(nt)
	b.WriteByte('\x1f')
	b.WriteString(Normalize(album))
	b.WriteByte('\x1f')
	b.WriteString(strconv.Itoa(bucket))
	b.WriteByte('\x1f')
	b.WriteString(strings.Join(markers, ","))

	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// extractMarkersRaw finds version qualifier words in the raw (un-normalized)
// title by lowercasing and doing a case-insensitive word-boundary search.
// Extracting from the raw title ensures "(Live)", "- Live", and "[Live]" all
// yield the same marker set before any paren/bracket stripping by Normalize.
func extractMarkersRaw(title string) []string {
	lower := strings.ToLower(title)
	found := map[string]bool{}
	for _, m := range versionMarkers {
		// Check if the marker appears as a whole word (surrounded by non-alpha or string boundaries).
		if containsWord(lower, m) {
			found[m] = true
		}
	}
	out := make([]string, 0, len(found))
	for m := range found {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// containsWord reports whether word appears as a whole word in s (case-insensitive
// via the lowercase-both contract of the caller).
func containsWord(s, word string) bool {
	idx := 0
	for {
		pos := strings.Index(s[idx:], word)
		if pos < 0 {
			return false
		}
		pos += idx
		before := pos == 0 || !isAlpha(rune(s[pos-1]))
		after := pos+len(word) >= len(s) || !isAlpha(rune(s[pos+len(word)]))
		if before && after {
			return true
		}
		idx = pos + 1
	}
}

func isAlpha(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}
