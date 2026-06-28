package wiring

import (
	"testing"

	"github.com/maxjb-xyz/reverb/internal/core"
)

// Tests for resolveGranularityOrder.
// Written RED-first: these tests were written before the implementation exists.

func TestResolveGranularityOrder_BothConfigured(t *testing.T) {
	cfg := map[string]any{
		"granularities": map[string]any{
			"track": float64(0),
			"album": float64(2),
		},
	}
	supported := []core.DownloadGranularity{core.GranularityTrack, core.GranularityAlbum}
	got := resolveGranularityOrder(cfg, supported, 5)
	want := map[core.DownloadGranularity]int{
		core.GranularityTrack: 0,
		core.GranularityAlbum: 2,
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d; got %v", len(got), len(want), got)
	}
	for g, v := range want {
		if got[g] != v {
			t.Errorf("order[%q] = %d, want %d", g, got[g], v)
		}
	}
}

func TestResolveGranularityOrder_EmptyCfg_DefaultsToAllSupported(t *testing.T) {
	cfg := map[string]any{}
	supported := []core.DownloadGranularity{core.GranularityTrack, core.GranularityAlbum}
	got := resolveGranularityOrder(cfg, supported, 5)
	want := map[core.DownloadGranularity]int{
		core.GranularityTrack: 5,
		core.GranularityAlbum: 5,
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d; got %v", len(got), len(want), got)
	}
	for g, v := range want {
		if got[g] != v {
			t.Errorf("order[%q] = %d, want %d", g, got[g], v)
		}
	}
}

func TestResolveGranularityOrder_OnlyTrackConfigured_AlbumDropped(t *testing.T) {
	// Only track is in granularities → album NOT enabled (not just lower priority, absent).
	cfg := map[string]any{
		"granularities": map[string]any{
			"track": float64(0),
		},
	}
	supported := []core.DownloadGranularity{core.GranularityTrack, core.GranularityAlbum}
	got := resolveGranularityOrder(cfg, supported, 5)
	if len(got) != 1 {
		t.Fatalf("want 1 entry (track only), got %d: %v", len(got), got)
	}
	if v, ok := got[core.GranularityTrack]; !ok || v != 0 {
		t.Errorf("order[track] = %d ok=%v, want 0 true", v, ok)
	}
	if _, ok := got[core.GranularityAlbum]; ok {
		t.Error("album must not be present when not in granularities config")
	}
}

func TestResolveGranularityOrder_BogusKeyOnly_FallsBackToDefault(t *testing.T) {
	// All keys invalid → result after filtering is empty → must fall back to default.
	cfg := map[string]any{
		"granularities": map[string]any{
			"bogus": float64(0),
		},
	}
	supported := []core.DownloadGranularity{core.GranularityTrack}
	got := resolveGranularityOrder(cfg, supported, 3)
	want := map[core.DownloadGranularity]int{core.GranularityTrack: 3}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d; got %v", len(got), len(want), got)
	}
	if got[core.GranularityTrack] != 3 {
		t.Errorf("order[track] = %d, want 3", got[core.GranularityTrack])
	}
}

func TestResolveGranularityOrder_UnsupportedKeyDropped_FallsBackToDefault(t *testing.T) {
	// cfg includes "track" but the downloader only supports "album".
	// After filtering out "track" (not in supported), result is empty → fall back.
	cfg := map[string]any{
		"granularities": map[string]any{
			"track": float64(0),
		},
	}
	supported := []core.DownloadGranularity{core.GranularityAlbum}
	priority := 7
	got := resolveGranularityOrder(cfg, supported, priority)
	want := map[core.DownloadGranularity]int{core.GranularityAlbum: 7}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d; got %v", len(got), len(want), got)
	}
	if got[core.GranularityAlbum] != 7 {
		t.Errorf("order[album] = %d, want 7", got[core.GranularityAlbum])
	}
}
