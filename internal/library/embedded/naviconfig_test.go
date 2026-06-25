package embedded

import (
	"strings"
	"testing"
)

func envMap(entries []string) map[string]string {
	m := map[string]string{}
	for _, e := range entries {
		if i := strings.IndexByte(e, '='); i >= 0 {
			m[e[:i]] = e[i+1:]
		}
	}
	return m
}

func TestBuildNavidromeEnv_LocalhostAndCreds(t *testing.T) {
	o := DefaultNaviOptions("/data", "/music", "s3cret")
	m := envMap(BuildNavidromeEnv(o))

	if m["ND_ADDRESS"] != "127.0.0.1" {
		t.Errorf("ND_ADDRESS = %q, want 127.0.0.1 (localhost-only)", m["ND_ADDRESS"])
	}
	if m["ND_PORT"] != "4533" {
		t.Errorf("ND_PORT = %q, want 4533", m["ND_PORT"])
	}
	if m["ND_MUSICFOLDER"] != "/music" {
		t.Errorf("ND_MUSICFOLDER = %q", m["ND_MUSICFOLDER"])
	}
	if m["ND_DATAFOLDER"] != "/data/navidrome" {
		t.Errorf("ND_DATAFOLDER = %q, want /data/navidrome", m["ND_DATAFOLDER"])
	}
	if m["ND_DEVAUTOCREATEADMINPASSWORD"] != "s3cret" {
		t.Errorf("ND_DEVAUTOCREATEADMINPASSWORD = %q", m["ND_DEVAUTOCREATEADMINPASSWORD"])
	}
}

func TestMusicDir_DefaultsToMusic(t *testing.T) {
	if got := MusicDir(func(string) string { return "" }); got != "/music" {
		t.Errorf("MusicDir default = %q, want /music", got)
	}
	if got := MusicDir(func(k string) string {
		if k == "REVERB_DOWNLOAD_DIR" {
			return "/songs"
		}
		return ""
	}); got != "/songs" {
		t.Errorf("MusicDir override = %q, want /songs", got)
	}
}
