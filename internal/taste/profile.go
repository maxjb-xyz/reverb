// Package taste turns what a user does (finishing songs, skipping them, adding
// them to playlists, downloading them, thumbing them down) into per-user scores
// for tracks and artists. It is the signal layer the recommendation engine
// ranks with. Scoring is a pure function of the stored events (Build), so a
// smarter model can replace it later without touching how events are recorded.
package taste

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/maxjb-xyz/reverb/internal/matching"
)

// Listen outcomes.
const (
	OutcomeCompleted = "completed"
	OutcomeSkipEarly = "skip_early"
	OutcomeSkipLate  = "skip_late"
	OutcomeStopped   = "stopped"
)

// Signal kinds.
const (
	SignalPlaylistAdd = "playlist_add"
	SignalDownload    = "download"
	SignalRequest     = "request"
)

// Feedback targets.
const (
	TargetTrack  = "track"
	TargetArtist = "artist"
)

// EarlySkipMs is where a skip stops being "not this one" and becomes "heard
// enough of it": the same 30 s line the Last.fm scrobble rules use.
const EarlySkipMs = 30_000

// completeWithinMs: a track that ended within this much of its length counts as
// finished even if the client reported a skip (the user hit next on the outro).
const completeWithinMs = 3_000

// AutoContexts are the play contexts where Reverb, not the user, chose the
// track. An early skip there is a clear "not for me", so it costs more.
var AutoContexts = map[string]bool{"radio": true, "dj": true, "mix": true, "discover": true}

// Weights tune Build. Exposed so tests and a future tuning UI can see them.
type Weights struct {
	Completed       float64
	HeardMost       float64 // stopped/late-skipped after hearing >= HeardMostFrac
	HeardMostFrac   float64
	LateSkipLittle  float64 // late skip before HeardMostFrac
	EarlySkipUser   float64
	EarlySkipAuto   float64
	LegacyCompleted float64
	LegacyPlay      float64
	PlaylistAdd     float64
	Download        float64
	Request         float64
	HalfLife        time.Duration
}

// DefaultWeights is the shipped tuning.
var DefaultWeights = Weights{
	Completed:       1.0,
	HeardMost:       0.5,
	HeardMostFrac:   0.6,
	LateSkipLittle:  -0.3,
	EarlySkipUser:   -0.4,
	EarlySkipAuto:   -1.0,
	LegacyCompleted: 1.0,
	LegacyPlay:      0.7,
	PlaylistAdd:     3.0,
	Download:        2.0,
	Request:         2.0,
	HalfLife:        180 * 24 * time.Hour,
}

// Listen is one stored listen event (input to Build).
type Listen struct {
	CatalogID      string
	LibraryTrackID string
	Title          string
	Artist         string
	ArtistKey      string
	StartedAt      int64
	MsPlayed       int64
	DurationMs     int64
	Outcome        string
	ContextKind    string
	ContextID      string
}

// Play is a legacy qualified play (recorded before the listen log existed).
type Play struct {
	CatalogID string
	Title     string
	Artist    string
	PlayedAt  int64
	Completed bool
}

// Signal is a stored taste signal. CatalogID empty means artist-level.
type Signal struct {
	Kind      string
	CatalogID string
	Title     string
	Artist    string
	ArtistKey string
	CreatedAt int64
}

// Feedback is a stored thumbs-down.
type Feedback struct {
	TargetKind string `json:"targetKind"`
	TargetKey  string `json:"targetKey"`
	Label      string `json:"label"`
	Sublabel   string `json:"sublabel,omitempty"`
	LibraryID  string `json:"libraryId,omitempty"`
	CreatedAt  int64  `json:"createdAt"`
}

// Inputs bundles everything Build scores.
type Inputs struct {
	Listens  []Listen
	Plays    []Play
	Signals  []Signal
	Feedback []Feedback
}

// TrackTaste is one track's score and the counts behind it.
type TrackTaste struct {
	CatalogID      string  `json:"catalogId"`
	LibraryTrackID string  `json:"libraryTrackId,omitempty"`
	Title          string  `json:"title"`
	Artist         string  `json:"artist"`
	ArtistKey      string  `json:"artistKey"`
	Score          float64 `json:"score"`
	Listens        int     `json:"listens"`
	Completions    int     `json:"completions"`
	EarlySkips     int     `json:"earlySkips"`
	LateSkips      int     `json:"lateSkips"`
	LastListened   int64   `json:"lastListened"`
}

// ArtistTaste is one artist's score and the counts behind it.
type ArtistTaste struct {
	Key          string  `json:"key"`
	Name         string  `json:"name"`
	Score        float64 `json:"score"`
	Listens      int     `json:"listens"`
	Completions  int     `json:"completions"`
	EarlySkips   int     `json:"earlySkips"`
	LastListened int64   `json:"lastListened"`
}

// Profile is one user's computed taste.
type Profile struct {
	UserID        string
	ComputedAt    time.Time
	Tracks        map[string]*TrackTaste
	Artists       map[string]*ArtistTaste
	HiddenTracks  map[string]bool
	HiddenArtists map[string]bool
}

// ArtistKey is the identity artists are scored under: the normalized primary
// artist, so "Egzod; Maestro Chives" and "egzod" land on the same key and a
// library artist matches the same name from Deezer.
func ArtistKey(artist string) string {
	a := strings.TrimSpace(artist)
	if a == "" {
		return ""
	}
	return matching.Normalize(matching.PrimaryArtist(a))
}

// NormalizeOutcome validates a client-reported outcome, deriving one from the
// timing when it is empty, and upgrading a "skip" that happened on the outro to
// completed.
func NormalizeOutcome(outcome string, msPlayed, durationMs int) (string, bool) {
	switch outcome {
	case OutcomeCompleted, OutcomeStopped:
		return outcome, true
	case OutcomeSkipEarly, OutcomeSkipLate, "":
	default:
		return "", false
	}
	if durationMs > 0 && msPlayed >= durationMs-completeWithinMs {
		return OutcomeCompleted, true
	}
	if outcome != "" {
		return outcome, true
	}
	if msPlayed < EarlySkipMs {
		return OutcomeSkipEarly, true
	}
	return OutcomeSkipLate, true
}

// Build scores a user's taste from their stored events. Pure: same inputs and
// clock always give the same profile.
func Build(userID string, at time.Time, in Inputs) *Profile {
	return BuildWith(userID, at, in, DefaultWeights)
}

// BuildWith is Build with explicit weights.
func BuildWith(userID string, at time.Time, in Inputs, w Weights) *Profile {
	p := &Profile{
		UserID:        userID,
		ComputedAt:    at,
		Tracks:        map[string]*TrackTaste{},
		Artists:       map[string]*ArtistTaste{},
		HiddenTracks:  map[string]bool{},
		HiddenArtists: map[string]bool{},
	}
	decay := func(ts int64) float64 {
		age := at.Sub(time.Unix(ts, 0))
		if age <= 0 || w.HalfLife <= 0 {
			return 1
		}
		return math.Pow(0.5, float64(age)/float64(w.HalfLife))
	}
	artist := func(key, name string) *ArtistTaste {
		if key == "" {
			return nil
		}
		a := p.Artists[key]
		if a == nil {
			a = &ArtistTaste{Key: key, Name: strings.TrimSpace(name)}
			p.Artists[key] = a
		} else if a.Name == "" {
			a.Name = strings.TrimSpace(name)
		}
		return a
	}
	track := func(id, title, artistName, libID string) *TrackTaste {
		t := p.Tracks[id]
		if t == nil {
			t = &TrackTaste{CatalogID: id, Title: title, Artist: artistName, ArtistKey: ArtistKey(artistName)}
			p.Tracks[id] = t
		}
		if libID != "" {
			t.LibraryTrackID = libID
		}
		return t
	}
	add := func(t *TrackTaste, a *ArtistTaste, v float64) {
		if t != nil {
			t.Score += v
		}
		if a != nil {
			a.Score += v
		}
	}

	for _, l := range in.Listens {
		key := l.ArtistKey
		if key == "" {
			key = ArtistKey(l.Artist)
		}
		t := track(l.CatalogID, l.Title, l.Artist, l.LibraryTrackID)
		a := artist(key, matching.PrimaryArtist(l.Artist))
		frac := 0.0
		if l.DurationMs > 0 {
			frac = float64(l.MsPlayed) / float64(l.DurationMs)
		}
		var v float64
		switch l.Outcome {
		case OutcomeCompleted:
			v = w.Completed
			t.Completions++
			if a != nil {
				a.Completions++
			}
		case OutcomeStopped:
			if frac >= w.HeardMostFrac {
				v = w.HeardMost
			}
		case OutcomeSkipLate:
			t.LateSkips++
			if frac >= w.HeardMostFrac {
				v = w.HeardMost
			} else {
				v = w.LateSkipLittle
			}
		case OutcomeSkipEarly:
			t.EarlySkips++
			if a != nil {
				a.EarlySkips++
			}
			if AutoContexts[l.ContextKind] {
				v = w.EarlySkipAuto
			} else {
				v = w.EarlySkipUser
			}
		}
		t.Listens++
		if l.StartedAt > t.LastListened {
			t.LastListened = l.StartedAt
		}
		if a != nil {
			a.Listens++
			if l.StartedAt > a.LastListened {
				a.LastListened = l.StartedAt
			}
		}
		add(t, a, v*decay(l.StartedAt))
	}

	for _, pl := range in.Plays {
		t := track(pl.CatalogID, pl.Title, pl.Artist, "")
		a := artist(ArtistKey(pl.Artist), matching.PrimaryArtist(pl.Artist))
		v := w.LegacyPlay
		if pl.Completed {
			v = w.LegacyCompleted
			t.Completions++
			if a != nil {
				a.Completions++
			}
		}
		t.Listens++
		if pl.PlayedAt > t.LastListened {
			t.LastListened = pl.PlayedAt
		}
		if a != nil {
			a.Listens++
			if pl.PlayedAt > a.LastListened {
				a.LastListened = pl.PlayedAt
			}
		}
		add(t, a, v*decay(pl.PlayedAt))
	}

	for _, s := range in.Signals {
		var v float64
		switch s.Kind {
		case SignalPlaylistAdd:
			v = w.PlaylistAdd
		case SignalDownload:
			v = w.Download
		case SignalRequest:
			v = w.Request
		default:
			continue
		}
		v *= decay(s.CreatedAt)
		key := s.ArtistKey
		if key == "" {
			key = ArtistKey(s.Artist)
		}
		a := artist(key, matching.PrimaryArtist(s.Artist))
		var t *TrackTaste
		if s.CatalogID != "" {
			t = track(s.CatalogID, s.Title, s.Artist, "")
		}
		add(t, a, v)
	}

	for _, f := range in.Feedback {
		switch f.TargetKind {
		case TargetTrack:
			p.HiddenTracks[f.TargetKey] = true
		case TargetArtist:
			p.HiddenArtists[f.TargetKey] = true
		}
	}
	return p
}

// TrackScore is the track's score (0 when unknown).
func (p *Profile) TrackScore(catalogID string) float64 {
	if t := p.Tracks[catalogID]; t != nil {
		return t.Score
	}
	return 0
}

// ArtistScore is the artist's score (0 when unknown).
func (p *Profile) ArtistScore(key string) float64 {
	if a := p.Artists[key]; a != nil {
		return a.Score
	}
	return 0
}

// IsHidden reports whether the user thumbed down this track or its artist.
// Either argument may be empty.
func (p *Profile) IsHidden(catalogID, artistKey string) bool {
	return (catalogID != "" && p.HiddenTracks[catalogID]) || (artistKey != "" && p.HiddenArtists[artistKey])
}

// TopArtists returns the n highest-scoring positive artists, hidden ones excluded.
func (p *Profile) TopArtists(n int) []ArtistTaste {
	out := make([]ArtistTaste, 0, len(p.Artists))
	for _, a := range p.Artists {
		if a.Score > 0 && !p.HiddenArtists[a.Key] {
			out = append(out, *a)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Key < out[j].Key
	})
	if n >= 0 && len(out) > n {
		out = out[:n]
	}
	return out
}

// TopTracks returns the n highest-scoring positive tracks, hidden ones excluded.
func (p *Profile) TopTracks(n int) []TrackTaste {
	out := make([]TrackTaste, 0, len(p.Tracks))
	for _, t := range p.Tracks {
		if t.Score > 0 && !p.IsHidden(t.CatalogID, t.ArtistKey) {
			out = append(out, *t)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].CatalogID < out[j].CatalogID
	})
	if n >= 0 && len(out) > n {
		out = out[:n]
	}
	return out
}
