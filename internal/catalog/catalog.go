package catalog

import (
	"context"
	"time"

	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// Identity holds all the metadata a caller knows about a library entity.
// Fields ISRC, MBID, Source, and ExternalID are optional.
type Identity struct {
	Kind       string // "track", "album", "artist"
	Title      string
	Artist     string
	Album      string
	ISRC       string
	MBID       string
	Source     string
	ExternalID string
	DurationMs int
}

// Querier is the slice of generated *db.Queries methods that catalog needs.
// It includes the merge methods that Task 4 will use so Task 4 doesn't need to
// change this interface.
type Querier interface {
	GetAliasCatalogID(ctx context.Context, arg db.GetAliasCatalogIDParams) (string, error)
	InsertCatalogEntity(ctx context.Context, arg db.InsertCatalogEntityParams) error
	InsertCatalogAlias(ctx context.Context, arg db.InsertCatalogAliasParams) error
	GetCatalogEntity(ctx context.Context, id string) (db.CatalogEntity, error)
	// Merge methods — Task 4 uses these; include now so Task 4 doesn't change the interface.
	ListAliasesForCatalog(ctx context.Context, catalogID string) ([]db.ListAliasesForCatalogRow, error)
	RepointAliases(ctx context.Context, arg db.RepointAliasesParams) error
	RepointBindings(ctx context.Context, arg db.RepointBindingsParams) error
	DeleteCatalogEntity(ctx context.Context, id string) error
}

// Service mints and resolves catalog IDs.
type Service struct {
	q     Querier
	now   func() time.Time
	idgen func() string // returns a uuid-ish token (no prefix)
}

// NewService constructs a Service.
func NewService(q Querier, now func() time.Time, idgen func() string) *Service {
	return &Service{q: q, now: now, idgen: idgen}
}
