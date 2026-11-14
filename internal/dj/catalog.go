package dj

import (
	"context"
	"strings"

	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// LibraryLister is the part of the store a library catalog reads.
type LibraryLister interface {
	ListLibraryArtistKeys(ctx context.Context) ([]db.ListLibraryArtistKeysRow, error)
	ListLibraryGenres(ctx context.Context, limit int64) ([]db.ListLibraryGenresRow, error)
}

// LibraryCatalog lists the artists and genres in the recommendation index.
type LibraryCatalog struct{ q LibraryLister }

// NewLibraryCatalog reads the library index through q.
func NewLibraryCatalog(q LibraryLister) *LibraryCatalog { return &LibraryCatalog{q: q} }

// Artists lists the library's artists, biggest first.
func (c *LibraryCatalog) Artists(ctx context.Context) ([]Name, error) {
	rows, err := c.q.ListLibraryArtistKeys(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Name, 0, len(rows))
	for _, r := range rows {
		out = append(out, Name{Key: r.ArtistKey, Name: r.Name})
	}
	return out, nil
}

// Genres lists the library's genres, biggest first.
func (c *LibraryCatalog) Genres(ctx context.Context) ([]Name, error) {
	rows, err := c.q.ListLibraryGenres(ctx, 1000)
	if err != nil {
		return nil, err
	}
	out := make([]Name, 0, len(rows))
	for _, r := range rows {
		out = append(out, Name{Key: r.GenreKey, Name: genreName(r.GenreKey)})
	}
	return out, nil
}

// genreName is a readable form of a genre key ("hip hop" -> "Hip hop").
func genreName(key string) string {
	r := []rune(key)
	if len(r) == 0 {
		return ""
	}
	return strings.ToUpper(string(r[:1])) + string(r[1:])
}
