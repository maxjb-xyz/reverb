package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/maxjb-xyz/reverb/internal/store/db"
	"golang.org/x/crypto/bcrypt"
)

const (
	keyAdminHash = "admin_password_hash"
	sessionTTL   = 30 * 24 * time.Hour
)

var (
	ErrInvalidCreds = errors.New("invalid credentials")
	ErrUserDisabled = errors.New("user disabled")
)

func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

func VerifyPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// Querier is the persistence slice the auth service needs. *db.Queries satisfies it.
type Querier interface {
	// settings (legacy admin migration + registration policy)
	GetSetting(ctx context.Context, key string) (string, error)
	UpsertSetting(ctx context.Context, arg db.UpsertSettingParams) error
	// sessions
	CreateSession(ctx context.Context, arg db.CreateSessionParams) error
	GetSession(ctx context.Context, tokenHash string) (db.Session, error)
	DeleteSession(ctx context.Context, tokenHash string) error
	BackfillSessionUser(ctx context.Context, userID sql.NullString) error
	// users
	CountUsers(ctx context.Context) (int64, error)
	CreateUser(ctx context.Context, arg db.CreateUserParams) error
	GetUserByID(ctx context.Context, id string) (db.User, error)
	GetUserByUsername(ctx context.Context, username string) (db.User, error)
	TouchUserLastSeen(ctx context.Context, id string) error
	// roles
	GetRole(ctx context.Context, id string) (db.Role, error)
	CreateRole(ctx context.Context, arg db.CreateRoleParams) error
}

type Service struct {
	q   Querier
	now func() time.Time
}

func NewService(q Querier, now func() time.Time) *Service {
	return &Service{q: q, now: now}
}

// CurrentUser is the resolved identity carried through the request context.
type CurrentUser struct {
	ID       string          `json:"id"`
	Username string          `json:"username"`
	RoleID   string          `json:"roleId"`
	RoleName string          `json:"roleName"`
	IsOwner  bool            `json:"isOwner"`
	Caps     map[string]bool `json:"-"`
}

func (u CurrentUser) Has(cap string) bool { return u.Caps[cap] }

// IsSetupRequired reports whether first-run setup is needed (no users yet).
func (s *Service) IsSetupRequired(ctx context.Context) (bool, error) {
	n, err := s.q.CountUsers(ctx)
	if err != nil {
		return false, err
	}
	return n == 0, nil
}

// SetupOwner creates the first user as the owner with the admin role. It fails if
// any user already exists (setup is one-time).
func (s *Service) SetupOwner(ctx context.Context, username, password string) (string, error) {
	n, err := s.q.CountUsers(ctx)
	if err != nil {
		return "", err
	}
	if n > 0 {
		return "", errors.New("setup already complete")
	}
	h, err := HashPassword(password)
	if err != nil {
		return "", err
	}
	id := uuid.NewString()
	if err := s.q.CreateUser(ctx, db.CreateUserParams{
		ID: id, Username: username, PasswordHash: h, RoleID: "role-admin", IsOwner: 1,
	}); err != nil {
		return "", err
	}
	return id, nil
}

// Login verifies username/password (username is case-insensitive) and returns the
// user ID. Returns ErrInvalidCreds for unknown user or wrong password, and
// ErrUserDisabled for a disabled account.
func (s *Service) Login(ctx context.Context, username, password string) (string, error) {
	u, err := s.q.GetUserByUsername(ctx, username)
	if err != nil {
		return "", ErrInvalidCreds
	}
	if u.Disabled == 1 {
		return "", ErrUserDisabled
	}
	if !VerifyPassword(u.PasswordHash, password) {
		return "", ErrInvalidCreds
	}
	return u.ID, nil
}

func hashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

// CreateSession mints a new session token bound to userID.
func (s *Service) CreateSession(ctx context.Context, userID string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	tok := base64.RawURLEncoding.EncodeToString(raw)
	if err := s.q.CreateSession(ctx, db.CreateSessionParams{
		ID:        uuid.NewString(),
		TokenHash: hashToken(tok),
		UserID:    sql.NullString{String: userID, Valid: true},
		ExpiresAt: s.now().Add(sessionTTL).Unix(),
	}); err != nil {
		return "", err
	}
	return tok, nil
}

// ResolveSession validates a session token and returns the current user with
// resolved capabilities. It also bumps the user's last_seen.
func (s *Service) ResolveSession(ctx context.Context, tok string) (CurrentUser, error) {
	if tok == "" {
		return CurrentUser{}, errors.New("no token")
	}
	sess, err := s.q.GetSession(ctx, hashToken(tok))
	if err != nil || sess.ExpiresAt < s.now().Unix() || !sess.UserID.Valid {
		return CurrentUser{}, errors.New("invalid session")
	}
	u, err := s.q.GetUserByID(ctx, sess.UserID.String)
	if err != nil || u.Disabled == 1 {
		return CurrentUser{}, errors.New("invalid user")
	}
	caps, roleName, err := s.resolveCaps(ctx, u.RoleID)
	if err != nil {
		return CurrentUser{}, err
	}
	_ = s.q.TouchUserLastSeen(ctx, u.ID)
	return CurrentUser{
		ID:       u.ID,
		Username: u.Username,
		RoleID:   u.RoleID,
		RoleName: roleName,
		IsOwner:  u.IsOwner == 1,
		Caps:     caps,
	}, nil
}

// resolveCaps loads a role's capability set and display name.
func (s *Service) resolveCaps(ctx context.Context, roleID string) (map[string]bool, string, error) {
	r, err := s.q.GetRole(ctx, roleID)
	if err != nil {
		return nil, "", err
	}
	var keys []string
	if err := json.Unmarshal([]byte(r.Capabilities), &keys); err != nil {
		return nil, "", err
	}
	m := make(map[string]bool, len(keys))
	for _, k := range keys {
		m[k] = true
	}
	return m, r.Name, nil
}

func (s *Service) Logout(ctx context.Context, tok string) error {
	return s.q.DeleteSession(ctx, hashToken(tok))
}

// EnsureSeed is idempotent. It seeds the system roles and registration-policy
// defaults, and migrates a legacy single-admin install (an existing
// admin_password_hash setting with no users) into the owner account, back-filling
// any null-user sessions to that owner. Safe to call on every startup.
func (s *Service) EnsureSeed(ctx context.Context) error {
	// 1. system roles (idempotent — skip any that exist)
	for _, sr := range DefaultSystemRoles() {
		if _, err := s.q.GetRole(ctx, sr.ID); err == nil {
			continue
		}
		caps, _ := json.Marshal(sr.Capabilities)
		sys := int64(0)
		if sr.IsSystem {
			sys = 1
		}
		if err := s.q.CreateRole(ctx, db.CreateRoleParams{ID: sr.ID, Name: sr.Name, IsSystem: sys, Capabilities: string(caps)}); err != nil {
			return err
		}
	}
	// 2. registration-policy defaults (only if absent)
	for k, v := range map[string]string{"signup_enabled": "false", "invites_enabled": "false", "default_role_id": "role-user"} {
		if _, err := s.q.GetSetting(ctx, k); err != nil {
			if err := s.q.UpsertSetting(ctx, db.UpsertSettingParams{Key: k, Value: v}); err != nil {
				return err
			}
		}
	}
	// 3. migrate single-admin install → owner
	n, err := s.q.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		if h, err := s.q.GetSetting(ctx, keyAdminHash); err == nil && h != "" {
			id := uuid.NewString()
			if err := s.q.CreateUser(ctx, db.CreateUserParams{ID: id, Username: "admin", PasswordHash: h, RoleID: "role-admin", IsOwner: 1}); err != nil {
				return err
			}
			if err := s.q.BackfillSessionUser(ctx, sql.NullString{String: id, Valid: true}); err != nil {
				return err
			}
		}
	}
	return nil
}
