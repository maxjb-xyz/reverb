package taste

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/catalog"
	"github.com/maxjb-xyz/reverb/internal/play"
	"github.com/maxjb-xyz/reverb/internal/store"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newSvc(t *testing.T) (*Service, *play.Service, *clock) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/taste.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	n := 0
	idgen := func() string { n++; return fmt.Sprintf("id%06d", n) }
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	cat := catalog.NewService(st.Q(), c.now, idgen)
	return NewService(st.Q(), cat, c.now, idgen), play.NewService(st.Q(), cat, c.now, idgen), c
}

var hurt = TrackRef{LibraryTrackID: "lib-1", Title: "Hurt", Artist: "Johnny Cash", Album: "American IV", DurationMs: 218_000}
var ring = TrackRef{LibraryTrackID: "lib-2", Title: "Ring of Fire", Artist: "Johnny Cash", Album: "Ring", DurationMs: 160_000}
var song2 = TrackRef{LibraryTrackID: "lib-3", Title: "Song 2", Artist: "Blur", Album: "Blur", DurationMs: 122_000}

func TestService_ListensBuildProfile(t *testing.T) {
	s, _, _ := newSvc(t)
	ctx := context.Background()
	for _, in := range []ListenInput{
		{TrackRef: hurt, MsPlayed: 218_000, Outcome: OutcomeCompleted, ContextKind: "album", ContextID: "alb-1"},
		{TrackRef: ring, MsPlayed: 159_000, Outcome: OutcomeSkipLate, ContextKind: "album"}, // outro skip → completed
		{TrackRef: song2, MsPlayed: 2_000, Outcome: OutcomeSkipEarly, ContextKind: "radio", ContextID: "st-1"},
	} {
		if err := s.RecordListen(ctx, "u1", in); err != nil {
			t.Fatal(err)
		}
	}
	p, err := s.Profile(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	cash := p.Artists[ArtistKey("Johnny Cash")]
	if cash == nil || cash.Completions != 2 || cash.Score < 1.9 {
		t.Fatalf("Johnny Cash taste = %+v; want 2 completions, score ~2", cash)
	}
	if p.ArtistScore(ArtistKey("Blur")) >= 0 {
		t.Fatal("early radio skip should make Blur negative")
	}
	top := p.TopTracks(5)
	if len(top) != 2 || top[0].LibraryTrackID == "" {
		t.Fatalf("top tracks = %+v; want the two Cash tracks with library ids", top)
	}

	// Another user's profile is independent.
	p2, err := s.Profile(ctx, "u2")
	if err != nil {
		t.Fatal(err)
	}
	if len(p2.Tracks) != 0 {
		t.Fatalf("u2 should have no taste, got %d tracks", len(p2.Tracks))
	}
}

func TestService_RejectsBadInput(t *testing.T) {
	s, _, _ := newSvc(t)
	ctx := context.Background()
	if err := s.RecordListen(ctx, "u", ListenInput{TrackRef: hurt, Outcome: "loved"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown outcome: err = %v", err)
	}
	if err := s.RecordListen(ctx, "u", ListenInput{TrackRef: TrackRef{Artist: "x"}, Outcome: OutcomeCompleted}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing title: err = %v", err)
	}
	if err := s.RecordSignal(ctx, "u", "starred", hurt); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown signal: err = %v", err)
	}
	if _, err := s.SetFeedback(ctx, "u", FeedbackInput{TargetKind: "album", TrackRef: hurt}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown feedback kind: err = %v", err)
	}
}

func TestService_LegacyPlaysCountOnlyBeforeListenLog(t *testing.T) {
	s, plays, c := newSvc(t)
	ctx := context.Background()
	rec := func(tr TrackRef) {
		if err := plays.Record(ctx, "u", play.PlayInput{Title: tr.Title, Artist: tr.Artist, Album: tr.Album, DurationMs: tr.DurationMs, MsPlayed: tr.DurationMs, Completed: true}); err != nil {
			t.Fatal(err)
		}
	}
	rec(hurt) // before the listen log existed
	c.t = c.t.Add(time.Hour)
	if err := s.RecordListen(ctx, "u", ListenInput{TrackRef: hurt, MsPlayed: 218_000, Outcome: OutcomeCompleted}); err != nil {
		t.Fatal(err)
	}
	rec(hurt) // the same playback also wrote a play row; must not double count
	p, err := s.Profile(ctx, "u")
	if err != nil {
		t.Fatal(err)
	}
	var tt *TrackTaste
	for _, v := range p.Tracks {
		tt = v
	}
	if len(p.Tracks) != 1 || tt.Listens != 2 {
		t.Fatalf("want one track with 2 listens (legacy + log), got %d tracks, %+v", len(p.Tracks), tt)
	}
}

func TestService_SignalsAndFeedbackRoundTrip(t *testing.T) {
	s, _, _ := newSvc(t)
	ctx := context.Background()
	if err := s.RecordSignal(ctx, "u", SignalPlaylistAdd, song2); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordSignal(ctx, "u", SignalDownload, TrackRef{Artist: "Radiohead"}); err != nil {
		t.Fatal(err)
	}
	p, err := s.Profile(ctx, "u")
	if err != nil {
		t.Fatal(err)
	}
	if p.ArtistScore(ArtistKey("Blur")) < 2 || p.Artists[ArtistKey("Radiohead")].Name != "Radiohead" {
		t.Fatalf("signals not reflected: blur=%v radiohead=%+v", p.ArtistScore(ArtistKey("Blur")), p.Artists[ArtistKey("Radiohead")])
	}

	tf, err := s.SetFeedback(ctx, "u", FeedbackInput{TargetKind: TargetTrack, TrackRef: song2})
	if err != nil {
		t.Fatal(err)
	}
	af, err := s.SetFeedback(ctx, "u", FeedbackInput{TargetKind: TargetArtist, TrackRef: TrackRef{Artist: "Radiohead"}, LibraryArtistID: "ar-9"})
	if err != nil {
		t.Fatal(err)
	}
	if tf.Label != "Song 2" || tf.Sublabel != "Blur" || af.LibraryID != "ar-9" {
		t.Fatalf("feedback rows = %+v %+v", tf, af)
	}
	// Profile reflects feedback immediately (cache invalidated).
	p, _ = s.Profile(ctx, "u")
	if !p.IsHidden(tf.TargetKey, "") || !p.IsHidden("", ArtistKey("Radiohead")) {
		t.Fatal("thumbs-down not applied to profile")
	}
	list, err := s.ListFeedback(ctx, "u")
	if err != nil || len(list) != 2 {
		t.Fatalf("ListFeedback = %+v, %v", list, err)
	}
	if err := s.ClearFeedback(ctx, "u", TargetArtist, af.TargetKey); err != nil {
		t.Fatal(err)
	}
	p, _ = s.Profile(ctx, "u")
	if p.IsHidden("", ArtistKey("Radiohead")) {
		t.Fatal("cleared artist feedback still hides")
	}
}

func TestService_FeedbackKeysOnSameCatalogEntityAsListens(t *testing.T) {
	s, _, _ := newSvc(t)
	ctx := context.Background()
	if err := s.RecordListen(ctx, "u", ListenInput{TrackRef: hurt, MsPlayed: 218_000, Outcome: OutcomeCompleted}); err != nil {
		t.Fatal(err)
	}
	f, err := s.SetFeedback(ctx, "u", FeedbackInput{TargetKind: TargetTrack, TrackRef: hurt})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := s.Profile(ctx, "u")
	if _, ok := p.Tracks[f.TargetKey]; !ok {
		t.Fatal("thumbs-down must target the same catalog entity the listens were scored under")
	}
	if len(p.TopTracks(10)) != 0 {
		t.Fatal("a thumbed-down track must drop out of top tracks")
	}
}
