package lyrics

import (
	"context"
	"encoding/json"
	"time"

	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// Store is the persistence slice the lyrics service needs; *db.Queries
// (from store.Store.Q()) satisfies it directly.
type Store interface {
	GetLyrics(ctx context.Context, trackKey string) (db.Lyric, error)
	UpsertLyrics(ctx context.Context, arg db.UpsertLyricsParams) error
}

type Fetcher interface {
	Fetch(ctx context.Context, q Query) (raw string, found bool, err error)
}

type Service struct {
	Store   Store
	Client  Fetcher // nil disables the LRCLIB step
	FFprobe string  // "" → "ffprobe"
}

type Request struct {
	TrackID   string
	LocalPath string // "" when the library has no filesystem access
	Query     Query
}

// Get resolves lyrics: cache → sidecar → tags → LRCLIB → negative-cache.
// ok=false means no lyrics (serve 204). Transient LRCLIB errors return
// (zero, false, nil) without writing a cache row.
func (s *Service) Get(ctx context.Context, req Request) (Lyrics, bool, error) {
	if row, err := s.Store.GetLyrics(ctx, req.TrackID); err == nil {
		return decodeRow(row)
	}
	// Local first.
	if req.LocalPath != "" {
		if raw, source, ok := ReadLocal(ctx, s.FFprobe, req.LocalPath); ok {
			lyr := Parse(raw)
			if err := s.put(ctx, req.TrackID, lyr, source); err != nil {
				return Lyrics{}, false, err
			}
			return lyr, true, nil
		}
	}
	// Then LRCLIB.
	if s.Client != nil {
		raw, found, err := s.Client.Fetch(ctx, req.Query)
		if err != nil {
			// Transient: degrade silently, and do not poison the cache.
			return Lyrics{}, false, nil
		}
		if found {
			lyr := Parse(raw)
			if err := s.put(ctx, req.TrackID, lyr, "lrclib"); err != nil {
				return Lyrics{}, false, err
			}
			return lyr, true, nil
		}
	}
	// Genuine miss: negative-cache so we don't re-query on every play.
	if err := s.Store.UpsertLyrics(ctx, db.UpsertLyricsParams{
		TrackKey: req.TrackID, Source: "none", FetchedAt: time.Now().Unix(),
	}); err != nil {
		return Lyrics{}, false, err
	}
	return Lyrics{}, false, nil
}

func (s *Service) put(ctx context.Context, key string, lyr Lyrics, source string) error {
	arg := db.UpsertLyricsParams{TrackKey: key, Source: source, FetchedAt: time.Now().Unix()}
	if lyr.Synced {
		b, err := json.Marshal(lyr.Lines)
		if err != nil {
			return err
		}
		arg.Synced, arg.Body = 1, string(b)
	} else {
		arg.Body = lyr.Plain
	}
	return s.Store.UpsertLyrics(ctx, arg)
}

func decodeRow(row db.Lyric) (Lyrics, bool, error) {
	if row.Source == "none" {
		return Lyrics{}, false, nil
	}
	if row.Synced != 0 {
		var lines []Line
		if err := json.Unmarshal([]byte(row.Body), &lines); err != nil {
			// Corrupt cache row: treat as plain rather than erroring the player.
			return Lyrics{Plain: row.Body}, true, nil
		}
		return Lyrics{Synced: true, Lines: lines}, true, nil
	}
	return Lyrics{Plain: row.Body}, true, nil
}
