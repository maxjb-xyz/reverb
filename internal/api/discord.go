package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/maxjb-xyz/reverb/internal/discord"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// KeyDiscordToken is the settings key of the Discord bot token.
const KeyDiscordToken = "discord:bot_token"

// DiscordBot is the slice of the bot service the API uses.
type DiscordBot interface {
	Apply(ctx context.Context, token string) error
	Status() discord.Status
	Online() bool
	// UserUnlinked stops the bot playing for an account that unlinked.
	UserUnlinked(ctx context.Context, userID string)
}

// handleGetDiscordIntegration serves GET /admin/integrations/discord. The
// token itself is never returned.
func (s *Server) handleGetDiscordIntegration(w http.ResponseWriter, r *http.Request) {
	if s.deps.Discord == nil || s.deps.Adapters == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "discord unavailable"})
		return
	}
	tok, _ := s.deps.Adapters.GetSetting(r.Context(), KeyDiscordToken)
	st := s.deps.Discord.Status()
	writeJSON(w, http.StatusOK, map[string]any{"tokenSet": tok != "" || st.FromEnv, "status": st})
}

type putDiscordBody struct {
	Token string `json:"token"`
	// Clear removes the saved token (and stops the bot).
	Clear bool `json:"clear"`
}

// handlePutDiscordIntegration serves PUT /admin/integrations/discord: save
// (or clear) the bot token and (re)start the bot. It answers once the bot is
// online or has failed, with the status.
func (s *Server) handlePutDiscordIntegration(w http.ResponseWriter, r *http.Request) {
	if s.deps.Discord == nil || s.deps.Adapters == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "discord unavailable"})
		return
	}
	var body putDiscordBody
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	tok := strings.TrimSpace(body.Token)
	if tok == secretSentinel {
		tok = ""
	}
	if tok == "" && !body.Clear {
		// Nothing new: retry with the saved token (a "reconnect" button).
		tok, _ = s.deps.Adapters.GetSetting(r.Context(), KeyDiscordToken)
	} else {
		if len(tok) > 200 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "that doesn't look like a bot token"})
			return
		}
		if err := s.deps.Adapters.UpsertSetting(r.Context(), db.UpsertSettingParams{Key: KeyDiscordToken, Value: tok}); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not save the token"})
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	_ = s.deps.Discord.Apply(ctx, tok) // the status carries any error
	saved, _ := s.deps.Adapters.GetSetting(r.Context(), KeyDiscordToken)
	st := s.deps.Discord.Status()
	writeJSON(w, http.StatusOK, map[string]any{"tokenSet": saved != "" || st.FromEnv, "status": st})
}

// handleGetDiscordLink serves GET /discord/link: is the bot available, and is
// the caller's Discord account linked.
func (s *Server) handleGetDiscordLink(w http.ResponseWriter, r *http.Request) {
	if s.deps.Discord == nil || s.deps.DiscordLinks == nil {
		writeJSON(w, http.StatusOK, map[string]any{"available": false, "linked": false})
		return
	}
	cu, _ := currentUser(r)
	link, err := s.deps.DiscordLinks.ByUser(r.Context(), cu.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not read the link"})
		return
	}
	out := map[string]any{
		"available": s.deps.Discord.Online(),
		"linked":    link.DiscordID != "",
	}
	if link.DiscordID != "" {
		out["discordName"] = link.DiscordName
	}
	writeJSON(w, http.StatusOK, out)
}

// handleCreateDiscordLinkCode serves POST /discord/link-code: a one-time code
// the caller types into Discord with /link.
func (s *Server) handleCreateDiscordLinkCode(w http.ResponseWriter, r *http.Request) {
	if s.deps.Discord == nil || s.deps.DiscordLinks == nil || !s.deps.Discord.Online() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the Discord bot isn't set up on this server"})
		return
	}
	cu, _ := currentUser(r)
	code, exp := s.deps.DiscordLinks.NewCode(cu.ID)
	writeJSON(w, http.StatusOK, map[string]any{"code": code, "expiresAt": exp.Unix()})
}

// handleDeleteDiscordLink serves DELETE /discord/link.
func (s *Server) handleDeleteDiscordLink(w http.ResponseWriter, r *http.Request) {
	if s.deps.DiscordLinks == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	cu, _ := currentUser(r)
	if err := s.deps.DiscordLinks.Unlink(r.Context(), cu.ID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not unlink"})
		return
	}
	if s.deps.Discord != nil {
		s.deps.Discord.UserUnlinked(r.Context(), cu.ID)
	}
	w.WriteHeader(http.StatusNoContent)
}
