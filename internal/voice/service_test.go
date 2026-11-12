package voice

import (
	"context"
	"errors"
	"testing"
)

type namedVoice struct {
	fakeVoice
	id string
}

func (n *namedVoice) Identity() string { return n.id }

func TestServiceWithoutVoiceIsUnavailable(t *testing.T) {
	s := NewService(NewCache(t.TempDir(), 10))
	if s.Active() != "" {
		t.Fatalf("Active() = %q, want empty", s.Active())
	}
	if _, err := s.Say(context.Background(), "Hi"); !errors.Is(err, ErrNoVoice) {
		t.Fatalf("err = %v, want ErrNoVoice", err)
	}
}

func TestServiceSaysThroughTheCurrentVoice(t *testing.T) {
	s := NewService(NewCache(t.TempDir(), 10))
	v := &namedVoice{id: "fake:a"}
	s.Set(v)
	if s.Active() != "fake" {
		t.Fatalf("Active() = %q", s.Active())
	}
	id, err := s.Say(context.Background(), "Hi")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := s.Open(id); !ok {
		t.Fatal("clip should be open-able")
	}

	// Swapping to a voice with another identity renders the line again.
	v2 := &namedVoice{id: "fake:b"}
	s.Set(v2)
	id2, err := s.Say(context.Background(), "Hi")
	if err != nil {
		t.Fatal(err)
	}
	if id2 == id || v2.calls.Load() != 1 {
		t.Fatalf("new voice must render its own clip (id2=%s calls=%d)", id2, v2.calls.Load())
	}

	s.Set(nil)
	if s.Active() != "" {
		t.Fatal("Set(nil) should turn the voice off")
	}
	// Clips already made stay playable after the voice is turned off.
	if _, _, ok := s.Open(id); !ok {
		t.Fatal("old clip should still open")
	}
}
