package embedded

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/maxjb-xyz/reverb/internal/store/db"
)

const settingKeyAdminPassword = "navidrome_admin_password"

// SettingStore is the slice of the DB queries this package needs.
type SettingStore interface {
	GetSetting(ctx context.Context, key string) (string, error)
	UpsertSetting(ctx context.Context, arg db.UpsertSettingParams) error
}

// EnsureInternalCredentials returns the internal admin credentials for the
// bundled Navidrome, generating and persisting a strong random password the
// first time. The username is always AdminUsername.
func EnsureInternalCredentials(ctx context.Context, s SettingStore) (Credentials, error) {
	if pw, err := s.GetSetting(ctx, settingKeyAdminPassword); err == nil && pw != "" {
		return Credentials{Username: AdminUsername, Password: pw}, nil
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return Credentials{}, fmt.Errorf("embedded: generate password: %w", err)
	}
	pw := hex.EncodeToString(buf)
	if err := s.UpsertSetting(ctx, db.UpsertSettingParams{Key: settingKeyAdminPassword, Value: pw}); err != nil {
		return Credentials{}, fmt.Errorf("embedded: persist password: %w", err)
	}
	return Credentials{Username: AdminUsername, Password: pw}, nil
}
