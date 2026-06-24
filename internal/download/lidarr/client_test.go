package lidarr

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// fakeDoer routes requests to canned responses by method+path substring.
type fakeDoer struct {
	routes map[string]string // "METHOD /path" substring → JSON body
	lastBodies map[string]string
}

func (f *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	if f.lastBodies == nil {
		f.lastBodies = map[string]string{}
	}
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		f.lastBodies[req.Method+" "+req.URL.Path] = string(b)
	}
	key := req.Method + " " + req.URL.Path
	// Longest-prefix match so "GET /api/v1/album/lookup" beats "GET /api/v1/album".
	best, bestLen := "", -1
	for k, v := range f.routes {
		if strings.HasPrefix(key, k) && len(k) > bestLen {
			best, bestLen = v, len(k)
		}
	}
	if bestLen >= 0 {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(best)), Header: http.Header{}}, nil
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("[]")), Header: http.Header{}}, nil
}

func TestLookupAlbumParsesResults(t *testing.T) {
	doer := &fakeDoer{routes: map[string]string{
		"GET /api/v1/album/lookup": `[{"title":"Discovery","foreignAlbumId":"mb-album-1","artist":{"artistName":"Daft Punk","foreignArtistId":"mb-artist-1"}}]`,
	}}
	c := NewClient("http://lidarr:8686", "key", doer)
	res, err := c.LookupAlbum(context.Background(), "Daft Punk Discovery")
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].ForeignAlbumID != "mb-album-1" || res[0].Artist.ForeignArtistID != "mb-artist-1" {
		t.Fatalf("lookup = %+v", res)
	}
}

func TestSystemStatusOK(t *testing.T) {
	doer := &fakeDoer{routes: map[string]string{"GET /api/v1/system/status": `{"version":"2.0.0"}`}}
	c := NewClient("http://lidarr:8686", "key", doer)
	if err := c.SystemStatus(context.Background()); err != nil {
		t.Fatalf("SystemStatus: %v", err)
	}
}

func TestSearchAlbumSendsCommand(t *testing.T) {
	doer := &fakeDoer{routes: map[string]string{"POST /api/v1/command": `{"id":1}`}}
	c := NewClient("http://lidarr:8686", "key", doer)
	if err := c.SearchAlbum(context.Background(), 42); err != nil {
		t.Fatal(err)
	}
	body := doer.lastBodies["POST /api/v1/command"]
	if !strings.Contains(body, "AlbumSearch") || !strings.Contains(body, "42") {
		t.Fatalf("command body = %s", body)
	}
}
