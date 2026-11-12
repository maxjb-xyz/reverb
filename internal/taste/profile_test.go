package taste

import (
	"testing"
	"time"
)

var now = time.Unix(1_800_000_000, 0)

func ev(id, artist, outcome string, ms, dur int64, ctxKind string, age time.Duration) Listen {
	return Listen{
		CatalogID: id, Title: id, Artist: artist, ArtistKey: ArtistKey(artist),
		StartedAt: now.Add(-age).Unix(), MsPlayed: ms, DurationMs: dur,
		Outcome: outcome, ContextKind: ctxKind,
	}
}

func TestBuild_CompletionsBeatSkips(t *testing.T) {
	p := Build("u", now, Inputs{Listens: []Listen{
		ev("trk_a", "Alpha", OutcomeCompleted, 200_000, 200_000, "album", time.Hour),
		ev("trk_a", "Alpha", OutcomeCompleted, 200_000, 200_000, "album", 2*time.Hour),
		ev("trk_b", "Beta", OutcomeSkipEarly, 4_000, 200_000, "radio", time.Hour),
	}})
	if got := p.TrackScore("trk_a"); got <= 1.5 {
		t.Fatalf("two fresh completions should score near 2, got %v", got)
	}
	if got := p.TrackScore("trk_b"); got >= 0 {
		t.Fatalf("an early radio skip must be negative, got %v", got)
	}
	if p.ArtistScore(ArtistKey("Alpha")) <= p.ArtistScore(ArtistKey("Beta")) {
		t.Fatal("artist with completions must outrank artist with skips")
	}
	if p.Tracks["trk_a"].Completions != 2 || p.Tracks["trk_b"].EarlySkips != 1 {
		t.Fatalf("counters wrong: %+v %+v", p.Tracks["trk_a"], p.Tracks["trk_b"])
	}
}

func TestBuild_EarlySkipCostsMoreWhenReverbPicked(t *testing.T) {
	p := Build("u", now, Inputs{Listens: []Listen{
		ev("trk_user", "A", OutcomeSkipEarly, 3_000, 200_000, "album", time.Hour),
		ev("trk_auto", "B", OutcomeSkipEarly, 3_000, 200_000, "radio", time.Hour),
	}})
	if !(p.TrackScore("trk_auto") < p.TrackScore("trk_user")) {
		t.Fatalf("auto-picked skip (%v) should cost more than user-picked skip (%v)",
			p.TrackScore("trk_auto"), p.TrackScore("trk_user"))
	}
}

func TestBuild_LateSkipDependsOnHowMuchWasHeard(t *testing.T) {
	p := Build("u", now, Inputs{Listens: []Listen{
		ev("trk_most", "A", OutcomeSkipLate, 180_000, 200_000, "album", time.Hour),
		ev("trk_little", "B", OutcomeSkipLate, 40_000, 200_000, "album", time.Hour),
	}})
	if p.TrackScore("trk_most") <= 0 {
		t.Fatalf("skipping near the end is still a positive listen, got %v", p.TrackScore("trk_most"))
	}
	if p.TrackScore("trk_little") >= 0 {
		t.Fatalf("bailing at 20%% is negative, got %v", p.TrackScore("trk_little"))
	}
}

func TestBuild_OldListeningDecays(t *testing.T) {
	p := Build("u", now, Inputs{Listens: []Listen{
		ev("trk_new", "A", OutcomeCompleted, 1, 1, "album", time.Hour),
		ev("trk_old", "B", OutcomeCompleted, 1, 1, "album", 365*24*time.Hour),
	}})
	n, o := p.TrackScore("trk_new"), p.TrackScore("trk_old")
	if !(o < n/2) {
		t.Fatalf("a year-old completion (%v) should weigh well under half a fresh one (%v)", o, n)
	}
	if o <= 0 {
		t.Fatalf("old listening still counts, got %v", o)
	}
}

func TestBuild_SignalsAndLegacyPlays(t *testing.T) {
	p := Build("u", now, Inputs{
		Plays: []Play{{CatalogID: "trk_p", Title: "P", Artist: "Gamma", PlayedAt: now.Add(-time.Hour).Unix(), Completed: true}},
		Signals: []Signal{
			{Kind: SignalPlaylistAdd, CatalogID: "trk_s", Title: "S", Artist: "Delta", CreatedAt: now.Unix()},
			{Kind: SignalDownload, ArtistKey: ArtistKey("Epsilon"), Artist: "Epsilon", CreatedAt: now.Unix()},
		},
	})
	if p.TrackScore("trk_p") <= 0 {
		t.Fatal("a legacy qualified play counts as positive")
	}
	if p.TrackScore("trk_s") < 2 {
		t.Fatalf("a playlist add is a strong positive, got %v", p.TrackScore("trk_s"))
	}
	if p.ArtistScore(ArtistKey("Delta")) <= 0 || p.ArtistScore(ArtistKey("Epsilon")) <= 0 {
		t.Fatal("signals must lift the artist too, including artist-level downloads")
	}
	if p.Artists[ArtistKey("Delta")].Name != "Delta" {
		t.Fatalf("artist display name lost: %+v", p.Artists[ArtistKey("Delta")])
	}
}

func TestBuild_FeedbackHides(t *testing.T) {
	p := Build("u", now, Inputs{
		Listens: []Listen{ev("trk_a", "Alpha", OutcomeCompleted, 1, 1, "album", time.Hour)},
		Feedback: []Feedback{
			{TargetKind: TargetTrack, TargetKey: "trk_x"},
			{TargetKind: TargetArtist, TargetKey: ArtistKey("Alpha")},
		},
	})
	if !p.IsHidden("trk_x", "") {
		t.Fatal("thumbed-down track must be hidden")
	}
	if !p.IsHidden("trk_other", ArtistKey("Alpha")) {
		t.Fatal("any track by a thumbed-down artist must be hidden")
	}
	if p.IsHidden("trk_y", ArtistKey("Zeta")) {
		t.Fatal("unrelated track must not be hidden")
	}
}

func TestProfile_TopListsAreSortedAndSkipHidden(t *testing.T) {
	p := Build("u", now, Inputs{
		Listens: []Listen{
			ev("trk_a", "Alpha", OutcomeCompleted, 1, 1, "album", time.Hour),
			ev("trk_b", "Beta", OutcomeCompleted, 1, 1, "album", time.Hour),
			ev("trk_b", "Beta", OutcomeCompleted, 1, 1, "album", time.Hour),
			ev("trk_c", "Gamma", OutcomeCompleted, 1, 1, "album", time.Hour),
			ev("trk_c", "Gamma", OutcomeCompleted, 1, 1, "album", time.Hour),
			ev("trk_c", "Gamma", OutcomeCompleted, 1, 1, "album", time.Hour),
		},
		Feedback: []Feedback{{TargetKind: TargetArtist, TargetKey: ArtistKey("Gamma")}},
	})
	arts := p.TopArtists(10)
	if len(arts) != 2 || arts[0].Name != "Beta" || arts[1].Name != "Alpha" {
		t.Fatalf("top artists = %+v; want Beta, Alpha (Gamma hidden)", arts)
	}
	trs := p.TopTracks(1)
	if len(trs) != 1 || trs[0].CatalogID != "trk_b" {
		t.Fatalf("top tracks = %+v; want trk_b", trs)
	}
}

func TestArtistKey_UsesPrimaryArtistAndNormalizes(t *testing.T) {
	if ArtistKey("Daft Punk") != ArtistKey("  daft punk ") {
		t.Fatal("case/space must not matter")
	}
	if ArtistKey("Egzod; Maestro Chives") != ArtistKey("Egzod") {
		t.Fatal("composite credits key on the primary artist")
	}
	if ArtistKey("") != "" {
		t.Fatal("empty in, empty out")
	}
}

func TestNormalizeOutcome(t *testing.T) {
	cases := []struct {
		in      string
		ms, dur int
		want    string
		wantOK  bool
	}{
		{"completed", 1, 1, OutcomeCompleted, true},
		{"skip_early", 1_000, 200_000, OutcomeSkipEarly, true},
		{"skip_late", 198_500, 200_000, OutcomeCompleted, true}, // next pressed on the outro
		{"", 10_000, 200_000, OutcomeSkipEarly, true},
		{"", 100_000, 200_000, OutcomeSkipLate, true},
		{"", 199_000, 200_000, OutcomeCompleted, true},
		{"bogus", 1, 1, "", false},
	}
	for _, c := range cases {
		got, ok := NormalizeOutcome(c.in, c.ms, c.dur)
		if got != c.want || ok != c.wantOK {
			t.Errorf("NormalizeOutcome(%q,%d,%d) = %q,%v; want %q,%v", c.in, c.ms, c.dur, got, ok, c.want, c.wantOK)
		}
	}
}
