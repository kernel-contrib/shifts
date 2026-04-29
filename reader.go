package shifts

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/kernel-contrib/shifts/internal"
	"github.com/kernel-contrib/shifts/types"
)

// ── Reader interface ──────────────────────────────────────────────────────────

// ShiftsReader is the cross-module reader interface.
// Other modules consume this via:
//
//	reader, err := sdk.Reader[shifts.ShiftsReader](&m.ctx, "shifts")
//
// Rules:
//   - All methods MUST be read-only (no writes, no events).
//   - Always scope queries by tenant to prevent cross-tenant data leaks.
//   - Resolve readers lazily in handlers, NEVER in Init().
type ShiftsReader interface {
	// GetShiftForDay returns the resolved effective shift for a member on a date.
	// Applies override hierarchy: member-specific → shift-wide → base rules.
	// Returns nil (no error) if the member has no shift that day.
	GetShiftForDay(ctx context.Context, tenantID, memberID uuid.UUID, date time.Time) (*types.ResolvedShift, error)

	// GetShiftsStartingWithinHour returns all resolved shifts across all tenants
	// that start within the next 60 minutes from `now`.
	// Used by the reminders cron to determine which notifications to fire.
	GetShiftsStartingWithinHour(ctx context.Context, now time.Time) ([]types.ResolvedShift, error)
}

// ── Implementation ────────────────────────────────────────────────────────────

// shiftsReader is the unexported implementation registered with the kernel.
type shiftsReader struct {
	svc *internal.Service
}

func (r *shiftsReader) GetShiftForDay(ctx context.Context, tenantID, memberID uuid.UUID, date time.Time) (*types.ResolvedShift, error) {
	return r.svc.ResolveShiftForDay(ctx, tenantID, memberID, date)
}

func (r *shiftsReader) GetShiftsStartingWithinHour(ctx context.Context, now time.Time) ([]types.ResolvedShift, error) {
	return r.svc.GetShiftsStartingWithinHour(ctx, now)
}
