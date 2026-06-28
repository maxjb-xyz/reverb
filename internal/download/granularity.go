package download

import "github.com/maxjb-xyz/reverb/internal/core"

// ResolveGranularityOrder maps each active granularity to its chain order for a
// single downloader instance.
//
// If cfg["granularities"] is a non-empty map[string]any, each key that is both a
// valid granularity constant AND present in supported is included; its value (JSON
// float64 or int) becomes the order. Keys that are invalid or not in supported are
// dropped. If the result is empty after filtering (e.g. the map had only bogus keys
// or only keys for granularities this plugin doesn't support), we fall back to the
// default below.
//
// Default: {g: priority} for every g in supported.
func ResolveGranularityOrder(
	cfg map[string]any,
	supported []core.DownloadGranularity,
	priority int,
) map[core.DownloadGranularity]int {
	// Build a set of valid granularity consts for fast membership checks.
	validGranularities := map[core.DownloadGranularity]struct{}{
		core.GranularityTrack: {},
		core.GranularityAlbum: {},
	}

	// Build a set of what this plugin actually supports.
	supportedSet := make(map[core.DownloadGranularity]struct{}, len(supported))
	for _, g := range supported {
		supportedSet[g] = struct{}{}
	}

	// Attempt config-driven resolution when granularities key is present and is a
	// non-empty map.
	if raw, ok := cfg["granularities"]; ok {
		if gMap, ok := raw.(map[string]any); ok && len(gMap) > 0 {
			result := make(map[core.DownloadGranularity]int, len(gMap))
			for k, v := range gMap {
				g := core.DownloadGranularity(k)
				// Drop keys that are not valid granularity constants.
				if _, valid := validGranularities[g]; !valid {
					continue
				}
				// Drop keys for granularities this plugin doesn't support.
				if _, inSupported := supportedSet[g]; !inSupported {
					continue
				}
				// JSON numbers arrive as float64; tolerate int too.
				switch n := v.(type) {
				case float64:
					result[g] = int(n)
				case int:
					result[g] = n
				}
			}
			// Only use the config-driven result if at least one entry survived filtering.
			if len(result) > 0 {
				return result
			}
		}
	}

	// Default: include all supported granularities at the instance priority.
	order := make(map[core.DownloadGranularity]int, len(supported))
	for _, g := range supported {
		order[g] = priority
	}
	return order
}
