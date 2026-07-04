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
	ErrInvalidCreds   = errors.New("invalid credentials")
	ErrUserDisabled   = errors.New("user disabled")
	ErrUsernameTaken  = errors.New("username taken")
	ErrOwnerProtected = errors.New("owner account is protected")
	ErrUserNotFound   = errors.New("user not found")
	ErrRoleNotFound   = errors.New("role not found")
	ErrRoleInUse      = errors.New("role is assigned to users")
	ErrLastAdmin      = errors.New("would leave no administrator")
	ErrRoleIsDefault  = errors.New("role is the registration default")
	ErrSignupDisabled = errors.New("signup disabled")
	ErrInviteInvalid  = errors.New("invite invalid")
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
	DeleteSessionsForUserExcept(ctx context.Context, arg db.DeleteSessionsForUserExceptParams) error
	BackfillSessionUser(ctx context.Context, userID sql.NullString) error
	// users
	CountUsers(ctx context.Context) (int64, error)
	CreateUser(ctx context.Context, arg db.CreateUserParams) error
	GetUserByID(ctx context.Context, id string) (db.User, error)
	GetUserByUsername(ctx context.Context, username string) (db.User, error)
	ListUsers(ctx context.Context) ([]db.User, error)
	UpdateUserRole(ctx context.Context, arg db.UpdateUserRoleParams) error
	SetUserDisabled(ctx context.Context, arg db.SetUserDisabledParams) error
	DeleteUser(ctx context.Context, id string) error
	TouchUserLastSeen(ctx context.Context, id string) error
	SetUserPassword(ctx context.Context, arg db.SetUserPasswordParams) error
	// roles
	GetRole(ctx context.Context, id string) (db.Role, error)
	ListRoles(ctx context.Context) ([]db.Role, error)
	CreateRole(ctx context.Context, arg db.CreateRoleParams) error
	UpdateRole(ctx context.Context, arg db.UpdateRoleParams) error
	DeleteRole(ctx context.Context, id string) error
	CountUsersWithRole(ctx context.Context, roleID string) (int64, error)
	// synced playlists (legacy back-fill only)
	BackfillSyncedPlaylistOwners(ctx context.Context, ownerUserID sql.NullString) error
	// invites
	CreateInvite(ctx context.Context, arg db.CreateInviteParams) error
	GetInviteByCode(ctx context.Context, code string) (db.Invite, error)
	ListInvites(ctx context.Context) ([]db.Invite, error)
	MarkInviteUsed(ctx context.Context, arg db.MarkInviteUsedParams) error
	DeleteInvite(ctx context.Context, id string) error
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
	ID        string          `json:"id"`
	Username  string          `json:"username"`
	RoleID    string          `json:"roleId"`
	RoleName  string          `json:"roleName"`
	IsOwner   bool            `json:"isOwner"`
	CreatedAt int64           `json:"createdAt"`
	Caps      map[string]bool `json:"-"`
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
	if err := ValidatePassword(password); err != nil {
		return "", err
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
		ID:        u.ID,
		Username:  u.Username,
		RoleID:    u.RoleID,
		RoleName:  roleName,
		IsOwner:   u.IsOwner == 1,
		CreatedAt: u.CreatedAt,
		Caps:      caps,
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

// ChangeOwnPassword verifies the current password then replaces it with next.
// Returns ErrInvalidCreds when current is wrong.
func (s *Service) ChangeOwnPassword(ctx context.Context, userID, current, next string) error {
	u, err := s.q.GetUserByID(ctx, userID)
	if err != nil {
		return ErrInvalidCreds
	}
	if !VerifyPassword(u.PasswordHash, current) {
		return ErrInvalidCreds
	}
	if err := ValidatePassword(next); err != nil {
		return err
	}
	h, err := HashPassword(next)
	if err != nil {
		return err
	}
	return s.q.SetUserPassword(ctx, db.SetUserPasswordParams{PasswordHash: h, ID: userID})
}

// LogoutAll deletes all sessions for userID except the one identified by exceptToken.
func (s *Service) LogoutAll(ctx context.Context, userID, exceptToken string) error {
	return s.q.DeleteSessionsForUserExcept(ctx, db.DeleteSessionsForUserExceptParams{
		UserID:    sql.NullString{String: userID, Valid: true},
		TokenHash: hashToken(exceptToken),
	})
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
			// Back-fill any pre-existing (legacy) playlists to the new owner so the
			// owner-scoped API list includes them. Only touches NULL owners, so it's
			// idempotent and safe even if some rows already carry an owner.
			if err := s.q.BackfillSyncedPlaylistOwners(ctx, sql.NullString{String: id, Valid: true}); err != nil {
				return err
			}
		}
	}
	// Idempotent capability remap for installs seeded before the Seerr-style
	// rename. Rewrites old keys and brings role-requester to its new definition.
	roles, err := s.q.ListRoles(ctx)
	if err != nil {
		return err
	}
	for _, r := range roles {
		var caps []string
		if err := json.Unmarshal([]byte(r.Capabilities), &caps); err != nil {
			continue
		}
		changed := false
		for i, c := range caps {
			switch c {
			case "can_download":
				caps[i], changed = CapAutoApprove, true
			case "can_request":
				caps[i], changed = CapRequest, true
			}
		}
		// role-requester gains create_playlists (its old def lacked it)
		if r.ID == "role-requester" {
			has := false
			for _, c := range caps {
				if c == CapCreatePlaylists {
					has = true
				}
			}
			if !has {
				caps, changed = append(caps, CapCreatePlaylists), true
			}
		}
		// role-admin gains manage_requests (added in SP2)
		if r.ID == "role-admin" {
			has := false
			for _, c := range caps {
				if c == CapManageRequests {
					has = true
				}
			}
			if !has {
				caps, changed = append(caps, CapManageRequests), true
			}
		}
		if changed {
			b, _ := json.Marshal(caps)
			if err := s.q.UpdateRole(ctx, db.UpdateRoleParams{Name: r.Name, Capabilities: string(b), ID: r.ID}); err != nil {
				return err
			}
		}
	}
	return nil
}

// UserView is the admin-safe projection of a user row (no password hash).
type UserView struct {
	ID        string  `json:"id"`
	Username  string  `json:"username"`
	RoleID    string  `json:"roleId"`
	RoleName  string  `json:"roleName"`
	IsOwner   bool    `json:"isOwner"`
	Disabled  bool    `json:"disabled"`
	CreatedAt int64   `json:"createdAt"`
	LastSeen  *int64  `json:"lastSeen"`
}

// ListUsers returns all users with resolved role names.
func (s *Service) ListUsers(ctx context.Context) ([]UserView, error) {
	rows, err := s.q.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]UserView, 0, len(rows))
	for _, u := range rows {
		r, err := s.q.GetRole(ctx, u.RoleID)
		roleName := u.RoleID
		if err == nil {
			roleName = r.Name
		}
		var lastSeen *int64
		if u.LastSeen.Valid {
			v := u.LastSeen.Int64
			lastSeen = &v
		}
		out = append(out, UserView{
			ID:        u.ID,
			Username:  u.Username,
			RoleID:    u.RoleID,
			RoleName:  roleName,
			IsOwner:   u.IsOwner == 1,
			Disabled:  u.Disabled == 1,
			CreatedAt: u.CreatedAt,
			LastSeen:  lastSeen,
		})
	}
	return out, nil
}

// CreateUser creates a new non-owner user with the given role.
// Returns ErrRoleNotFound if roleID doesn't exist, ErrUsernameTaken if taken.
func (s *Service) CreateUser(ctx context.Context, username, password, roleID string) (string, error) {
	if _, err := s.q.GetRole(ctx, roleID); err != nil {
		return "", ErrRoleNotFound
	}
	if _, err := s.q.GetUserByUsername(ctx, username); err == nil {
		return "", ErrUsernameTaken
	}
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	h, err := HashPassword(password)
	if err != nil {
		return "", err
	}
	id := uuid.NewString()
	return id, s.q.CreateUser(ctx, db.CreateUserParams{ID: id, Username: username, PasswordHash: h, RoleID: roleID, IsOwner: 0})
}

// UpdateUserRole changes a user's role. Returns ErrOwnerProtected if trying to
// demote the owner to a non-admin role, ErrRoleNotFound for unknown roleID or
// userID, and ErrLastAdmin if the change would leave no enabled administrator.
func (s *Service) UpdateUserRole(ctx context.Context, id, roleID string) error {
	u, err := s.q.GetUserByID(ctx, id)
	if err != nil {
		return ErrUserNotFound
	}
	if u.IsOwner == 1 && roleID != "role-admin" {
		return ErrOwnerProtected
	}
	r, err := s.q.GetRole(ctx, roleID)
	if err != nil {
		return ErrRoleNotFound
	}
	newHasAdmin := false
	var caps []string
	_ = json.Unmarshal([]byte(r.Capabilities), &caps)
	for _, c := range caps {
		if c == CapAdmin {
			newHasAdmin = true
		}
	}
	if !newHasAdmin {
		admins, err := s.enabledAdminUserIDs(ctx)
		if err != nil {
			return err
		}
		if _, isAdmin := admins[id]; isAdmin && len(admins) == 1 {
			return ErrLastAdmin
		}
	}
	return s.q.UpdateUserRole(ctx, db.UpdateUserRoleParams{RoleID: roleID, ID: id})
}

// SetUserDisabled enables or disables a user account. Returns ErrOwnerProtected
// if the target is the owner, and ErrLastAdmin if disabling would leave no
// enabled administrator.
func (s *Service) SetUserDisabled(ctx context.Context, id string, disabled bool) error {
	u, err := s.q.GetUserByID(ctx, id)
	if err != nil {
		return ErrUserNotFound
	}
	if u.IsOwner == 1 {
		return ErrOwnerProtected
	}
	if disabled {
		admins, err := s.enabledAdminUserIDs(ctx)
		if err != nil {
			return err
		}
		if _, isAdmin := admins[id]; isAdmin && len(admins) == 1 {
			return ErrLastAdmin
		}
	}
	v := int64(0)
	if disabled {
		v = 1
	}
	return s.q.SetUserDisabled(ctx, db.SetUserDisabledParams{Disabled: v, ID: id})
}

// AdminSetPassword resets a user's password without requiring the current one.
func (s *Service) AdminSetPassword(ctx context.Context, id, password string) error {
	if err := ValidatePassword(password); err != nil {
		return err
	}
	h, err := HashPassword(password)
	if err != nil {
		return err
	}
	return s.q.SetUserPassword(ctx, db.SetUserPasswordParams{PasswordHash: h, ID: id})
}

// DeleteUser removes a user. Returns ErrOwnerProtected if the target is the
// owner, and ErrLastAdmin if deletion would leave no enabled administrator.
func (s *Service) DeleteUser(ctx context.Context, id string) error {
	u, err := s.q.GetUserByID(ctx, id)
	if err != nil {
		return ErrUserNotFound
	}
	if u.IsOwner == 1 {
		return ErrOwnerProtected
	}
	admins, err := s.enabledAdminUserIDs(ctx)
	if err != nil {
		return err
	}
	if _, isAdmin := admins[id]; isAdmin && len(admins) == 1 {
		return ErrLastAdmin
	}
	return s.q.DeleteUser(ctx, id)
}

// RoleView is the admin-safe projection of a role row.
type RoleView struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	IsSystem     bool     `json:"isSystem"`
	Capabilities []string `json:"capabilities"`
}

// ListRoles returns all roles with decoded capability slices.
func (s *Service) ListRoles(ctx context.Context) ([]RoleView, error) {
	rows, err := s.q.ListRoles(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]RoleView, 0, len(rows))
	for _, r := range rows {
		var caps []string
		if err := json.Unmarshal([]byte(r.Capabilities), &caps); err != nil {
			caps = []string{}
		}
		out = append(out, RoleView{
			ID:           r.ID,
			Name:         r.Name,
			IsSystem:     r.IsSystem == 1,
			Capabilities: caps,
		})
	}
	return out, nil
}

// enabledAdminUserIDs returns userID->roleID for every ENABLED user whose role grants is_admin.
func (s *Service) enabledAdminUserIDs(ctx context.Context) (map[string]string, error) {
	roles, err := s.q.ListRoles(ctx)
	if err != nil {
		return nil, err
	}
	adminRole := map[string]bool{}
	for _, r := range roles {
		var caps []string
		_ = json.Unmarshal([]byte(r.Capabilities), &caps)
		for _, c := range caps {
			if c == CapAdmin {
				adminRole[r.ID] = true
				break
			}
		}
	}
	users, err := s.q.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, u := range users {
		if u.Disabled == 0 && adminRole[u.RoleID] {
			out[u.ID] = u.RoleID
		}
	}
	return out, nil
}

// normalizeCaps appends request when auto_approve is present (auto_approve implies request).
func normalizeCaps(caps []string) []string {
	hasAuto, hasReq := false, false
	for _, c := range caps {
		if c == CapAutoApprove {
			hasAuto = true
		}
		if c == CapRequest {
			hasReq = true
		}
	}
	if hasAuto && !hasReq {
		caps = append(caps, CapRequest)
	}
	return caps
}

// CreateRole creates a new custom role. Returns ErrInvalidCapability for unknown caps.
func (s *Service) CreateRole(ctx context.Context, name string, caps []string) (string, error) {
	if err := ValidateCapabilities(caps); err != nil {
		return "", err
	}
	caps = normalizeCaps(caps)
	b, _ := json.Marshal(caps)
	id := "role-" + uuid.NewString()
	return id, s.q.CreateRole(ctx, db.CreateRoleParams{ID: id, Name: name, IsSystem: 0, Capabilities: string(b)})
}

// UpdateRole updates name and capabilities of any role (including default roles).
// Returns ErrInvalidCapability for unknown caps, ErrLastAdmin if the edit would
// leave no enabled administrator.
func (s *Service) UpdateRole(ctx context.Context, id, name string, caps []string) error {
	if _, err := s.q.GetRole(ctx, id); err != nil {
		return ErrRoleNotFound
	}
	if err := ValidateCapabilities(caps); err != nil {
		return err
	}
	caps = normalizeCaps(caps)
	// anti-lockout: if this edit removes is_admin and would leave no admins, reject
	newHasAdmin := false
	for _, c := range caps {
		if c == CapAdmin {
			newHasAdmin = true
		}
	}
	if !newHasAdmin {
		admins, err := s.enabledAdminUserIDs(ctx)
		if err != nil {
			return err
		}
		remaining := 0
		for _, roleID := range admins {
			if roleID != id {
				remaining++
			}
		}
		if len(admins) > 0 && remaining == 0 {
			return ErrLastAdmin
		}
	}
	b, _ := json.Marshal(caps)
	return s.q.UpdateRole(ctx, db.UpdateRoleParams{Name: name, Capabilities: string(b), ID: id})
}

// DeleteRole removes a role. Returns ErrRoleInUse if any user is assigned to it,
// ErrRoleIsDefault if the role is the current registration default.
func (s *Service) DeleteRole(ctx context.Context, id string) error {
	if _, err := s.q.GetRole(ctx, id); err != nil {
		return nil // not found; no-op
	}
	n, err := s.q.CountUsersWithRole(ctx, id)
	if err != nil {
		return err
	}
	if n > 0 {
		return ErrRoleInUse
	}
	if pol, err := s.GetRegPolicy(ctx); err == nil && pol.DefaultRoleID == id {
		return ErrRoleIsDefault
	}
	return s.q.DeleteRole(ctx, id)
}

// RegPolicy holds the current registration policy settings.
type RegPolicy struct {
	SignupEnabled  bool   `json:"signupEnabled"`
	InvitesEnabled bool   `json:"invitesEnabled"`
	DefaultRoleID  string `json:"defaultRoleId"`
}

// GetRegPolicy reads the three registration policy settings.
func (s *Service) GetRegPolicy(ctx context.Context) (RegPolicy, error) {
	seStr, err := s.q.GetSetting(ctx, "signup_enabled")
	if err != nil {
		return RegPolicy{}, err
	}
	invStr, err := s.q.GetSetting(ctx, "invites_enabled")
	if err != nil {
		return RegPolicy{}, err
	}
	roleID, err := s.q.GetSetting(ctx, "default_role_id")
	if err != nil {
		return RegPolicy{}, err
	}
	return RegPolicy{
		SignupEnabled:  seStr == "true",
		InvitesEnabled: invStr == "true",
		DefaultRoleID:  roleID,
	}, nil
}

// SetRegPolicy persists the three registration policy settings.
func (s *Service) SetRegPolicy(ctx context.Context, pol RegPolicy) error {
	se := "false"
	if pol.SignupEnabled {
		se = "true"
	}
	inv := "false"
	if pol.InvitesEnabled {
		inv = "true"
	}
	for k, v := range map[string]string{
		"signup_enabled":  se,
		"invites_enabled": inv,
		"default_role_id": pol.DefaultRoleID,
	} {
		if err := s.q.UpsertSetting(ctx, db.UpsertSettingParams{Key: k, Value: v}); err != nil {
			return err
		}
	}
	return nil
}

// Signup registers a new user according to the current registration policy.
// It returns ErrSignupDisabled or ErrInviteInvalid when policy gates are not met,
// and ErrUsernameTaken when the username is already taken.
func (s *Service) Signup(ctx context.Context, username, password, inviteCode string) (string, error) {
	pol, err := s.GetRegPolicy(ctx)
	if err != nil {
		return "", err
	}
	roleID := pol.DefaultRoleID
	var inviteID string
	if inviteCode != "" {
		if !pol.InvitesEnabled {
			return "", ErrInviteInvalid
		}
		inv, err := s.q.GetInviteByCode(ctx, inviteCode)
		if err != nil || inv.UsedAt.Valid || (inv.ExpiresAt.Valid && inv.ExpiresAt.Int64 < s.now().Unix()) {
			return "", ErrInviteInvalid
		}
		inviteID = inv.ID
		if inv.RoleID.Valid {
			roleID = inv.RoleID.String
		}
	} else if !pol.SignupEnabled {
		return "", ErrSignupDisabled
	}
	if _, err := s.q.GetUserByUsername(ctx, username); err == nil {
		return "", ErrUsernameTaken
	}
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	h, err := HashPassword(password)
	if err != nil {
		return "", err
	}
	id := uuid.NewString()
	if err := s.q.CreateUser(ctx, db.CreateUserParams{ID: id, Username: username, PasswordHash: h, RoleID: roleID, IsOwner: 0}); err != nil {
		return "", err
	}
	if inviteID != "" {
		if err := s.q.MarkInviteUsed(ctx, db.MarkInviteUsedParams{UsedBy: sql.NullString{String: id, Valid: true}, ID: inviteID}); err != nil {
			return "", err
		}
	}
	return id, nil
}

// InviteView is the admin-safe projection of an invite row.
type InviteView struct {
	ID        string  `json:"id"`
	Code      string  `json:"code"`
	RoleID    *string `json:"roleId"`
	RoleName  *string `json:"roleName"`
	CreatedBy *string `json:"createdBy"`
	ExpiresAt *int64  `json:"expiresAt"`
	UsedBy    *string `json:"usedBy"`
	UsedAt    *int64  `json:"usedAt"`
	CreatedAt int64   `json:"createdAt"`
}

// CreateInvite generates a random invite code and inserts it into the database.
// It returns the new invite's id and code.
func (s *Service) CreateInvite(ctx context.Context, roleID *string, expiresAt *int64, createdBy string) (id, code string, err error) {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	code = base64.RawURLEncoding.EncodeToString(raw)
	id = uuid.NewString()

	var dbRoleID sql.NullString
	if roleID != nil {
		dbRoleID = sql.NullString{String: *roleID, Valid: true}
	}
	var dbExpiresAt sql.NullInt64
	if expiresAt != nil {
		dbExpiresAt = sql.NullInt64{Int64: *expiresAt, Valid: true}
	}
	var dbCreatedBy sql.NullString
	if createdBy != "" {
		dbCreatedBy = sql.NullString{String: createdBy, Valid: true}
	}

	if err := s.q.CreateInvite(ctx, db.CreateInviteParams{
		ID:        id,
		Code:      code,
		RoleID:    dbRoleID,
		CreatedBy: dbCreatedBy,
		ExpiresAt: dbExpiresAt,
	}); err != nil {
		return "", "", err
	}
	return id, code, nil
}

// ListInvites returns all invite rows as views.
func (s *Service) ListInvites(ctx context.Context) ([]InviteView, error) {
	rows, err := s.q.ListInvites(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]InviteView, 0, len(rows))
	for _, r := range rows {
		v := InviteView{
			ID:        r.ID,
			Code:      r.Code,
			CreatedAt: r.CreatedAt,
		}
		if r.RoleID.Valid {
			roleIDStr := r.RoleID.String
			v.RoleID = &roleIDStr
			// Resolve role name the same way ListUsers does.
			if role, err := s.q.GetRole(ctx, roleIDStr); err == nil {
				n := role.Name
				v.RoleName = &n
			}
		}
		if r.CreatedBy.Valid {
			s := r.CreatedBy.String
			v.CreatedBy = &s
		}
		if r.ExpiresAt.Valid {
			x := r.ExpiresAt.Int64
			v.ExpiresAt = &x
		}
		if r.UsedBy.Valid {
			s := r.UsedBy.String
			v.UsedBy = &s
		}
		if r.UsedAt.Valid {
			x := r.UsedAt.Int64
			v.UsedAt = &x
		}
		out = append(out, v)
	}
	return out, nil
}

// DeleteInviteByID removes an invite by its ID.
func (s *Service) DeleteInviteByID(ctx context.Context, id string) error {
	return s.q.DeleteInvite(ctx, id)
}
