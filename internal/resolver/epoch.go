package resolver

import (
	"context"
	"strconv"

	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// EpochStore is the settings subset needed to read and bump a per-identity
// binding epoch. *db.Queries satisfies this interface, and so does wiring's
// b.queries field — allowing reconcileLibraryIdentity to bump the epoch without
// holding a reference to the resolver Service (which does not exist yet at
// Builder.Build time).
type EpochStore interface {
	GetSetting(ctx context.Context, key string) (string, error)
	UpsertSetting(ctx context.Context, arg db.UpsertSettingParams) error
}

// epochKey returns the settings key for the per-identity binding epoch.
// Single source of truth: both BumpEpoch (called from wiring) and
// Service.epoch / Service.BumpEpoch read/write this same key.
func epochKey(identity string) string {
	return "binding_epoch:" + identity
}

// BumpEpoch increments the per-identity binding epoch stored in settings.
// Starting value when the key is absent is 1 (same default as Service.epoch),
// so the first bump writes 2. Idempotency is NOT provided by this helper —
// callers must guard (e.g. reconcileLibraryIdentity only calls it on the
// identity-CHANGED path).
//
// This is the single implementation shared by Service.BumpEpoch (resolver
// internal) and reconcileLibraryIdentity (wiring), ensuring both read and write
// the exact same key format.
//
// DEFERRED (P2/SP3): pieces (2) runScan targeted binding refresh and (3) async
// post-swap sweep are intentionally NOT wired here. They are best-effort
// pre-warming, not a correctness dependency — the resolver lazily re-resolves on
// a binding miss. In P1 there is NO canonical-keyed consumer to refresh yet
// (download jobs become canonical-keyed in Task 11, plays in SP3). The resolver
// is also constructed AFTER Builder.Build, so wiring it into Build-time
// reconcile/Manager would create a construction cycle. Revisit pre-warming in
// P2/SP3 once consumers are canonical-keyed.
func BumpEpoch(ctx context.Context, q EpochStore, identity string) error {
	key := epochKey(identity)
	var cur int64 = 1 // default when absent or unparseable
	if v, err := q.GetSetting(ctx, key); err == nil {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			cur = n
		}
	}
	return q.UpsertSetting(ctx, db.UpsertSettingParams{
		Key:   key,
		Value: strconv.FormatInt(cur+1, 10),
	})
}
