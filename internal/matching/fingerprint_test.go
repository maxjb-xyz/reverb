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

// External metadata carries the primary artist while a library tag can carry the
// full "; "-joined credit — both must fingerprint to the same identity.
func TestFingerprint_CompositeCreditConvergesOnPrimaryArtist(t *testing.T) {
	a := Fingerprint("Royalty", "Egzod", "Royalty", 221000)
	b := Fingerprint("Royalty", "Egzod; Maestro Chives; Neoni", "Royalty", 221000)
	if a != b {
		t.Errorf("composite credit fingerprint diverged from primary-artist fingerprint")
	}
}

// Bare "/" is a literal in real band names and must never split — an AC/DC
// fingerprint keyed on the full name is a PERSISTED identity that cannot churn.
func TestFingerprint_BareSlashNameUnsplit(t *testing.T) {
	acdc := Fingerprint("Thunderstruck", "AC/DC", "The Razors Edge", 292000)
	ac := Fingerprint("Thunderstruck", "AC", "The Razors Edge", 292000)
	if acdc == ac {
		t.Errorf("AC/DC fingerprint collapsed to its first slash segment")
	}
}
