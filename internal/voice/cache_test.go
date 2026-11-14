package voice

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/registry"
)

// fakeVoice counts calls and returns fixed audio.
type fakeVoice struct {
	calls atomic.Int32
	err   error
}

func (f *fakeVoice) Type() string                         { return Type }
func (f *fakeVoice) Name() string                         { return "fake" }
func (f *fakeVoice) ConfigSchema() registry.ConfigSchema  { return registry.ConfigSchema{} }
func (f *fakeVoice) Init(map[string]any) error            { return nil }
func (f *fakeVoice) TestConnection(context.Context) error { return nil }
func (f *fakeVoice) Synthesize(ctx context.Context, text string) (Audio, error) {
	f.calls.Add(1)
	if err := CheckText(text); err != nil {
		return Audio{}, err
	}
	if f.err != nil {
		return Audio{}, f.err
	}
	return Audio{Data: []byte("ID3fake:" + text), ContentType: "audio/mpeg"}, nil
}

func TestSpeakerRendersOnceAndCaches(t *testing.T) {
	dir := t.TempDir()
	fv := &fakeVoice{}
	sp := NewSpeaker(fv, "fake:v1", NewCache(dir, 100))

	id1, err := sp.Say(context.Background(), "Here's some Bonobo.")
	if err != nil {
		t.Fatal(err)
	}
	id2, err := sp.Say(context.Background(), "Here's some Bonobo.")
	if err != nil {
		t.Fatal(err)
	}
	if id1 != id2 {
		t.Fatalf("same line, different ids: %s %s", id1, id2)
	}
	if fv.calls.Load() != 1 {
		t.Fatalf("synthesized %d times, want 1", fv.calls.Load())
	}
	path, ctype, ok := sp.Cache().Open(id1)
	if !ok || ctype != "audio/mpeg" {
		t.Fatalf("Open: ok=%v ctype=%q", ok, ctype)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "ID3fake:Here's some Bonobo." {
		t.Fatalf("clip bytes = %q", b)
	}
}

func TestSpeakerKeysOnVoiceIdentity(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir, 100)
	a, _ := NewSpeaker(&fakeVoice{}, "kokoro:af_heart", c).Say(context.Background(), "Hi")
	b, _ := NewSpeaker(&fakeVoice{}, "elevenlabs:rachel", c).Say(context.Background(), "Hi")
	if a == b {
		t.Fatal("a different voice must not reuse another voice's clip")
	}
}

func TestSpeakerCollapsesConcurrentRequests(t *testing.T) {
	fv := &fakeVoice{}
	sp := NewSpeaker(fv, "fake", NewCache(t.TempDir(), 100))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := sp.Say(context.Background(), "Same line"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if fv.calls.Load() != 1 {
		t.Fatalf("synthesized %d times, want 1", fv.calls.Load())
	}
}

func TestSpeakerPassesErrorsAndCachesNothing(t *testing.T) {
	fv := &fakeVoice{err: errors.New("quota exceeded")}
	sp := NewSpeaker(fv, "fake", NewCache(t.TempDir(), 100))
	if _, err := sp.Say(context.Background(), "Hi"); err == nil {
		t.Fatal("want error")
	}
	if _, err := sp.Say(context.Background(), " "); !errors.Is(err, ErrEmptyText) {
		t.Fatalf("blank: %v", err)
	}
	if fv.calls.Load() != 1 {
		t.Fatalf("blank text must not reach the adapter; calls = %d", fv.calls.Load())
	}
}

func TestCacheOpenRejectsUnknownAndTraversal(t *testing.T) {
	c := NewCache(t.TempDir(), 100)
	for _, id := range []string{"", "nope", "../../etc/passwd", "abc/def"} {
		if _, _, ok := c.Open(id); ok {
			t.Fatalf("Open(%q) should fail", id)
		}
	}
}

func TestCacheKeepsOnlyTheNewestClips(t *testing.T) {
	dir := t.TempDir()
	sp := NewSpeaker(&fakeVoice{}, "fake", NewCache(dir, 3))
	var first string
	for i, line := range []string{"one", "two", "three", "four", "five"} {
		id, err := sp.Say(context.Background(), line)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = id
		}
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(files) != 3 {
		t.Fatalf("cache holds %d clips, want 3", len(files))
	}
	if _, _, ok := sp.Cache().Open(first); ok {
		t.Fatal("the oldest clip should have been pruned")
	}
}
