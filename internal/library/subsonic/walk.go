package subsonic

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/maxjb-xyz/reverb/internal/library"
)

const walkPage = 500

// WalkTracks visits every track in the library. It pages search3 with the
// empty-quoted query (`""`) that Navidrome and most OpenSubsonic servers treat
// as "list everything"; a server that returns nothing for it is walked album
// by album instead (getAlbumList2 + getAlbum), which every Subsonic server
// supports but costs one request per album.
func (a *Adapter) WalkTracks(ctx context.Context, batch func([]library.IndexedTrack) error) error {
	n, err := a.walkSearch(ctx, batch)
	if err != nil || n > 0 {
		return err
	}
	return a.walkAlbums(ctx, batch)
}

func (a *Adapter) walkSearch(ctx context.Context, batch func([]library.IndexedTrack) error) (int, error) {
	total := 0
	for offset := 0; ; offset += walkPage {
		params := url.Values{}
		params.Set("query", `""`)
		params.Set("artistCount", "0")
		params.Set("albumCount", "0")
		params.Set("songCount", strconv.Itoa(walkPage))
		params.Set("songOffset", strconv.Itoa(offset))
		var resp subsonicResponse
		if err := a.client.GetJSON(ctx, "search3", params, &resp); err != nil {
			return total, err
		}
		if resp.SearchResult3 == nil || len(resp.SearchResult3.Song) == 0 {
			return total, nil
		}
		out := make([]library.IndexedTrack, 0, len(resp.SearchResult3.Song))
		for _, c := range resp.SearchResult3.Song {
			out = append(out, mapIndexed(c))
		}
		total += len(out)
		if err := batch(out); err != nil {
			return total, err
		}
		if len(resp.SearchResult3.Song) < walkPage {
			return total, nil
		}
	}
}

func (a *Adapter) walkAlbums(ctx context.Context, batch func([]library.IndexedTrack) error) error {
	for offset := 0; ; offset += walkPage {
		params := url.Values{}
		params.Set("type", "alphabeticalByName")
		params.Set("size", strconv.Itoa(walkPage))
		params.Set("offset", strconv.Itoa(offset))
		var resp subsonicResponse
		if err := a.client.GetJSON(ctx, "getAlbumList2", params, &resp); err != nil {
			return err
		}
		if resp.AlbumList2 == nil || len(resp.AlbumList2.Album) == 0 {
			return nil
		}
		for _, al := range resp.AlbumList2.Album {
			p := url.Values{}
			p.Set("id", al.ID)
			var ar subsonicResponse
			if err := a.client.GetJSON(ctx, "getAlbum", p, &ar); err != nil {
				return err
			}
			if ar.Album == nil || len(ar.Album.Song) == 0 {
				continue
			}
			out := make([]library.IndexedTrack, 0, len(ar.Album.Song))
			for _, c := range ar.Album.Song {
				out = append(out, mapIndexed(c))
			}
			if err := batch(out); err != nil {
				return err
			}
		}
		if len(resp.AlbumList2.Album) < walkPage {
			return nil
		}
	}
}

func mapIndexed(c childDTO) library.IndexedTrack {
	t := library.IndexedTrack{Track: mapTrack(c), Year: c.Year}
	for _, g := range c.Genres {
		t.Genres = appendGenres(t.Genres, g.Name)
	}
	if len(t.Genres) == 0 {
		t.Genres = appendGenres(nil, c.Genre)
	}
	if c.Created != "" {
		if ts, err := time.Parse(time.RFC3339, c.Created); err == nil {
			t.AddedAt = ts.Unix()
		}
	}
	return t
}

// appendGenres splits a genre tag on the separators taggers commonly use for
// multi-genre fields and appends the non-empty, de-duplicated parts.
func appendGenres(dst []string, tag string) []string {
	parts := strings.FieldsFunc(tag, func(r rune) bool { return r == ';' || r == '/' || r == ',' || r == '|' })
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		dup := false
		for _, d := range dst {
			if strings.EqualFold(d, p) {
				dup = true
				break
			}
		}
		if !dup {
			dst = append(dst, p)
		}
	}
	return dst
}
