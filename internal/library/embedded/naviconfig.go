package embedded

import (
	"os"
	"path/filepath"
	"strconv"
)

// NaviOptions are the inputs needed to launch the bundled Navidrome.
type NaviOptions struct {
	MusicDir      string
	DataDir       string // Navidrome's own data/DB dir
	Address       string
	Port          int
	AdminPassword string
	ScanSchedule  string
}

// DefaultNaviOptions returns localhost-bound options with Navidrome's data dir
// nested under Reverb's data dir.
func DefaultNaviOptions(reverbDataDir, musicDir, adminPassword string) NaviOptions {
	return NaviOptions{
		MusicDir:      musicDir,
		DataDir:       filepath.Join(reverbDataDir, "navidrome"),
		Address:       "127.0.0.1",
		Port:          4533,
		AdminPassword: adminPassword,
		ScanSchedule:  "@every 1h",
	}
}

// BuildNavidromeEnv renders the ND_* environment for the child process. The
// process inherits the parent env plus these (later entries win in os/exec).
func BuildNavidromeEnv(o NaviOptions) []string {
	env := append([]string{}, os.Environ()...)
	return append(env,
		"ND_MUSICFOLDER="+o.MusicDir,
		"ND_DATAFOLDER="+o.DataDir,
		"ND_ADDRESS="+o.Address,
		"ND_PORT="+strconv.Itoa(o.Port),
		"ND_DEVAUTOCREATEADMINPASSWORD="+o.AdminPassword,
		"ND_SCANSCHEDULE="+o.ScanSchedule,
		// Navidrome synthesizes Subsonic `path` fields by default (a privacy
		// default for internet-facing servers). The bundled instance is
		// loopback-only and shares Reverb's filesystem, and the waveform-peaks
		// endpoint stats the getSong path on disk — report the real path.
		"ND_SUBSONIC_DEFAULTREPORTREALPATH=true",
	)
}

// MusicDir resolves the music folder (shared with the download output dir).
func MusicDir(getenv func(string) string) string {
	if d := getenv("REVERB_DOWNLOAD_DIR"); d != "" {
		return d
	}
	return "/music"
}
