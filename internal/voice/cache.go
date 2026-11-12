package voice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"golang.org/x/sync/singleflight"
)

// Cache keeps rendered voice clips on disk, one file per clip, named by a
// hash of the voice and the text. It holds at most max clips and drops the
// least recently used ones first.
type Cache struct {
	dir string
	max int

	mu    sync.Mutex
	seq   uint64
	clips map[string]*clip
}

type clip struct {
	path  string
	ctype string
	used  uint64
}

// Clip file extensions and the content types they stand for.
var extTypes = map[string]string{
	".mp3":  "audio/mpeg",
	".wav":  "audio/wav",
	".ogg":  "audio/ogg",
	".opus": "audio/ogg",
	".aac":  "audio/aac",
	".flac": "audio/flac",
}

func extFor(ctype string) string {
	ct := strings.ToLower(strings.TrimSpace(strings.SplitN(ctype, ";", 2)[0]))
	switch ct {
	case "audio/wav", "audio/x-wav", "audio/wave":
		return ".wav"
	case "audio/ogg", "audio/opus":
		return ".ogg"
	case "audio/aac":
		return ".aac"
	case "audio/flac":
		return ".flac"
	default:
		return ".mp3"
	}
}

const idLen = 32

func validID(id string) bool {
	if len(id) != idLen {
		return false
	}
	for _, r := range id {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// NewCache opens a clip cache in dir, creating it if needed, and picks up any
// clips left there by an earlier run. max < 1 means 1.
func NewCache(dir string, max int) *Cache {
	if max < 1 {
		max = 1
	}
	c := &Cache{dir: dir, max: max, clips: map[string]*clip{}}
	_ = os.MkdirAll(dir, 0o755)
	entries, _ := os.ReadDir(dir)
	type found struct {
		id    string
		c     *clip
		mtime int64
	}
	var all []found
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		ctype, ok := extTypes[ext]
		id := strings.TrimSuffix(e.Name(), ext)
		if !ok || !validID(id) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		all = append(all, found{id, &clip{path: filepath.Join(dir, e.Name()), ctype: ctype}, info.ModTime().UnixNano()})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].mtime < all[j].mtime })
	for _, f := range all {
		c.seq++
		f.c.used = c.seq
		c.clips[f.id] = f.c
	}
	c.mu.Lock()
	c.pruneLocked()
	c.mu.Unlock()
	return c
}

// Open returns the file path and content type of a cached clip.
func (c *Cache) Open(id string) (path, contentType string, ok bool) {
	if !validID(id) {
		return "", "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	cl, ok := c.clips[id]
	if !ok {
		return "", "", false
	}
	c.seq++
	cl.used = c.seq
	return cl.path, cl.ctype, true
}

func (c *Cache) has(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	cl, ok := c.clips[id]
	if ok {
		c.seq++
		cl.used = c.seq
	}
	return ok
}

func (c *Cache) put(id string, a Audio) error {
	ext := extFor(a.ContentType)
	path := filepath.Join(c.dir, id+ext)
	tmp, err := os.CreateTemp(c.dir, ".clip-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(a.Data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	c.clips[id] = &clip{path: path, ctype: extTypes[ext], used: c.seq}
	c.pruneLocked()
	return nil
}

func (c *Cache) pruneLocked() {
	for len(c.clips) > c.max {
		var oldest string
		var min uint64
		for id, cl := range c.clips {
			if oldest == "" || cl.used < min {
				oldest, min = id, cl.used
			}
		}
		_ = os.Remove(c.clips[oldest].path)
		delete(c.clips, oldest)
	}
}

// Speaker renders lines through one voice and caches the clips, so a line the
// DJ says twice is only synthesized once.
type Speaker struct {
	v        Voice
	identity string
	cache    *Cache
	group    singleflight.Group
}

// NewSpeaker wraps a voice. identity names the voice and its settings (for
// example "elevenlabs:<voice id>:<model>"); clips are keyed on it, so changing
// voice never replays another voice's clip.
func NewSpeaker(v Voice, identity string, c *Cache) *Speaker {
	return &Speaker{v: v, identity: identity, cache: c}
}

// Cache returns the clip cache the speaker writes to.
func (s *Speaker) Cache() *Cache { return s.cache }

// Say renders text (or finds it in the cache) and returns the clip id.
func (s *Speaker) Say(ctx context.Context, text string) (string, error) {
	text = strings.TrimSpace(text)
	if err := CheckText(text); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(s.identity + "\x00" + text))
	id := hex.EncodeToString(sum[:])[:idLen]
	if s.cache.has(id) {
		return id, nil
	}
	_, err, _ := s.group.Do(id, func() (any, error) {
		if s.cache.has(id) {
			return nil, nil
		}
		a, err := s.v.Synthesize(ctx, text)
		if err != nil {
			return nil, err
		}
		if len(a.Data) == 0 {
			return nil, fmt.Errorf("voice: %s returned no audio", s.v.Name())
		}
		return nil, s.cache.put(id, a)
	})
	if err != nil {
		return "", err
	}
	return id, nil
}
