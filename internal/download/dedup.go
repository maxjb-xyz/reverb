package download

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/maxjb-xyz/crate/internal/core"
	"github.com/maxjb-xyz/crate/internal/matching"
)

// dedupSep is the unit-separator rune (␟) joining the normalized fields, matching
// the spec's dedup_key definition. It cannot appear in normalized text.
const dedupSep = "␟"

// DedupKey computes the deduplication key for a download request. It is
// sha256(Normalize(artist)+␟+Normalize(title)+␟+Normalize(album)), hex-encoded.
// It REUSES matching.Normalize so the dedup key and the matcher can never drift.
func DedupKey(req core.DownloadRequest) string {
	raw := matching.Normalize(req.Artist) + dedupSep +
		matching.Normalize(req.Title) + dedupSep +
		matching.Normalize(req.Album)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
