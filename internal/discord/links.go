package discord

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
)

// Settings is the key/value store links live in (the settings table).
type Settings interface {
	Get(ctx context.Context, key string) (string, error)
	// Put with "" clears a key.
	Put(ctx context.Context, key, value string) error
}

// Link ties a Discord account to a Reverb account.
type Link struct {
	ReverbUserID string `json:"reverbUserId"`
	DiscordID    string `json:"discordId"`
	DiscordName  string `json:"discordName"`
	LinkedAt     int64  `json:"linkedAt"`
}

// codeTTL is how long a link code stays good.
const codeTTL = 10 * time.Minute

// maxRedeemFailures per Discord user within codeTTL before redeeming pauses.
const maxRedeemFailures = 5

// Links issues one-time link codes in Reverb and redeems them from Discord.
// The code is shown only to the signed-in Reverb user, who types it into
// Discord, so whoever redeems it proves they hold both accounts.
type Links struct {
	store Settings
	now   func() time.Time

	mu       sync.Mutex
	codes    map[string]pendingCode // code -> pending
	failures map[string][]time.Time // discord id -> failed attempts
}

type pendingCode struct {
	userID  string
	expires time.Time
}

// NewLinks makes the link registry.
func NewLinks(store Settings, now func() time.Time) *Links {
	if now == nil {
		now = time.Now
	}
	return &Links{store: store, now: now, codes: map[string]pendingCode{}, failures: map[string][]time.Time{}}
}

func linkKeyDiscord(discordID string) string { return "discord:link:" + discordID }
func linkKeyUser(userID string) string       { return "discord:user:" + userID }

// codeAlphabet leaves out look-alikes (0/O, 1/I/L).
const codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

func newCode() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	out := make([]byte, 0, 9)
	for i, v := range b {
		if i == 4 {
			out = append(out, '-')
		}
		out = append(out, codeAlphabet[int(v)%len(codeAlphabet)])
	}
	return string(out)
}

// NewCode issues a code for userID, replacing any earlier one.
func (l *Links) NewCode(userID string) (string, time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	for c, p := range l.codes {
		if p.userID == userID || now.After(p.expires) {
			delete(l.codes, c)
		}
	}
	code := newCode()
	exp := now.Add(codeTTL)
	l.codes[code] = pendingCode{userID: userID, expires: exp}
	return code, exp
}

// Errors Redeem returns.
var (
	ErrBadCode      = errors.New("that code is wrong or has expired")
	ErrTooManyTries = errors.New("too many wrong codes; wait a few minutes")
)

// Redeem links discordID to the account that issued code.
func (l *Links) Redeem(ctx context.Context, code, discordID, discordName string) (Link, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if len(code) == 8 && !strings.Contains(code, "-") {
		code = code[:4] + "-" + code[4:]
	}
	l.mu.Lock()
	now := l.now()
	recent := l.failures[discordID][:0]
	for _, t := range l.failures[discordID] {
		if now.Sub(t) < codeTTL {
			recent = append(recent, t)
		}
	}
	l.failures[discordID] = recent
	if len(recent) >= maxRedeemFailures {
		l.mu.Unlock()
		return Link{}, ErrTooManyTries
	}
	p, ok := l.codes[code]
	if !ok || now.After(p.expires) {
		l.failures[discordID] = append(l.failures[discordID], now)
		l.mu.Unlock()
		return Link{}, ErrBadCode
	}
	delete(l.codes, code)
	delete(l.failures, discordID)
	l.mu.Unlock()

	link := Link{ReverbUserID: p.userID, DiscordID: discordID, DiscordName: discordName, LinkedAt: now.Unix()}
	// One Discord account per Reverb account and the other way round: drop
	// whatever either side was linked to before.
	if old, err := l.ByUser(ctx, p.userID); err == nil && old.DiscordID != "" && old.DiscordID != discordID {
		_ = l.store.Put(ctx, linkKeyDiscord(old.DiscordID), "")
	}
	if old, err := l.ByDiscord(ctx, discordID); err == nil && old.ReverbUserID != "" && old.ReverbUserID != p.userID {
		_ = l.store.Put(ctx, linkKeyUser(old.ReverbUserID), "")
	}
	b, _ := json.Marshal(link)
	if err := l.store.Put(ctx, linkKeyDiscord(discordID), string(b)); err != nil {
		return Link{}, err
	}
	if err := l.store.Put(ctx, linkKeyUser(p.userID), string(b)); err != nil {
		return Link{}, err
	}
	return link, nil
}

func (l *Links) read(ctx context.Context, key string) (Link, error) {
	raw, err := l.store.Get(ctx, key)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && raw == "") {
		return Link{}, nil
	}
	if err != nil {
		return Link{}, err
	}
	var link Link
	if json.Unmarshal([]byte(raw), &link) != nil {
		return Link{}, nil
	}
	return link, nil
}

// ByDiscord returns the link of a Discord account (zero Link when none).
func (l *Links) ByDiscord(ctx context.Context, discordID string) (Link, error) {
	link, err := l.read(ctx, linkKeyDiscord(discordID))
	if err != nil || link.ReverbUserID == "" {
		return link, err
	}
	// Both halves must agree; a half-written or superseded link is no link.
	back, err := l.read(ctx, linkKeyUser(link.ReverbUserID))
	if err != nil || back.DiscordID != discordID {
		return Link{}, err
	}
	return link, nil
}

// ByUser returns the link of a Reverb account (zero Link when none).
func (l *Links) ByUser(ctx context.Context, userID string) (Link, error) {
	return l.read(ctx, linkKeyUser(userID))
}

// Unlink removes userID's link.
func (l *Links) Unlink(ctx context.Context, userID string) error {
	link, err := l.ByUser(ctx, userID)
	if err != nil {
		return err
	}
	if link.DiscordID != "" {
		if err := l.store.Put(ctx, linkKeyDiscord(link.DiscordID), ""); err != nil {
			return err
		}
	}
	return l.store.Put(ctx, linkKeyUser(userID), "")
}
