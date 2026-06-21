package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/maxjb-xyz/crate/internal/store/db"
	"golang.org/x/crypto/bcrypt"
)

const (
	keyAdminHash    = "admin_password_hash"
	keyAuthDisabled = "auth_disabled"
	sessionTTL      = 30 * 24 * time.Hour
)

func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

func VerifyPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

type Querier interface {
	GetSetting(ctx context.Context, key string) (string, error)
	UpsertSetting(ctx context.Context, arg db.UpsertSettingParams) error
	CreateSession(ctx context.Context, arg db.CreateSessionParams) error
	GetSession(ctx context.Context, tokenHash string) (db.Session, error)
	DeleteSession(ctx context.Context, tokenHash string) error
}

type Service struct {
	q   Querier
	now func() time.Time
}

func NewService(q Querier, now func() time.Time) *Service {
	return &Service{q: q, now: now}
}

func (s *Service) SetAdminPassword(ctx context.Context, pw string) error {
	h, err := HashPassword(pw)
	if err != nil {
		return err
	}
	return s.q.UpsertSetting(ctx, db.UpsertSettingParams{Key: keyAdminHash, Value: h})
}

func (s *Service) IsSetupRequired(ctx context.Context) (bool, error) {
	if v, _ := s.q.GetSetting(ctx, keyAuthDisabled); v == "true" {
		return false, nil
	}
	_, err := s.q.GetSetting(ctx, keyAdminHash)
	if err != nil {
		return true, nil // no row → setup needed
	}
	return false, nil
}

func (s *Service) CheckLogin(ctx context.Context, pw string) (bool, error) {
	h, err := s.q.GetSetting(ctx, keyAdminHash)
	if err != nil {
		return false, errors.New("admin password not set")
	}
	return VerifyPassword(h, pw), nil
}

func hashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

func (s *Service) CreateSession(ctx context.Context) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	tok := base64.RawURLEncoding.EncodeToString(raw)
	err := s.q.CreateSession(ctx, db.CreateSessionParams{
		ID:        uuid.NewString(),
		TokenHash: hashToken(tok),
		ExpiresAt: s.now().Add(sessionTTL).Unix(),
	})
	if err != nil {
		return "", err
	}
	return tok, nil
}

func (s *Service) ValidateToken(ctx context.Context, tok string) (bool, error) {
	if tok == "" {
		return false, nil
	}
	sess, err := s.q.GetSession(ctx, hashToken(tok))
	if err != nil {
		return false, nil
	}
	if sess.ExpiresAt < s.now().Unix() {
		return false, nil
	}
	return true, nil
}

func (s *Service) Logout(ctx context.Context, tok string) error {
	return s.q.DeleteSession(ctx, hashToken(tok))
}

func (s *Service) IsAuthDisabled(ctx context.Context) (bool, error) {
	v, err := s.q.GetSetting(ctx, keyAuthDisabled)
	if err != nil {
		return false, nil // setting absent → not disabled
	}
	return v == "true", nil
}

func (s *Service) SetAuthDisabled(ctx context.Context, disabled bool) error {
	v := "false"
	if disabled {
		v = "true"
	}
	return s.q.UpsertSetting(ctx, db.UpsertSettingParams{Key: keyAuthDisabled, Value: v})
}
