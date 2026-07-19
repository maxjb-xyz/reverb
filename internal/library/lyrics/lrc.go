// Package lyrics resolves and parses song lyrics: local .lrc sidecars and
// embedded tags first, then LRCLIB.net, cached in the store.
package lyrics

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type Line struct {
	TimeMs int    `json:"timeMs"`
	Text   string `json:"text"`
}

// Lyrics is the parsed result. Exactly one of Lines/Plain is meaningful.
type Lyrics struct {
	Synced bool
	Lines  []Line // sorted by TimeMs when Synced
	Plain  string // trimmed raw text when !Synced
}

var (
	// [mm:ss], [mm:ss.xx], [mm:ss.xxx], [mm:ss:xx] — one or more per line.
	stampRe = regexp.MustCompile(`^\[(\d+):(\d{1,2})(?:[.:](\d{1,3}))?\]`)
	// <mm:ss.xx> word-level stamps from enhanced LRC.
	wordStampRe = regexp.MustCompile(`<\d+:\d{1,2}(?:[.:]\d{1,3})?>\s?`)
	// [ar:...] / [ti:...] etc — a tag whose id starts with a letter.
	metaRe = regexp.MustCompile(`^\[[a-zA-Z][^\]]*\]\s*$`)
)

// Parse never fails: input that isn't valid LRC comes back as plain lyrics.
func Parse(raw string) Lyrics {
	var lines []Line
	for _, ln := range strings.Split(raw, "\n") {
		ln = strings.TrimRight(ln, "\r")
		if metaRe.MatchString(strings.TrimSpace(ln)) {
			continue
		}
		var stamps []int
		rest := strings.TrimSpace(ln)
		for {
			m := stampRe.FindStringSubmatch(rest)
			if m == nil {
				break
			}
			min, _ := strconv.Atoi(m[1])
			sec, _ := strconv.Atoi(m[2])
			frac := 0
			if m[3] != "" {
				f, _ := strconv.Atoi(m[3])
				switch len(m[3]) {
				case 1:
					frac = f * 100
				case 2:
					frac = f * 10
				default:
					frac = f
				}
			}
			stamps = append(stamps, min*60_000+sec*1000+frac)
			rest = rest[len(m[0]):]
		}
		if len(stamps) == 0 {
			continue
		}
		text := strings.TrimSpace(wordStampRe.ReplaceAllString(rest, ""))
		for _, ms := range stamps {
			lines = append(lines, Line{TimeMs: ms, Text: text})
		}
	}
	if len(lines) > 0 {
		sort.SliceStable(lines, func(i, j int) bool { return lines[i].TimeMs < lines[j].TimeMs })
		return Lyrics{Synced: true, Lines: lines}
	}
	return Lyrics{Plain: strings.TrimSpace(raw)}
}
