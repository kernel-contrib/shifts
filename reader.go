package mymodule

import (
	"context"

	"github.com/google/uuid"
	"go.edgescale.dev/kernel-contrib/mymodule/internal"
	"go.edgescale.dev/kernel-contrib/mymodule/types"
)

// ── Reader interface ──────────────────────────────────────────────────────────

// MyModuleReader is the cross-module reader interface.
// Other modules consume this via:
//
//	reader, err := sdk.Reader[mymodule.MyModuleReader](&m.ctx, "mymodule")
//
// Rules:
//   - All methods MUST be read-only (no writes, no events).
//   - Always scope queries by tenant to prevent cross-tenant data leaks.
//   - Resolve readers lazily in handlers, NEVER in Init().
//   - Optional: back with Redis cache for performance.
type MyModuleReader interface {
	GetItem(ctx context.Context, id uuid.UUID) (*types.Item, error)
}

// ── Implementation ────────────────────────────────────────────────────────────

// moduleReader is the unexported implementation registered with the kernel.
type moduleReader struct {
	repo *internal.Repository
}

func (r *moduleReader) GetItem(ctx context.Context, id uuid.UUID) (*types.Item, error) {
	return r.repo.FindItemByID(ctx, id)
}
