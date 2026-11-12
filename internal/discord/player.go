package discord

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/maxjb-xyz/reverb/internal/playback"
)

// frameMs is the length of one Opus frame Discord takes.
const frameMs = 20

// frameBuffer is how far ahead of playback a track is decoded (2 s).
const frameBuffer = 100

// Player is the bot's audio engine for one voice channel: a queue like the web
// player's, fed to Discord one 20 ms Opus frame at a time. Discord's audio
// sender calls ProvideOpusFrame every 20 ms; time only moves while it does.
type Player struct {
	opener SourceOpener
	log    *slog.Logger
	// changed is called (without the lock) after anything a viewer would see
	// changed: track, play/pause, queue, shuffle, repeat.
	changed func()

	mu       sync.Mutex
	queue    []playback.Track
	index    int
	playing  bool
	shuffle  bool
	order    []int // shuffle order of queue indices
	orderPos int
	repeat   string
	volume   float64
	posMs    int64

	// The track being decoded. gen invalidates loads and pumps that belong to
	// an earlier track.
	gen     int
	src     FrameSource
	frames  chan []byte
	srcErr  error
	retries int
	// consecutive tracks that failed to open
	failures int
	closed   bool
}

// NewPlayer makes an empty player.
func NewPlayer(opener SourceOpener, log *slog.Logger, changed func()) *Player {
	if log == nil {
		log = slog.Default()
	}
	if changed == nil {
		changed = func() {}
	}
	return &Player{opener: opener, log: log, changed: changed, index: -1, repeat: "off", volume: 1}
}

// State is the player's state as the playback hub wants it.
func (p *Player) State() playback.State {
	p.mu.Lock()
	defer p.mu.Unlock()
	return playback.State{
		Queue:      append([]playback.Track(nil), p.queue...),
		Index:      p.index,
		PositionMs: p.posMs,
		Playing:    p.playing,
		Shuffle:    p.shuffle,
		Repeat:     p.repeat,
		Volume:     p.volume,
	}
}

// Current is the track at the queue position, if any.
func (p *Player) Current() (playback.Track, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.index < 0 || p.index >= len(p.queue) {
		return playback.Track{}, false
	}
	return p.queue[p.index], true
}

// Upcoming lists up to n tracks after the current one, in play order.
func (p *Player) Upcoming(n int) []playback.Track {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []playback.Track
	if p.shuffle {
		for i := p.orderPos + 1; i < len(p.order) && len(out) < n; i++ {
			out = append(out, p.queue[p.order[i]])
		}
		return out
	}
	for i := p.index + 1; i < len(p.queue) && len(out) < n; i++ {
		out = append(out, p.queue[i])
	}
	return out
}

// ProvideOpusFrame implements disgo's voice.OpusFrameProvider. It never
// blocks: while paused or buffering it returns no frame and Discord sends
// silence.
func (p *Player) ProvideOpusFrame() ([]byte, error) {
	p.mu.Lock()
	if !p.playing || p.frames == nil {
		p.mu.Unlock()
		return nil, nil
	}
	select {
	case f, ok := <-p.frames:
		if ok {
			p.posMs += frameMs
			p.mu.Unlock()
			return f, nil
		}
		// The track's frames ran out.
		err := p.srcErr
		changed := false
		if err == nil || errors.Is(err, io.EOF) {
			changed = p.advanceLocked(1, true)
		} else if p.retries < 2 {
			// The stream broke mid-track (a long pause, a network blip):
			// pick it up where it stopped.
			p.retries++
			p.log.Warn("discord: stream broke; resuming", "err", err)
			p.loadLocked(p.posMs)
		} else {
			changed = p.advanceLocked(1, true)
		}
		p.mu.Unlock()
		if changed {
			p.changed()
		}
		return nil, nil
	default:
		p.mu.Unlock()
		return nil, nil // still buffering
	}
}

// Close implements voice.OpusFrameProvider and stops decoding.
func (p *Player) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	p.playing = false
	p.stopSourceLocked()
}

func (p *Player) stopSourceLocked() {
	p.gen++
	if p.src != nil {
		p.src.Close()
		p.src = nil
	}
	p.frames = nil
	p.srcErr = nil
}

// loadLocked starts decoding the current track from startMs. The source opens
// in the background; ProvideOpusFrame plays silence until frames arrive.
func (p *Player) loadLocked(startMs int64) {
	p.stopSourceLocked()
	p.posMs = startMs
	if p.closed || p.index < 0 || p.index >= len(p.queue) {
		return
	}
	gen := p.gen
	id := p.queue[p.index].ID
	frames := make(chan []byte, frameBuffer)
	p.frames = frames
	go func() {
		src, err := p.opener.Open(context.Background(), id, startMs)
		p.mu.Lock()
		if gen != p.gen {
			p.mu.Unlock()
			if src != nil {
				src.Close()
			}
			return
		}
		if err != nil {
			p.log.Warn("discord: open track", "track", id, "err", err)
			p.failures++
			p.frames = nil
			changed := false
			if p.failures < len(p.queue) {
				changed = p.advanceLocked(1, true)
			} else {
				p.playing = false
				changed = true
			}
			p.mu.Unlock()
			if changed {
				p.changed()
			}
			return
		}
		p.failures = 0
		p.src = src
		p.mu.Unlock()
		p.pump(gen, src, frames)
	}()
}

// pump moves packets from src into frames until the track ends or is
// replaced. Closing the source (on replace) makes Next fail, ending the loop.
func (p *Player) pump(gen int, src FrameSource, frames chan []byte) {
	var err error
	for {
		var pkt []byte
		pkt, err = src.Next()
		if err != nil {
			break
		}
		// Block while the buffer is full (playback paused or ahead), but
		// give up once this track is no longer the current one.
		for sent := false; !sent; {
			p.mu.Lock()
			stale := gen != p.gen
			p.mu.Unlock()
			if stale {
				return
			}
			select {
			case frames <- pkt:
				sent = true
			default:
				waitBriefly()
			}
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if gen != p.gen {
		return
	}
	p.srcErr = err
	close(frames)
}

// step moves the queue position one place. False at the end with repeat off.
func (p *Player) stepLocked(dir int) bool {
	if len(p.queue) == 0 {
		return false
	}
	if p.shuffle && len(p.order) == len(p.queue) {
		np := p.orderPos + dir
		if np >= len(p.order) {
			if p.repeat != "all" {
				return false
			}
			np = 0
		}
		if np < 0 {
			np = 0
		}
		p.orderPos = np
		p.index = p.order[np]
		return true
	}
	ni := p.index + dir
	if ni >= len(p.queue) {
		if p.repeat != "all" {
			return false
		}
		ni = 0
	}
	if ni < 0 {
		ni = 0
	}
	p.index = ni
	return true
}

// advanceLocked moves on a track. fromEnded is true when the current track
// finished by itself (repeat one replays it). Reports whether anything
// changed.
func (p *Player) advanceLocked(dir int, fromEnded bool) bool {
	p.retries = 0
	if fromEnded && p.repeat == "one" && p.index >= 0 {
		p.loadLocked(0)
		return true
	}
	if !p.stepLocked(dir) {
		if fromEnded {
			p.stopSourceLocked()
			p.playing = false
			p.posMs = 0
			return true
		}
		return false
	}
	p.loadLocked(0)
	return true
}

func (p *Player) rebuildShuffleLocked() {
	if !p.shuffle {
		p.order = nil
		p.orderPos = 0
		return
	}
	p.order = rand.Perm(len(p.queue))
	if p.index >= 0 {
		for i, v := range p.order {
			if v == p.index {
				p.order[0], p.order[i] = p.order[i], p.order[0]
				break
			}
		}
	}
	p.orderPos = 0
}

// do runs fn under the lock and reports the change.
func (p *Player) do(fn func() bool) {
	p.mu.Lock()
	changed := fn()
	p.mu.Unlock()
	if changed {
		p.changed()
	}
}

// Adopt takes over a session (from another device or the saved state).
func (p *Player) Adopt(st *playback.State, play bool) {
	p.do(func() bool {
		p.stopSourceLocked()
		p.queue = nil
		p.index = -1
		p.playing = false
		p.posMs = 0
		p.failures = 0
		if st != nil {
			p.queue = append([]playback.Track(nil), st.Queue...)
			p.index = st.Index
			p.shuffle = st.Shuffle
			p.repeat = st.Repeat
			if p.repeat == "" {
				p.repeat = "off"
			}
			if st.Volume > 0 {
				p.volume = st.Volume
			}
			if p.index < 0 || p.index >= len(p.queue) {
				p.index = -1
				if len(p.queue) > 0 {
					p.index = 0
				}
			}
			p.rebuildShuffleLocked()
			p.posMs = st.PositionMs
		}
		if play && p.index >= 0 {
			p.playing = true
			p.loadLocked(p.posMs)
		}
		return true
	})
}

// PlayTracks replaces the queue and starts at index.
func (p *Player) PlayTracks(tracks []playback.Track, index int) {
	p.do(func() bool {
		if len(tracks) == 0 {
			return false
		}
		if index < 0 || index >= len(tracks) {
			index = 0
		}
		p.queue = append([]playback.Track(nil), tracks...)
		p.index = index
		p.failures = 0
		p.rebuildShuffleLocked()
		p.playing = true
		p.loadLocked(0)
		return true
	})
}

// PlayNext puts t right after the current track and skips to it.
func (p *Player) PlayNext(t playback.Track) {
	p.do(func() bool {
		at := p.index + 1
		if at < 0 || at > len(p.queue) {
			at = len(p.queue)
		}
		q := make([]playback.Track, 0, len(p.queue)+1)
		q = append(q, p.queue[:at]...)
		q = append(q, t)
		q = append(q, p.queue[at:]...)
		p.queue = q
		p.index = at
		p.failures = 0
		p.rebuildShuffleLocked()
		p.playing = true
		p.loadLocked(0)
		return true
	})
}

// Enqueue adds t to the end.
func (p *Player) Enqueue(t playback.Track) {
	p.do(func() bool {
		p.queue = append(p.queue, t)
		if p.index < 0 {
			p.index = 0
		}
		if p.shuffle {
			// Shuffle the new track in among those not yet played.
			p.order = append(p.order, len(p.queue)-1)
			if rest := len(p.order) - p.orderPos - 1; rest > 1 {
				j := p.orderPos + 1 + rand.IntN(rest)
				last := len(p.order) - 1
				p.order[j], p.order[last] = p.order[last], p.order[j]
			}
		}
		return true
	})
}

// Play resumes (or starts the current track).
func (p *Player) Play() {
	p.do(func() bool {
		if p.playing || p.index < 0 {
			return false
		}
		p.playing = true
		if p.frames == nil {
			p.loadLocked(p.posMs)
		}
		return true
	})
}

// Pause stops sending audio, keeping the place.
func (p *Player) Pause() {
	p.do(func() bool {
		if !p.playing {
			return false
		}
		p.playing = false
		return true
	})
}

// Toggle flips play/pause.
func (p *Player) Toggle() {
	p.mu.Lock()
	playing := p.playing
	p.mu.Unlock()
	if playing {
		p.Pause()
	} else {
		p.Play()
	}
}

// Next skips to the next track.
func (p *Player) Next() {
	p.do(func() bool { return p.advanceLocked(1, false) })
}

// Prev restarts the track if more than 3 s in, else goes back one.
func (p *Player) Prev() {
	p.do(func() bool {
		if p.index < 0 {
			return false
		}
		if p.posMs <= 3000 {
			p.stepLocked(-1) // at the first track this stays put: a restart
		}
		p.retries = 0
		p.loadLocked(0)
		return true
	})
}

// SeekTo moves within the current track.
func (p *Player) SeekTo(ms int64) {
	p.do(func() bool {
		if p.index < 0 {
			return false
		}
		if d := int64(p.queue[p.index].DurationMs); d > 0 && ms > d {
			ms = d
		}
		if ms < 0 {
			ms = 0
		}
		p.retries = 0
		if p.playing {
			p.loadLocked(ms)
		} else {
			p.stopSourceLocked()
			p.posMs = ms
		}
		return true
	})
}

// JumpTo plays the track at index.
func (p *Player) JumpTo(index int) {
	p.do(func() bool {
		if index < 0 || index >= len(p.queue) {
			return false
		}
		p.index = index
		if p.shuffle {
			for i, v := range p.order {
				if v == index {
					p.orderPos = i
				}
			}
		}
		p.retries = 0
		p.playing = true
		p.loadLocked(0)
		return true
	})
}

// RemoveAt drops the track at index.
func (p *Player) RemoveAt(index int) {
	p.do(func() bool {
		if index < 0 || index >= len(p.queue) {
			return false
		}
		wasCurrent := index == p.index
		p.queue = append(p.queue[:index:index], p.queue[index+1:]...)
		if index < p.index {
			p.index--
		}
		if p.index >= len(p.queue) {
			p.index = len(p.queue) - 1
		}
		p.rebuildShuffleLocked()
		if wasCurrent {
			if p.index < 0 {
				p.stopSourceLocked()
				p.playing = false
			} else {
				p.loadLocked(0)
			}
		}
		return true
	})
}

// MoveItem moves a queue entry, keeping the current track playing.
func (p *Player) MoveItem(from, to int) {
	p.do(func() bool {
		if from < 0 || from >= len(p.queue) || to < 0 || to >= len(p.queue) || from == to {
			return false
		}
		curIdx := p.index
		t := p.queue[from]
		q := append(p.queue[:from:from], p.queue[from+1:]...)
		q = append(q[:to:to], append([]playback.Track{t}, q[to:]...)...)
		p.queue = q
		// Follow the current entry by position first (duplicates share ids).
		switch {
		case from == curIdx:
			p.index = to
		case from < curIdx && to >= curIdx:
			p.index = curIdx - 1
		case from > curIdx && to <= curIdx:
			p.index = curIdx + 1
		}
		p.rebuildShuffleLocked()
		return true
	})
}

// ToggleShuffle flips shuffle.
func (p *Player) ToggleShuffle() {
	p.do(func() bool {
		p.shuffle = !p.shuffle
		p.rebuildShuffleLocked()
		return true
	})
}

// CycleRepeat goes off -> all -> one -> off.
func (p *Player) CycleRepeat() {
	p.do(func() bool {
		switch p.repeat {
		case "off":
			p.repeat = "all"
		case "all":
			p.repeat = "one"
		default:
			p.repeat = "off"
		}
		return true
	})
}

// SetVolume stores the volume the session reports. Listeners set their own
// volume for the bot in Discord, so the audio is not scaled.
func (p *Player) SetVolume(v float64) {
	p.do(func() bool {
		if v < 0 || v > 1 {
			return false
		}
		p.volume = v
		return true
	})
}

// Apply runs a remote control command from the session.
func (p *Player) Apply(cmd *playback.Command) {
	switch cmd.Action {
	case "play":
		p.Play()
	case "pause":
		p.Pause()
	case "toggle":
		p.Toggle()
	case "next":
		p.Next()
	case "prev":
		p.Prev()
	case "seek":
		p.SeekTo(cmd.PositionMs)
	case "setVolume":
		p.SetVolume(cmd.Volume)
	case "toggleShuffle":
		p.ToggleShuffle()
	case "cycleRepeat":
		p.CycleRepeat()
	case "jumpTo":
		p.JumpTo(cmd.Index)
	case "removeAt":
		p.RemoveAt(cmd.Index)
	case "moveItem":
		p.MoveItem(cmd.From, cmd.To)
	case "enqueue":
		if cmd.Track != nil {
			p.Enqueue(*cmd.Track)
		}
	case "playTracks":
		p.PlayTracks(cmd.Tracks, cmd.Index)
	}
}

// waitBriefly paces the pump while the frame buffer is full.
func waitBriefly() { time.Sleep(10 * time.Millisecond) }
