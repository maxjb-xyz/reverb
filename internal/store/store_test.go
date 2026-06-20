package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/maximusjb/crate/internal/store/db"
)

func TestMigrateAndSettingsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := st.Q().UpsertSetting(ctx, db.UpsertSettingParams{Key: "k", Value: "v1"}); err != nil {
		t.Fatal(err)
	}
	got, err := st.Q().GetSetting(ctx, "k")
	if err != nil || got != "v1" {
		t.Fatalf("get = %q err=%v", got, err)
	}
	// upsert overwrites
	_ = st.Q().UpsertSetting(ctx, db.UpsertSettingParams{Key: "k", Value: "v2"})
	got, _ = st.Q().GetSetting(ctx, "k")
	if got != "v2" {
		t.Fatalf("upsert did not overwrite: %q", got)
	}
}
