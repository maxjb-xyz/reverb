package api

import (
	"net/http"
	"sort"

	"github.com/maxjb-xyz/reverb/internal/core"
)

type collectionArtist struct {
	LibraryArtistID  string                  `json:"libraryArtistId"`
	Name             string                  `json:"name"`
	CoverArtID       string                  `json:"coverArtId,omitempty"`
	Source           string                  `json:"source"`
	ExternalArtistID string                  `json:"externalArtistId,omitempty"`
	OwnedAlbums      int                     `json:"ownedAlbums"`
	TotalAlbums      int                     `json:"totalAlbums"`
	MissingAlbums    []core.DiscographyAlbum `json:"missingAlbums"`
}

func (s *Server) handleCollection(w http.ResponseWriter, r *http.Request) {
	cov := s.coverage()
	if cov == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "coverage unavailable"})
		return
	}
	rows, err := cov.ListCachedDiscographies(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	artists := make([]collectionArtist, 0, len(rows))
	for _, row := range rows {
		artist := collectionArtist{LibraryArtistID: row.LibraryArtistID, Name: row.Name, CoverArtID: row.CoverArtID, Source: row.Source, ExternalArtistID: row.ExternalArtistID, TotalAlbums: len(row.Albums), MissingAlbums: []core.DiscographyAlbum{}}
		for _, album := range row.Albums {
			if album.LibraryAlbumID != "" {
				artist.OwnedAlbums++
			} else {
				artist.MissingAlbums = append(artist.MissingAlbums, album)
			}
		}
		artists = append(artists, artist)
	}
	sort.SliceStable(artists, func(i, j int) bool {
		a, b := artists[i], artists[j]
		if a.TotalAlbums == 0 {
			return false
		}
		if b.TotalAlbums == 0 {
			return true
		}
		aFull, bFull := a.OwnedAlbums == a.TotalAlbums, b.OwnedAlbums == b.TotalAlbums
		if aFull != bFull {
			return !aFull
		}
		return a.OwnedAlbums*b.TotalAlbums < b.OwnedAlbums*a.TotalAlbums
	})
	resolvedCount := len(artists)
	artistCount := resolvedCount
	if n, err := cov.CountLibraryArtists(r.Context()); err == nil {
		artistCount = n
	}
	writeJSON(w, http.StatusOK, map[string]any{"artists": artists, "resolvedCount": resolvedCount, "artistCount": artistCount})
}
