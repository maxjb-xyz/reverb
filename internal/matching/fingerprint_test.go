package matching

import "testing"

func TestFingerprint_LiveVsStudioDistinct(t *testing.T) {
	studio := Fingerprint("Hurt", "Johnny Cash", "American IV", 218000)
	live := Fingerprint("Hurt (Live)", "Johnny Cash", "American IV", 235000)
	if studio == live {
		t.Fatal("live and studio must not collide")
	}
}

func TestFingerprint_QualifierFormsConverge(t *testing.T) {
	// Same live recording labelled three ways must produce ONE fingerprint.
	a := Fingerprint("Song (Live)", "Artist", "Album", 240000)
	b := Fingerprint("Song - Live", "Artist", "Album", 240000)
	c := Fingerprint("Song [Live]", "Artist", "Album", 240000)
	if a != b || b != c {
		t.Fatalf("qualifier forms must converge: %q %q %q", a, b, c)
	}
}

func TestFingerprint_TitleCollisionSeparatedByArtistAlbumDuration(t *testing.T) {
	x := Fingerprint("Intro", "Artist A", "Album A", 60000)
	y := Fingerprint("Intro", "Artist B", "Album B", 95000)
	if x == y {
		t.Fatal("distinct 'Intro' tracks must not collide")
	}
}

func TestFingerprint_DurationWobbleWithinBucketStable(t *testing.T) {
	// <5s wobble (re-match jitter) stays in one bucket.
	a := Fingerprint("Song", "Artist", "Album", 200000)
	b := Fingerprint("Song", "Artist", "Album", 203000)
	if a != b {
		t.Fatal("sub-bucket duration wobble must not split identity")
	}
}
