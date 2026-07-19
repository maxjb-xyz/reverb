package lyrics

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ReadLocal returns raw lyrics text found beside or inside the audio file.
// source is "sidecar" or "tags" when ok. ffprobePath "" defaults to "ffprobe";
// a missing ffprobe binary just skips the tag step.
func ReadLocal(ctx context.Context, ffprobePath, audioPath string) (raw, source string, ok bool) {
	// 1. .lrc sidecar with the same basename.
	base := strings.TrimSuffix(audioPath, extOf(audioPath))
	if b, err := os.ReadFile(base + ".lrc"); err == nil && len(strings.TrimSpace(string(b))) > 0 {
		return string(b), "sidecar", true
	}
	// 2. Embedded tags via ffprobe.
	if ffprobePath == "" {
		ffprobePath = "ffprobe"
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, ffprobePath,
		"-v", "quiet", "-print_format", "json", "-show_format", audioPath).Output()
	if err != nil {
		return "", "", false
	}
	if v := extractLyricsTag(out); v != "" {
		return v, "tags", true
	}
	return "", "", false
}

func extOf(p string) string {
	if i := strings.LastIndexByte(p, '.'); i >= 0 && !strings.ContainsRune(p[i:], '/') {
		return p[i:]
	}
	return ""
}

// extractLyricsTag pulls the first lyrics-ish tag out of ffprobe -show_format
// JSON. ID3 USLT frames surface as "lyrics-<lang>"; Vorbis/FLAC use LYRICS or
// UNSYNCEDLYRICS.
func extractLyricsTag(probeJSON []byte) string {
	var doc struct {
		Format struct {
			Tags map[string]string `json:"tags"`
		} `json:"format"`
	}
	if err := json.Unmarshal(probeJSON, &doc); err != nil {
		return ""
	}
	for k, v := range doc.Format.Tags {
		lk := strings.ToLower(k)
		if (lk == "lyrics" || lk == "unsyncedlyrics" || strings.HasPrefix(lk, "lyrics-")) &&
			strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
