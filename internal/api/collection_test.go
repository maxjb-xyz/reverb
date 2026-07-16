package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/core"
)

// TestCollectionReportsTrueArtistTotals asserts the collection summary reports
// resolvedCount (artists with a cached discography) separately from artistCount
// (total library artists), rather than always reading N of N.
func TestCollectionReportsTrueArtistTotals(t *testing.T) {
	cov := &fakeCoverage{
		discographies: []collectionFakeDiscography{
			{
				libraryArtistID: "a1", name: "The Band", source: "spotify", externalArtistID: "sp1",
				albums: []core.DiscographyAlbum{
					{Source: "spotify", ExternalID: "al1", Name: "One", LibraryAlbumID: "lib-al1"},
					{Source: "spotify", ExternalID: "al2", Name: "Two"},
					{Source: "spotify", ExternalID: "al3", Name: "Three"},
				},
			},
		},
		artistTotal: 5,
	}
	srv, cookie := coverageTestServer(t, cov, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/collection", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Artists []struct {
			OwnedAlbums   int                     `json:"ownedAlbums"`
			TotalAlbums   int                     `json:"totalAlbums"`
			MissingAlbums []core.DiscographyAlbum `json:"missingAlbums"`
		} `json:"artists"`
		ResolvedCount int `json:"resolvedCount"`
		ArtistCount   int `json:"artistCount"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Artists) != 1 {
		t.Fatalf("artists = %+v", body.Artists)
	}
	if body.Artists[0].OwnedAlbums != 1 || body.Artists[0].TotalAlbums != 3 {
		t.Fatalf("owned/total = %d/%d, want 1/3", body.Artists[0].OwnedAlbums, body.Artists[0].TotalAlbums)
	}
	if len(body.Artists[0].MissingAlbums) != 2 {
		t.Fatalf("missing albums = %d, want 2", len(body.Artists[0].MissingAlbums))
	}
	if body.ResolvedCount != 1 {
		t.Fatalf("resolvedCount = %d, want 1", body.ResolvedCount)
	}
	if body.ArtistCount != 5 {
		t.Fatalf("artistCount = %d, want 5", body.ArtistCount)
	}
}

// TestCollectionCountErrorFallsBackToResolvedCount asserts that when the library
// artist count fails, artistCount degrades to resolvedCount rather than failing
// the whole request.
func TestCollectionCountErrorFallsBackToResolvedCount(t *testing.T) {
	cov := &fakeCoverage{
		discographies: []collectionFakeDiscography{
			{libraryArtistID: "a1", name: "The Band", source: "spotify", externalArtistID: "sp1"},
		},
		artistCountErr: errFakeCount,
	}
	srv, cookie := coverageTestServer(t, cov, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/collection", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		ResolvedCount int `json:"resolvedCount"`
		ArtistCount   int `json:"artistCount"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.ArtistCount != body.ResolvedCount {
		t.Fatalf("artistCount = %d, want fallback to resolvedCount %d", body.ArtistCount, body.ResolvedCount)
	}
}

func TestCollectionUnauthenticatedReturns401(t *testing.T) {
	cov := &fakeCoverage{}
	srv, _ := coverageTestServer(t, cov, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/collection", nil)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
