package lastfm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/scrobble"
)

// ----------------------------------------------------------------------------
// Test 1 — Signature vector (pins sort+concat+secret order)
// ----------------------------------------------------------------------------

func TestAPISignature(t *testing.T) {
	// Known vector: params {api_key:"k", method:"auth.getToken"}, secret "s"
	// Expected: md5("api_keykmethodauth.getTokens") = fcb68e4c03131d77e8851e889687a441
	const want = "fcb68e4c03131d77e8851e889687a441"

	params := map[string]string{
		"api_key": "k",
		"method":  "auth.getToken",
	}
	got := apiSig(params, "s")
	if got != want {
		t.Errorf("apiSig() = %q; want %q", got, want)
	}
}

// ----------------------------------------------------------------------------
// Test 2 — auth.getToken: parses token from JSON response
// ----------------------------------------------------------------------------

func TestGetToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"token": "tok123"})
	}))
	defer srv.Close()

	a := newTestAdapter(srv.URL)
	token, err := a.getToken(context.Background(), scrobble.Creds{APIKey: "key1", APISecret: "sec1"})
	if err != nil {
		t.Fatalf("getToken: %v", err)
	}
	if token != "tok123" {
		t.Errorf("token = %q; want tok123", token)
	}
}

// ----------------------------------------------------------------------------
// Test 3 — AuthURL: builds the correct redirect URL + returns the token
// ----------------------------------------------------------------------------

func TestAuthURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"token": "mytok"})
	}))
	defer srv.Close()

	a := newTestAdapter(srv.URL)
	authURL, token, err := a.AuthURL(context.Background(), scrobble.Creds{APIKey: "apikey1", APISecret: "apisecret1"})
	if err != nil {
		t.Fatalf("AuthURL: %v", err)
	}
	if token != "mytok" {
		t.Errorf("token = %q; want mytok", token)
	}
	wantURL := "https://www.last.fm/api/auth/?api_key=apikey1&token=mytok"
	if authURL != wantURL {
		t.Errorf("authURL = %q; want %q", authURL, wantURL)
	}
}

// ----------------------------------------------------------------------------
// Test 4 — CompleteAuth: posts signed auth.getSession, parses (key, name)
// ----------------------------------------------------------------------------

func TestCompleteAuth(t *testing.T) {
	var capturedForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		capturedForm = r.Form
		resp := map[string]any{
			"session": map[string]string{
				"key":  "sesskey",
				"name": "testuser",
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	a := newTestAdapter(srv.URL)
	sk, username, err := a.CompleteAuth(context.Background(), scrobble.Creds{APIKey: "ak", APISecret: "as"}, "tok456")
	if err != nil {
		t.Fatalf("CompleteAuth: %v", err)
	}
	if sk != "sesskey" {
		t.Errorf("sessionKey = %q; want sesskey", sk)
	}
	if username != "testuser" {
		t.Errorf("username = %q; want testuser", username)
	}
	// Must have sent a signature
	if capturedForm.Get("api_sig") == "" {
		t.Error("CompleteAuth did not send api_sig")
	}
	if capturedForm.Get("method") != "auth.getSession" {
		t.Errorf("method = %q; want auth.getSession", capturedForm.Get("method"))
	}
}

// ----------------------------------------------------------------------------
// Test 5 — NowPlaying: posts signed track.updateNowPlaying with sk
// ----------------------------------------------------------------------------

func TestNowPlaying(t *testing.T) {
	var capturedForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		capturedForm = r.Form
		json.NewEncoder(w).Encode(map[string]any{"nowplaying": map[string]any{}})
	}))
	defer srv.Close()

	a := newTestAdapter(srv.URL)
	track := scrobble.Track{Title: "Song", Artist: "Band", Album: "Record", DurationMs: 210000}
	err := a.NowPlaying(context.Background(), scrobble.Creds{APIKey: "ak", APISecret: "as", SessionKey: "sk1"}, track)
	if err != nil {
		t.Fatalf("NowPlaying: %v", err)
	}
	if capturedForm.Get("api_sig") == "" {
		t.Error("NowPlaying did not send api_sig")
	}
	if capturedForm.Get("sk") != "sk1" {
		t.Errorf("sk = %q; want sk1", capturedForm.Get("sk"))
	}
	if capturedForm.Get("method") != "track.updateNowPlaying" {
		t.Errorf("method = %q; want track.updateNowPlaying", capturedForm.Get("method"))
	}
}

// ----------------------------------------------------------------------------
// Test 6 — Scrobble: sends indexed params, returns accepted count
// ----------------------------------------------------------------------------

func TestScrobble(t *testing.T) {
	var capturedForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		capturedForm = r.Form
		resp := map[string]any{
			"scrobbles": map[string]any{
				"@attr": map[string]any{"accepted": float64(1)},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	a := newTestAdapter(srv.URL)
	plays := []scrobble.ScrobblePlay{
		{Track: scrobble.Track{Title: "MySong", Artist: "MyBand", Album: "MyAlbum"}, PlayedAt: 1700000000},
	}
	accepted, err := a.Scrobble(context.Background(), scrobble.Creds{APIKey: "ak", APISecret: "as", SessionKey: "sk1"}, plays)
	if err != nil {
		t.Fatalf("Scrobble: %v", err)
	}
	if accepted != 1 {
		t.Errorf("accepted = %d; want 1", accepted)
	}
	if capturedForm.Get("api_sig") == "" {
		t.Error("Scrobble did not send api_sig")
	}
	if capturedForm.Get("sk") != "sk1" {
		t.Errorf("sk = %q; want sk1", capturedForm.Get("sk"))
	}
	if capturedForm.Get("method") != "track.scrobble" {
		t.Errorf("method = %q; want track.scrobble", capturedForm.Get("method"))
	}
	if capturedForm.Get("artist[0]") != "MyBand" {
		t.Errorf("artist[0] = %q; want MyBand", capturedForm.Get("artist[0]"))
	}
	if capturedForm.Get("track[0]") != "MySong" {
		t.Errorf("track[0] = %q; want MySong", capturedForm.Get("track[0]"))
	}
	if capturedForm.Get("timestamp[0]") != "1700000000" {
		t.Errorf("timestamp[0] = %q; want 1700000000", capturedForm.Get("timestamp[0]"))
	}
}

// A composite library credit scrobbles as the primary artist — Last.fm matches
// on a single canonical artist name.
func TestScrobbleSendsPrimaryArtistForCompositeCredit(t *testing.T) {
	var capturedForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		capturedForm = r.Form
		json.NewEncoder(w).Encode(map[string]any{
			"scrobbles": map[string]any{"@attr": map[string]any{"accepted": float64(1)}},
		})
	}))
	defer srv.Close()

	a := newTestAdapter(srv.URL)
	plays := []scrobble.ScrobblePlay{
		{Track: scrobble.Track{Title: "Royalty", Artist: "Egzod; Maestro Chives; Neoni"}, PlayedAt: 1700000001},
	}
	if _, err := a.Scrobble(context.Background(), scrobble.Creds{APIKey: "ak", APISecret: "as", SessionKey: "sk1"}, plays); err != nil {
		t.Fatalf("Scrobble: %v", err)
	}
	if capturedForm.Get("artist[0]") != "Egzod" {
		t.Errorf("artist[0] = %q; want Egzod (primary artist of the composite credit)", capturedForm.Get("artist[0]"))
	}
	// Bare "/" names are single artists and must pass through untouched.
	plays[0].Artist = "AC/DC"
	if _, err := a.Scrobble(context.Background(), scrobble.Creds{APIKey: "ak", APISecret: "as", SessionKey: "sk1"}, plays); err != nil {
		t.Fatalf("Scrobble: %v", err)
	}
	if capturedForm.Get("artist[0]") != "AC/DC" {
		t.Errorf("artist[0] = %q; want AC/DC unsplit", capturedForm.Get("artist[0]"))
	}
}

// ----------------------------------------------------------------------------
// Test 7 — ErrAuth: error code 9 in response body → scrobble.ErrAuth
// ----------------------------------------------------------------------------

func TestErrAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"error":   9,
			"message": "Invalid session key - Please re-authenticate",
		})
	}))
	defer srv.Close()

	a := newTestAdapter(srv.URL)
	track := scrobble.Track{Title: "S", Artist: "A"}
	err := a.NowPlaying(context.Background(), scrobble.Creds{APIKey: "ak", APISecret: "as", SessionKey: "bad"}, track)
	if !isErrAuth(err) {
		t.Errorf("expected ErrAuth, got: %v", err)
	}
}

// TestErrAuthScrobble checks that Scrobble also returns ErrAuth on error code 9.
func TestErrAuthScrobble(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"error":   9,
			"message": "Invalid session key",
		})
	}))
	defer srv.Close()

	a := newTestAdapter(srv.URL)
	plays := []scrobble.ScrobblePlay{
		{Track: scrobble.Track{Title: "S", Artist: "A"}, PlayedAt: 1},
	}
	_, err := a.Scrobble(context.Background(), scrobble.Creds{APIKey: "ak", APISecret: "as", SessionKey: "bad"}, plays)
	if !isErrAuth(err) {
		t.Errorf("expected ErrAuth, got: %v", err)
	}
}

// ----------------------------------------------------------------------------
// Test 8 — ConfigSchema: has api_key (not secret) and api_secret (Secret:true)
// ----------------------------------------------------------------------------

func TestConfigSchema(t *testing.T) {
	a := &Adapter{}
	schema := a.ConfigSchema()
	fields := make(map[string]bool) // key -> Secret
	for _, f := range schema.Fields {
		fields[f.Key] = f.Secret
	}
	if _, ok := fields["api_key"]; !ok {
		t.Error("ConfigSchema missing api_key field")
	}
	if fields["api_key"] {
		t.Error("api_key should NOT be secret")
	}
	if _, ok := fields["api_secret"]; !ok {
		t.Error("ConfigSchema missing api_secret field")
	}
	if !fields["api_secret"] {
		t.Error("api_secret should be Secret:true")
	}
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

// newTestAdapter creates an Adapter pointed at the test server URL.
func newTestAdapter(baseURL string) *Adapter {
	return &Adapter{
		baseURL: strings.TrimRight(baseURL, "/") + "/",
		client:  http.DefaultClient,
	}
}

// isErrAuth unwraps the error chain checking for scrobble.ErrAuth.
func isErrAuth(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), scrobble.ErrAuth.Error())
}
