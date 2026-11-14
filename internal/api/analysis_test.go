package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/analysis"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/store"
)

func TestAnalysisAPI_StatusAndRun(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/analysis.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	authSvc, tok := seededAuthToken(t, st)
	cookie := &http.Cookie{Name: sessionCookie, Value: tok}
	off := NewServer(Deps{Auth: authSvc, Search: registry.NewRegistry("search"), Downloader: registry.NewRegistry("downloader")})
	if rec := do(t, off, cookie, http.MethodGet, "/api/v1/engine/analysis", ""); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no service = %d", rec.Code)
	}
	srv := NewServer(Deps{Auth: authSvc, Search: registry.NewRegistry("search"), Downloader: registry.NewRegistry("downloader"),
		Analysis: analysis.NewService(st.Q(), nil, time.Now)})
	rec := do(t, srv, cookie, http.MethodGet, "/api/v1/engine/analysis", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"enabled":false`) || !strings.Contains(rec.Body.String(), `"total":0`) {
		t.Fatalf("status = %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, srv, cookie, http.MethodPost, "/api/v1/engine/analysis/run", ""); rec.Code != http.StatusAccepted {
		t.Fatalf("run = %d", rec.Code)
	}
}
