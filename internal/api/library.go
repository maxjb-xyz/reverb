package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/maximusjb/crate/internal/core"
)

// optional browse interfaces (implemented by the subsonic adapter).
type artistBrowser interface {
	GetArtistsBrowse(ctx context.Context) ([]core.Artist, error)
}
type albumBrowser interface {
	GetAlbumsBrowse(ctx context.Context, listType string, size int) ([]core.Album, error)
}

// libraryReady writes 503 and returns false if no library adapter is configured.
func (s *Server) libraryReady(w http.ResponseWriter) bool {
	if s.deps.Library == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no library configured"})
		return false
	}
	return true
}

func (s *Server) handleLibrarySearch(w http.ResponseWriter, r *http.Request) {
	if !s.libraryReady(w) {
		return
	}
	q := r.URL.Query().Get("q")
	var types []core.EntityType
	switch r.URL.Query().Get("type") {
	case "track":
		types = []core.EntityType{core.EntityTrack}
	case "album":
		types = []core.EntityType{core.EntityAlbum}
	case "artist":
		types = []core.EntityType{core.EntityArtist}
	default:
		types = []core.EntityType{core.EntityTrack, core.EntityAlbum, core.EntityArtist}
	}
	res, err := s.deps.Library.Search(r.Context(), q, types)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleLibraryArtist(w http.ResponseWriter, r *http.Request) {
	if !s.libraryReady(w) {
		return
	}
	ar, err := s.deps.Library.GetArtist(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, ar)
}

func (s *Server) handleLibraryAlbum(w http.ResponseWriter, r *http.Request) {
	if !s.libraryReady(w) {
		return
	}
	al, err := s.deps.Library.GetAlbum(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, al)
}

func (s *Server) handleLibraryPlaylists(w http.ResponseWriter, r *http.Request) {
	if !s.libraryReady(w) {
		return
	}
	pls, err := s.deps.Library.GetPlaylists(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, pls)
}

func (s *Server) handleLibraryArtists(w http.ResponseWriter, r *http.Request) {
	if !s.libraryReady(w) {
		return
	}
	br, ok := s.deps.Library.(artistBrowser)
	if !ok {
		writeJSON(w, http.StatusOK, []core.Artist{})
		return
	}
	arts, err := br.GetArtistsBrowse(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, arts)
}

func (s *Server) handleLibraryAlbums(w http.ResponseWriter, r *http.Request) {
	if !s.libraryReady(w) {
		return
	}
	br, ok := s.deps.Library.(albumBrowser)
	if !ok {
		writeJSON(w, http.StatusOK, []core.Album{})
		return
	}
	listType := r.URL.Query().Get("type")
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	albs, err := br.GetAlbumsBrowse(r.Context(), listType, size)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, albs)
}
