package engine

import (
	"context"

	"github.com/maxjb-xyz/reverb/internal/matching"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// Suggestion is a station the user might want to start.
type Suggestion struct {
	Seed     Seed   `json:"seed"`
	Label    string `json:"label"`
	Sublabel string `json:"sublabel,omitempty"`
	// CoverArtID / LibraryArtistID let the UI show art and link the artist.
	CoverArtID      string `json:"coverArtId,omitempty"`
	LibraryArtistID string `json:"libraryArtistId,omitempty"`
}

// Suggestions lists stations built from the user's taste: their favourite
// artists, then the genres those artists play. A user with no history gets
// the library's biggest genres.
func (e *Engine) Suggestions(ctx context.Context, userID string, n int) ([]Suggestion, error) {
	if n <= 0 {
		n = 8
	}
	prof, err := e.taste.Profile(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := []Suggestion{}
	top := prof.TopArtists(max(4, n/2))
	keys := make([]string, 0, len(top))
	for _, a := range top {
		keys = append(keys, a.Key)
	}
	art := map[string]db.LibraryTrack{}
	if len(keys) > 0 {
		rows, err := e.q.ListLibraryTracksByArtistKeys(ctx, keys)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			if _, ok := art[r.ArtistKey]; !ok || (art[r.ArtistKey].CoverArtID == "" && r.CoverArtID != "") {
				art[r.ArtistKey] = r
			}
		}
	}
	for _, a := range top {
		r, ok := art[a.Key]
		if !ok {
			continue // not in the library (anymore)
		}
		name := a.Name
		if name == "" {
			name = matching.PrimaryArtist(r.Artist)
		}
		out = append(out, Suggestion{
			Seed: Seed{Kind: SeedArtist, ID: a.Key, Artist: name}, Label: name + " radio", Sublabel: "Artist radio",
			CoverArtID: r.CoverArtID, LibraryArtistID: r.ArtistID,
		})
		if len(out) >= n-2 {
			break
		}
	}

	var genres []string
	if len(keys) > 0 {
		rows, err := e.q.ListGenresForArtistKeys(ctx, db.ListGenresForArtistKeysParams{Keys: keys, Limit: int64(n)})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			genres = append(genres, r.GenreKey)
		}
	}
	if len(genres) == 0 {
		rows, err := e.q.ListLibraryGenres(ctx, int64(n))
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			genres = append(genres, r.GenreKey)
		}
	}
	for _, g := range genres {
		if len(out) >= n {
			break
		}
		rows, err := e.q.ListLibraryTracksByGenre(ctx, db.ListLibraryTracksByGenreParams{GenreKey: g, Limit: 1})
		if err != nil || len(rows) == 0 {
			continue
		}
		label := titleCase(g)
		for _, dg := range splitGenres(rows[0].Genres) {
			if matching.GenreKey(dg) == g {
				label = dg
			}
		}
		out = append(out, Suggestion{
			Seed: Seed{Kind: SeedGenre, ID: g}, Label: label + " radio", Sublabel: "Genre radio", CoverArtID: rows[0].CoverArtID,
		})
	}
	return out, nil
}
