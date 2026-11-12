package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/auth"
	"github.com/maxjb-xyz/reverb/internal/discord"
	"github.com/maxjb-xyz/reverb/internal/store"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

type fakeDiscordBot struct {
	mu       sync.Mutex
	applied  []string
	online   bool
	unlinked []string
}

func (f *fakeDiscordBot) UserUnlinked(_ context.Context, userID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unlinked = append(f.unlinked, userID)
}

func (f *fakeDiscordBot) Apply(_ context.Context, tok string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applied = append(f.applied, tok)
	f.online = tok != ""
	return nil
}

func (f *fakeDiscordBot) Status() discord.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.online {
		return discord.Status{State: discord.StateOnline, InviteURL: "https://discord.example/invite"}
	}
	return discord.Status{State: discord.StateOff}
}

func (f *fakeDiscordBot) Online() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.online
}

type kv struct{ st *store.Store }

func (k kv) Get(ctx context.Context, key string) (string, error) {
	return k.st.Q().GetSetting(ctx, key)
}
func (k kv) Put(ctx context.Context, key, v string) error {
	return k.st.Q().UpsertSetting(ctx, db.UpsertSettingParams{Key: key, Value: v})
}

func discordServer(t *testing.T) (*Server, *http.Cookie, *store.Store, *fakeDiscordBot, *discord.Links) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/discord.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	authSvc, tok := seededAuthToken(t, st)
	bot := &fakeDiscordBot{}
	links := discord.NewLinks(kv{st}, time.Now)
	srv := NewServer(Deps{Auth: authSvc, Adapters: st.Q(), Discord: bot, DiscordLinks: links})
	return srv, &http.Cookie{Name: sessionCookie, Value: tok}, st, bot, links
}

func TestDiscordTokenIsSavedButNeverReturned(t *testing.T) {
	srv, cookie, _, bot, _ := discordServer(t)
	rec := do(t, srv, cookie, http.MethodPut, "/api/v1/admin/integrations/discord", `{"token":" Bot-secret-token "}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "secret-token") {
		t.Fatal("token echoed back")
	}
	if len(bot.applied) != 1 || bot.applied[0] != "Bot-secret-token" {
		t.Fatalf("applied = %v", bot.applied)
	}
	rec = do(t, srv, cookie, http.MethodGet, "/api/v1/admin/integrations/discord", "")
	var got struct {
		TokenSet bool           `json:"tokenSet"`
		Status   discord.Status `json:"status"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if !got.TokenSet || got.Status.State != discord.StateOnline || strings.Contains(rec.Body.String(), "secret-token") {
		t.Fatalf("GET = %s", rec.Body.String())
	}
	// A blank save reconnects with the saved token; clear removes it.
	do(t, srv, cookie, http.MethodPut, "/api/v1/admin/integrations/discord", `{"token":""}`)
	do(t, srv, cookie, http.MethodPut, "/api/v1/admin/integrations/discord", `{"clear":true}`)
	if bot.applied[1] != "Bot-secret-token" || bot.applied[2] != "" {
		t.Fatalf("applied = %v", bot.applied)
	}
}

func TestDiscordAdminNeedsManageLibrary(t *testing.T) {
	srv, _, st, _, _ := discordServer(t)
	ctx := context.Background()
	authSvc := auth.NewService(st.Q(), time.Now)
	uid, err := authSvc.CreateUser(ctx, "listener", "listener-pass-12345", "role-requester")
	if err != nil {
		t.Fatal(err)
	}
	tok, _ := authSvc.CreateSession(ctx, uid)
	c := &http.Cookie{Name: sessionCookie, Value: tok}
	if rec := do(t, srv, c, http.MethodPut, "/api/v1/admin/integrations/discord", `{"token":"x"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("PUT by a listener = %d", rec.Code)
	}
	// Linking is for everyone.
	if rec := do(t, srv, c, http.MethodGet, "/api/v1/discord/link", ""); rec.Code != http.StatusOK {
		t.Fatalf("GET link = %d", rec.Code)
	}
}

func TestDiscordLinkFlow(t *testing.T) {
	srv, cookie, _, bot, links := discordServer(t)
	if rec := do(t, srv, cookie, http.MethodPost, "/api/v1/discord/link-code", ""); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code while offline = %d", rec.Code)
	}
	_ = bot.Apply(context.Background(), "tok")
	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/discord/link-code", "")
	var code struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &code)
	if len(code.Code) != 9 {
		t.Fatalf("code = %s", rec.Body.String())
	}
	if _, err := links.Redeem(context.Background(), code.Code, "d1", "Dee"); err != nil {
		t.Fatal(err)
	}
	rec = do(t, srv, cookie, http.MethodGet, "/api/v1/discord/link", "")
	if !strings.Contains(rec.Body.String(), `"linked":true`) || !strings.Contains(rec.Body.String(), "Dee") {
		t.Fatalf("link = %s", rec.Body.String())
	}
	if rec := do(t, srv, cookie, http.MethodDelete, "/api/v1/discord/link", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d", rec.Code)
	}
	if l, _ := links.ByDiscord(context.Background(), "d1"); l.ReverbUserID != "" {
		t.Fatalf("still linked: %+v", l)
	}
	bot.mu.Lock()
	defer bot.mu.Unlock()
	if len(bot.unlinked) != 1 || bot.unlinked[0] == "" {
		t.Fatalf("bot not told about the unlink: %v", bot.unlinked)
	}
}
