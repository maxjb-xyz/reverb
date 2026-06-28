package wiring

import (
	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/download"
)

// resolveGranularityOrder is the package-local alias kept for backward
// compatibility within wiring. It delegates to download.ResolveGranularityOrder
// so the logic lives in one place and is accessible from both wiring and api.
func resolveGranularityOrder(
	cfg map[string]any,
	supported []core.DownloadGranularity,
	priority int,
) map[core.DownloadGranularity]int {
	return download.ResolveGranularityOrder(cfg, supported, priority)
}
