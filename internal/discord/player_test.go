package discord

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/playback"
)

// fakeSource yields n one-byte frames tagged with the track id's first byte.
type fakeSource struct {
	mu     sync.Mutex
	left   int
	tag    byte
	err    error // returned after the frames instead of io.EOF
	closed bool
}

func (s *fakeSource) Next() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("closed")
	}
	if s.left == 0 {
		if s.err != nil {
			return nil, s.err
		}
		return nil, io.EOF
	}
	s.left--
	return []byte{s.tag}, nil
}

func (s *fakeSource) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
}

type fakeOpener struct {
	mu     sync.Mutex
	frames int
	opened []string
	starts []int64
	fail   map[string]bool
	errAt  map[string]error
}

func (o *fakeOpener) Open(_ context.Context, id string, startMs int64) (FrameSource, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.opened = append(o.opened, id)
	o.starts = append(o.starts, startMs)
	if o.fail[id] {
		return nil, errors.New("gone")
	}
	err := o.errAt[id]
	delete(o.errAt, id)
	return &fakeSource{left: o.frames, tag: id[0], err: err}, nil
}

func (o *fakeOpener) log() ([]string, []int64) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.opened...), append([]int64(nil), o.starts...)
}

func ptrack(id string) playback.Track {
	return playback.Track{Track: core.Track{ID: id, Title: "T" + id, Artist: "A", DurationMs: 60_000}}
}

// pull asks for frames like Discord's sender until it gets want of them or
// gives up.
func pull(t *testing.T, p *Player, want int) []byte {
	t.Helper()
	var got []byte
	deadline := time.Now().Add(2 * time.Second)
	for len(got) < want && time.Now().Before(deadline) {
		f, err := p.ProvideOpusFrame()
		if err != nil {
			t.Fatal(err)
		}
		if len(f) == 0 {
			time.Sleep(time.Millisecond)
			continue
		}
		got = append(got, f...)
	}
	return got
}

func TestPlayerPlaysThroughTheQueue(t *testing.T) {
	op := &fakeOpener{frames: 3}
	changes := 0
	var mu sync.Mutex
	p := NewPlayer(op, nil, func() { mu.Lock(); changes++; mu.Unlock() })
	p.PlayTracks([]playback.Track{ptrack("a"), ptrack("b")}, 0)
	got := pull(t, p, 6)
	if string(got) != "aaabbb" {
		t.Fatalf("frames = %q", got)
	}
	if st := p.State(); st.Index != 1 || st.PositionMs != 60 {
		t.Fatalf("state = %+v", st)
	}
	// The end of the queue stops playback.
	for i := 0; i < 50; i++ {
		p.ProvideOpusFrame()
		time.Sleep(time.Millisecond)
	}
	if p.State().Playing {
		t.Fatal("still playing after the last track")
	}
	mu.Lock()
	defer mu.Unlock()
	if changes < 3 {
		t.Fatalf("changes = %d", changes)
	}
}

func TestPlayerPauseHoldsPosition(t *testing.T) {
	op := &fakeOpener{frames: 50}
	p := NewPlayer(op, nil, nil)
	p.PlayTracks([]playback.Track{ptrack("a")}, 0)
	pull(t, p, 5)
	p.Pause()
	if f, _ := p.ProvideOpusFrame(); f != nil {
		t.Fatal("frame while paused")
	}
	if p.State().PositionMs != 100 {
		t.Fatalf("pos = %d", p.State().PositionMs)
	}
	p.Play()
	pull(t, p, 1)
	if p.State().PositionMs != 120 {
		t.Fatalf("pos = %d", p.State().PositionMs)
	}
}

func TestPlayerAdoptStartsAtPosition(t *testing.T) {
	op := &fakeOpener{frames: 2}
	p := NewPlayer(op, nil, nil)
	p.Adopt(&playback.State{Queue: []playback.Track{ptrack("a"), ptrack("b")}, Index: 1, PositionMs: 42_000, Repeat: "all"}, true)
	pull(t, p, 1)
	ids, starts := op.log()
	if ids[0] != "b" || starts[0] != 42_000 {
		t.Fatalf("opened %v at %v", ids, starts)
	}
	if st := p.State(); st.Repeat != "all" || !st.Playing {
		t.Fatalf("state = %+v", st)
	}
}

func TestPlayerSkipsTracksThatFailToOpen(t *testing.T) {
	op := &fakeOpener{frames: 1, fail: map[string]bool{"a": true}}
	p := NewPlayer(op, nil, nil)
	p.PlayTracks([]playback.Track{ptrack("a"), ptrack("b")}, 0)
	if got := pull(t, p, 1); string(got) != "b" {
		t.Fatalf("frames = %q", got)
	}
}

func TestPlayerStopsWhenEveryTrackFails(t *testing.T) {
	op := &fakeOpener{frames: 1, fail: map[string]bool{"a": true, "b": true}}
	p := NewPlayer(op, nil, nil)
	p.CycleRepeat() // all: would loop forever without the failure cap
	p.PlayTracks([]playback.Track{ptrack("a"), ptrack("b")}, 0)
	deadline := time.Now().Add(time.Second)
	for p.State().Playing && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if p.State().Playing {
		t.Fatal("kept trying dead tracks")
	}
}

func TestPlayerResumesABrokenStream(t *testing.T) {
	op := &fakeOpener{frames: 2, errAt: map[string]error{"a": errors.New("reset")}}
	p := NewPlayer(op, nil, nil)
	p.PlayTracks([]playback.Track{ptrack("a"), ptrack("b")}, 0)
	got := pull(t, p, 4)
	if string(got) != "aaaa" {
		t.Fatalf("frames = %q (should reopen a, not skip)", got)
	}
	_, starts := op.log()
	if len(starts) < 2 || starts[1] != 40 {
		t.Fatalf("reopened at %v", starts)
	}
}

func TestPlayerPrevRestartsOrGoesBack(t *testing.T) {
	op := &fakeOpener{frames: 1000}
	p := NewPlayer(op, nil, nil)
	p.PlayTracks([]playback.Track{ptrack("a"), ptrack("b")}, 1)
	pull(t, p, 1)
	p.Prev() // at 20 ms: back a track
	if st := p.State(); st.Index != 0 {
		t.Fatalf("index = %d", st.Index)
	}
	pull(t, p, 200) // 4 s in
	p.Prev()
	if st := p.State(); st.Index != 0 || st.PositionMs != 0 {
		t.Fatalf("state = %+v", st)
	}
}

func TestPlayerQueueEdits(t *testing.T) {
	p := NewPlayer(&fakeOpener{frames: 1000}, nil, nil)
	p.PlayTracks([]playback.Track{ptrack("a"), ptrack("b"), ptrack("c")}, 1)
	p.MoveItem(2, 0) // c a b, current b stays current
	if st := p.State(); st.Queue[st.Index].ID != "b" || st.Queue[0].ID != "c" {
		t.Fatalf("after move: %+v", st)
	}
	p.RemoveAt(0) // a b
	if st := p.State(); st.Queue[st.Index].ID != "b" || len(st.Queue) != 2 {
		t.Fatalf("after remove: %+v", st)
	}
	p.PlayNext(ptrack("x"))
	if st := p.State(); st.Queue[st.Index].ID != "x" || st.Queue[st.Index-1].ID != "b" {
		t.Fatalf("after play next: %+v", st)
	}
	p.Enqueue(ptrack("z"))
	if st := p.State(); st.Queue[len(st.Queue)-1].ID != "z" {
		t.Fatalf("after enqueue: %+v", st)
	}
	p.Apply(&playback.Command{Action: "jumpTo", Index: 0})
	if st := p.State(); st.Index != 0 {
		t.Fatalf("after jump: %+v", st)
	}
	p.Close()
	if f, _ := p.ProvideOpusFrame(); f != nil {
		t.Fatal("frames after close")
	}
}

func TestPlayerShuffleVisitsEveryTrackOnce(t *testing.T) {
	p := NewPlayer(&fakeOpener{frames: 1}, nil, nil)
	q := []playback.Track{ptrack("a"), ptrack("b"), ptrack("c"), ptrack("d")}
	p.PlayTracks(q, 0)
	p.ToggleShuffle()
	seen := map[string]bool{}
	for i := 0; i < 4; i++ {
		cur, _ := p.Current()
		seen[cur.ID] = true
		p.Next()
	}
	if len(seen) != 4 {
		t.Fatalf("shuffle visited %v", seen)
	}
}
