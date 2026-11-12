package discord

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Bot states as the admin page shows them.
const (
	StateOff        = "off"        // no token
	StateConnecting = "connecting" // token set, logging in
	StateOnline     = "online"
	StateError      = "error"
)

// Status is the bot's state for the admin page.
type Status struct {
	State     string `json:"state"`
	BotName   string `json:"botName,omitempty"`
	InviteURL string `json:"inviteUrl,omitempty"`
	Error     string `json:"error,omitempty"`
	// Sessions is how many voice channels the bot is playing in.
	Sessions int `json:"sessions"`
	// FromEnv is true when the token comes from REVERB_DISCORD_TOKEN (the
	// admin page can't change it then).
	FromEnv bool `json:"fromEnv"`
}

// Service runs the Discord bot when a token is configured and restarts it
// when the token changes. It is optional: with no token it does nothing.
type Service struct {
	Core *Core
	Log  *slog.Logger
	// EnvToken, when set, wins over the token saved in settings.
	EnvToken string

	// botName is set from Discord's Ready event; kept apart from mu so the
	// event never waits on an Apply that is closing the client.
	botName atomic.Value

	mu sync.Mutex
	// gen counts Apply calls; only the latest one may install its bot.
	gen    int
	bot    *gatewayBot
	token  string
	status Status
	cancel context.CancelFunc
}

// Apply (re)starts the bot with token (the saved one; EnvToken wins). An
// empty token stops it. It returns once the bot has logged in or failed.
func (s *Service) Apply(ctx context.Context, token string) error {
	fromEnv := s.EnvToken != ""
	if fromEnv {
		token = s.EnvToken
	}
	token = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(token), "Bot "))

	s.mu.Lock()
	if token == s.token && s.bot != nil && s.status.State == StateOnline {
		s.mu.Unlock()
		return nil
	}
	s.gen++
	gen := s.gen
	s.stopLocked()
	s.token = token
	s.status = Status{State: StateOff, FromEnv: fromEnv}
	if token == "" {
		s.mu.Unlock()
		return nil
	}
	s.status.State = StateConnecting
	s.mu.Unlock()

	log := s.Log
	if log == nil {
		log = slog.Default()
	}
	b, err := newGatewayBot(token, s.Core, log, func(name string) { s.botName.Store(name) })
	if err == nil {
		openCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		err = b.open(openCtx)
		cancel()
		if err != nil {
			closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			b.client.Close(closeCtx)
			cancel()
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gen != gen {
		// A later Apply (or Stop) took over meanwhile.
		if err == nil {
			go b.client.Close(context.Background())
		}
		return nil
	}
	if err != nil {
		s.status.State = StateError
		s.status.Error = friendlyError(err)
		return err
	}
	s.bot = b
	s.Core.SetVoice(b)
	s.status.State = StateOnline
	s.status.InviteURL = InviteURL(b.appID())
	runCtx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	go s.sweep(runCtx)
	return nil
}

func friendlyError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "401") || strings.Contains(strings.ToLower(msg), "unauthorized") || strings.Contains(msg, "4004"):
		return "Discord rejected the token. Copy it again from the Developer Portal (Bot > Reset Token)."
	case strings.Contains(msg, "4014") || strings.Contains(strings.ToLower(msg), "disallowed intent"):
		return "Discord refused the bot's permissions. No privileged intents are needed; check the application settings."
	case errors.Is(err, context.DeadlineExceeded):
		return "Timed out connecting to Discord."
	}
	return msg
}

func (s *Service) sweep(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			s.Core.SweepIdle(ctx, now)
		}
	}
}

// UserUnlinked stops the bot playing for a Reverb account that unlinked
// Discord (from the web page).
func (s *Service) UserUnlinked(ctx context.Context, userID string) {
	if s.Core != nil {
		s.Core.LeaveUser(ctx, userID)
	}
}

func (s *Service) stopLocked() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if s.bot != nil {
		b := s.bot
		s.bot = nil
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		b.close(ctx)
		cancel()
		s.Core.SetVoice(nil)
	}
}

// Stop shuts the bot down.
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gen++
	s.stopLocked()
	s.token = ""
	s.status = Status{State: StateOff, FromEnv: s.EnvToken != ""}
}

// Status reports the bot's state.
func (s *Service) Status() Status {
	s.mu.Lock()
	st := s.status
	s.mu.Unlock()
	if name, ok := s.botName.Load().(string); ok && st.State == StateOnline {
		st.BotName = name
	}
	if s.Core != nil {
		st.Sessions = s.Core.Sessions()
	}
	return st
}

// Online reports whether linking and commands work right now.
func (s *Service) Online() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status.State == StateOnline
}
