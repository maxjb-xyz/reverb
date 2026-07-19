package download

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/matching"
)

// dedupSep is the unit-separator rune (␟) joining the normalized fields, matching
// the spec's dedup_key definition. It cannot appear in normalized text.
const dedupSep = "␟"

// DedupKey computes the deduplication key for a download request. An external
// catalog identity is authoritative whenever it exists: title/artist metadata
// often differs between a search result, coverage result, and retry. Requests
// without an external id retain the normalized metadata fallback.
func DedupKey(req core.DownloadRequest) string {
	var raw string
	if source, externalID := strings.ToLower(strings.TrimSpace(req.Source)), strings.TrimSpace(req.ExternalID); source != "" && externalID != "" {
		raw = "external" + dedupSep + source + dedupSep + externalID
	} else {
		raw = matching.Normalize(req.Artist) + dedupSep +
			matching.Normalize(req.Title) + dedupSep +
			matching.Normalize(req.Album)
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
