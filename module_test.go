package shifts_test

import (
	"context"
	"testing"
	"time"

	"github.com/edgescaleDev/kernel/sdk"
	"github.com/google/uuid"
	"github.com/kernel-contrib/shifts/internal"
	"github.com/kernel-contrib/shifts/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// ── test DB setup ─────────────────────────────────────────────────────────────

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err, "open in-memory sqlite")

	ddl := []string{
		`CREATE TABLE shifts (
			id TEXT PRIMARY KEY,
			created_at DATETIME, updated_at DATETIME, deleted_at DATETIME,
			tenant_id TEXT NOT NULL,
			title TEXT NOT NULL,
			shift_type TEXT NOT NULL DEFAULT 'permanent',
			start_date DATE,
			end_date DATE,
			working_days BLOB NOT NULL DEFAULT '[]',
			specific_dates BLOB NOT NULL DEFAULT '[]',
			start_time TEXT NOT NULL,
			end_time TEXT NOT NULL,
			work_location_type TEXT NOT NULL DEFAULT 'onsite',
			metadata BLOB NOT NULL DEFAULT '{}'
		)`,
		`CREATE TABLE shift_members (
			id TEXT PRIMARY KEY,
			created_at DATETIME, updated_at DATETIME, deleted_at DATETIME,
			tenant_id TEXT NOT NULL,
			shift_id TEXT NOT NULL,
			tenant_member_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			effective_from DATE NOT NULL,
			effective_to DATE
		)`,
		`CREATE TABLE shift_overrides (
			id TEXT PRIMARY KEY,
			created_at DATETIME, updated_at DATETIME, deleted_at DATETIME,
			tenant_id TEXT NOT NULL,
			shift_id TEXT NOT NULL,
			tenant_member_id TEXT,
			start_date DATE NOT NULL,
			end_date DATE NOT NULL,
			is_day_off INTEGER NOT NULL DEFAULT 0,
			new_start_time TEXT,
			new_end_time TEXT,
			reason TEXT,
			metadata BLOB NOT NULL DEFAULT '{}'
		)`,
	}

	for _, stmt := range ddl {
		require.NoError(t, db.Exec(stmt).Error, "DDL failed")
	}
	return db
}

// ── test harness ──────────────────────────────────────────────────────────────

type testHarness struct {
	db   *gorm.DB
	ctx  *sdk.Context
	repo *internal.Repository
	svc  *internal.Service
}

func newTestHarness(t *testing.T) *testHarness {
	t.Helper()
	db := newTestDB(t)
	tctx := sdk.NewTestContext("shifts")
	tctx.DB = db

	repo := internal.NewRepository(db)
	svc := internal.NewService(repo, tctx.Bus, tctx.Redis, tctx.Config, tctx.Logger)

	return &testHarness{
		db:   db,
		ctx:  tctx,
		repo: repo,
		svc:  svc,
	}
}

func (h *testHarness) bus() *sdk.TestBus {
	return h.ctx.Bus.(*sdk.TestBus)
}

// ── error helpers ─────────────────────────────────────────────────────────────

func isNotFound(err error) bool {
	se, ok := sdk.IsServiceError(err)
	return ok && se.HTTPStatus == 404
}

func isBadRequest(err error) bool {
	se, ok := sdk.IsServiceError(err)
	return ok && se.HTTPStatus == 400
}

func isConflict(err error) bool {
	se, ok := sdk.IsServiceError(err)
	return ok && se.HTTPStatus == 409
}

// ═══════════════════════════════════════════════════════════════════════════════
// Shift CRUD Tests
// ═══════════════════════════════════════════════════════════════════════════════

func TestShiftCreate(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	tenantID := uuid.New()

	shift, err := h.svc.CreateShift(ctx, internal.CreateShiftInput{
		TenantID:         tenantID,
		Title:            "Morning Shift",
		ShiftType:        types.ShiftTypePermanent,
		WorkingDays:      []int{1, 2, 3, 4, 5},
		StartTime:        "09:00",
		EndTime:          "17:00",
		WorkLocationType: types.LocationOnsite,
	})
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, shift.ID)
	assert.Equal(t, "Morning Shift", shift.Title)
	assert.Equal(t, tenantID, shift.TenantID)
	assert.Equal(t, types.ShiftTypePermanent, shift.ShiftType)
	assert.Equal(t, "09:00", shift.StartTime)

	// Event published.
	events := h.bus().Events()
	require.Len(t, events, 1)
	assert.Equal(t, "shifts.shift.created", events[0].Subject)
}

func TestShiftCreate_MissingTitle(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	_, err := h.svc.CreateShift(ctx, internal.CreateShiftInput{
		TenantID:    uuid.New(),
		Title:       "",
		WorkingDays: []int{1},
		StartTime:   "09:00",
		EndTime:     "17:00",
	})
	assert.True(t, isBadRequest(err))
}

func TestShiftGetByID(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	tenantID := uuid.New()

	created, err := h.svc.CreateShift(ctx, internal.CreateShiftInput{
		TenantID:    tenantID,
		Title:       "Findable",
		ShiftType:   types.ShiftTypePermanent,
		WorkingDays: []int{1, 2, 3, 4, 5},
		StartTime:   "09:00",
		EndTime:     "17:00",
	})
	require.NoError(t, err)

	found, err := h.svc.GetShiftByID(ctx, tenantID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, found.ID)
	assert.Equal(t, "Findable", found.Title)
}

func TestShiftGetByID_NotFound(t *testing.T) {
	h := newTestHarness(t)
	_, err := h.svc.GetShiftByID(context.Background(), uuid.New(), uuid.New())
	assert.True(t, isNotFound(err))
}

func TestShiftDelete(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	tenantID := uuid.New()

	created, err := h.svc.CreateShift(ctx, internal.CreateShiftInput{
		TenantID:    tenantID,
		Title:       "Deletable",
		ShiftType:   types.ShiftTypePermanent,
		WorkingDays: []int{1},
		StartTime:   "09:00",
		EndTime:     "17:00",
	})
	require.NoError(t, err)

	err = h.svc.DeleteShift(ctx, tenantID, created.ID)
	require.NoError(t, err)

	_, err = h.svc.GetShiftByID(ctx, tenantID, created.ID)
	assert.True(t, isNotFound(err))
}

// ═══════════════════════════════════════════════════════════════════════════════
// Overnight Shift Tests
// ═══════════════════════════════════════════════════════════════════════════════

func TestShiftIsOvernight(t *testing.T) {
	s := &types.Shift{StartTime: "23:00", EndTime: "07:00"}
	assert.True(t, s.IsOvernight())

	s2 := &types.Shift{StartTime: "09:00", EndTime: "17:00"}
	assert.False(t, s2.IsOvernight())
}

// ═══════════════════════════════════════════════════════════════════════════════
// Helper Tests
// ═══════════════════════════════════════════════════════════════════════════════

func TestGroupContiguousDates(t *testing.T) {
	dates := []time.Time{
		time.Date(2026, 5, 16, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC),
	}

	ranges := internal.GroupContiguousDates(dates)
	require.Len(t, ranges, 3)

	assert.Equal(t, time.Date(2026, 5, 16, 0, 0, 0, 0, time.UTC), ranges[0].Start)
	assert.Equal(t, time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC), ranges[0].End)

	assert.Equal(t, time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC), ranges[1].Start)
	assert.Equal(t, time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC), ranges[1].End)

	assert.Equal(t, time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC), ranges[2].Start)
	assert.Equal(t, time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC), ranges[2].End)
}

func TestTimeOverlaps(t *testing.T) {
	// Same window: overlaps.
	assert.True(t, internal.TimeOverlaps("09:00", "17:00", "09:00", "17:00"))

	// Partial overlap.
	assert.True(t, internal.TimeOverlaps("09:00", "17:00", "15:00", "21:00"))

	// No overlap.
	assert.False(t, internal.TimeOverlaps("09:00", "17:00", "18:00", "22:00"))

	// Overnight vs daytime — no overlap.
	assert.False(t, internal.TimeOverlaps("23:00", "07:00", "08:00", "16:00"))

	// Overnight vs early morning — overlap.
	assert.True(t, internal.TimeOverlaps("23:00", "07:00", "05:00", "13:00"))

	// Two overnight shifts — overlap.
	assert.True(t, internal.TimeOverlaps("22:00", "06:00", "23:00", "07:00"))
}

func TestIsWorkingDay(t *testing.T) {
	days := []int{1, 2, 3, 4, 5} // Mon-Fri

	// Monday = ISO 1.
	mon := time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC) // Monday
	assert.True(t, internal.IsWorkingDay(days, mon))

	// Sunday = ISO 7.
	sun := time.Date(2026, 4, 19, 0, 0, 0, 0, time.UTC) // Sunday
	assert.False(t, internal.IsWorkingDay(days, sun))
}

func TestWorkingDaysOverlap(t *testing.T) {
	assert.True(t, internal.WorkingDaysOverlap([]int{1, 2, 3}, []int{3, 4, 5}))
	assert.False(t, internal.WorkingDaysOverlap([]int{1, 2, 3}, []int{4, 5, 6}))
}

func TestDateRangesOverlap(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	t4 := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	t5 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	// Overlap.
	assert.True(t, internal.DateRangesOverlap(t1, &t2, t3, &t4))

	// No overlap.
	assert.False(t, internal.DateRangesOverlap(t1, &t2, t5, &t4))

	// Nil end (indefinite) — always overlaps.
	assert.True(t, internal.DateRangesOverlap(t1, nil, t3, &t4))
	assert.True(t, internal.DateRangesOverlap(t1, &t2, t3, nil))
}

// ═══════════════════════════════════════════════════════════════════════════════
// Cross-Tenant Conflict Tests
// ═══════════════════════════════════════════════════════════════════════════════

func TestAssignMember_CrossTenantConflict(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	tenantA := uuid.New()
	tenantB := uuid.New()
	userID := uuid.New() // Same human, different tenants.

	// Create shift in tenant A: Mon-Fri 09:00-17:00.
	shiftA, err := h.svc.CreateShift(ctx, internal.CreateShiftInput{
		TenantID:    tenantA,
		Title:       "Tenant-A Morning",
		ShiftType:   types.ShiftTypePermanent,
		WorkingDays: []int{1, 2, 3, 4, 5},
		StartTime:   "09:00",
		EndTime:     "17:00",
	})
	require.NoError(t, err)

	// Assign user to tenant A's shift.
	_, err = h.svc.AssignMember(ctx, internal.AssignMemberInput{
		TenantID:       tenantA,
		ShiftID:        shiftA.ID,
		TenantMemberID: uuid.New(), // Different tenant_member_id.
		UserID:         userID,
		EffectiveFrom:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)

	// Create shift in tenant B: Mon-Fri 09:00-17:00 (same hours = conflict!).
	shiftB, err := h.svc.CreateShift(ctx, internal.CreateShiftInput{
		TenantID:    tenantB,
		Title:       "Tenant-B Morning",
		ShiftType:   types.ShiftTypePermanent,
		WorkingDays: []int{1, 2, 3, 4, 5},
		StartTime:   "09:00",
		EndTime:     "17:00",
	})
	require.NoError(t, err)

	// Try to assign same user to tenant B's shift — should fail.
	_, err = h.svc.AssignMember(ctx, internal.AssignMemberInput{
		TenantID:       tenantB,
		ShiftID:        shiftB.ID,
		TenantMemberID: uuid.New(),
		UserID:         userID,
		EffectiveFrom:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	assert.True(t, isConflict(err), "expected 409 conflict, got: %v", err)
}

func TestAssignMember_CrossTenantNoConflict_DifferentDays(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	tenantA := uuid.New()
	tenantB := uuid.New()
	userID := uuid.New()

	// Tenant A: Mon-Wed.
	shiftA, err := h.svc.CreateShift(ctx, internal.CreateShiftInput{
		TenantID:    tenantA,
		Title:       "A: Mon-Wed",
		ShiftType:   types.ShiftTypePermanent,
		WorkingDays: []int{1, 2, 3},
		StartTime:   "09:00",
		EndTime:     "17:00",
	})
	require.NoError(t, err)

	_, err = h.svc.AssignMember(ctx, internal.AssignMemberInput{
		TenantID:       tenantA,
		ShiftID:        shiftA.ID,
		TenantMemberID: uuid.New(),
		UserID:         userID,
		EffectiveFrom:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)

	// Tenant B: Thu-Fri (no day overlap = OK).
	shiftB, err := h.svc.CreateShift(ctx, internal.CreateShiftInput{
		TenantID:    tenantB,
		Title:       "B: Thu-Fri",
		ShiftType:   types.ShiftTypePermanent,
		WorkingDays: []int{4, 5},
		StartTime:   "09:00",
		EndTime:     "17:00",
	})
	require.NoError(t, err)

	_, err = h.svc.AssignMember(ctx, internal.AssignMemberInput{
		TenantID:       tenantB,
		ShiftID:        shiftB.ID,
		TenantMemberID: uuid.New(),
		UserID:         userID,
		EffectiveFrom:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	assert.NoError(t, err, "different days should not conflict")
}

// ═══════════════════════════════════════════════════════════════════════════════
// Override Tests
// ═══════════════════════════════════════════════════════════════════════════════

func TestOverrideConflictDetection(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	tenantID := uuid.New()

	shift, err := h.svc.CreateShift(ctx, internal.CreateShiftInput{
		TenantID:    tenantID,
		Title:       "Test",
		ShiftType:   types.ShiftTypePermanent,
		WorkingDays: []int{1, 2, 3, 4, 5},
		StartTime:   "09:00",
		EndTime:     "17:00",
	})
	require.NoError(t, err)

	// Create override for May 20-22.
	_, err = h.svc.CreateOverrides(ctx, internal.CreateOverridesInput{
		TenantID: tenantID,
		ShiftID:  shift.ID,
		Dates: []time.Time{
			time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC),
		},
		IsDayOff: true,
		Reason:   strPtr("National holiday"),
	})
	require.NoError(t, err)

	// Try creating overlapping override — should 409.
	_, err = h.svc.CreateOverrides(ctx, internal.CreateOverridesInput{
		TenantID: tenantID,
		ShiftID:  shift.ID,
		Dates: []time.Time{
			time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC),
		},
		IsDayOff: false,
	})
	assert.True(t, isConflict(err), "expected 409 conflict, got: %v", err)
}

func TestBulkMemberOverrides(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	tenantID := uuid.New()

	shift, err := h.svc.CreateShift(ctx, internal.CreateShiftInput{
		TenantID:    tenantID,
		Title:       "Morning",
		ShiftType:   types.ShiftTypePermanent,
		WorkingDays: []int{1, 2, 3, 4, 5},
		StartTime:   "08:00",
		EndTime:     "16:00",
	})
	require.NoError(t, err)

	memberA := uuid.New()
	memberB := uuid.New()
	memberC := uuid.New()

	// Bulk override: 3 members × 2 dates = should create 6 override records.
	overrides, err := h.svc.CreateOverrides(ctx, internal.CreateOverridesInput{
		TenantID:        tenantID,
		ShiftID:         shift.ID,
		TenantMemberIDs: []uuid.UUID{memberA, memberB, memberC},
		Dates: []time.Time{
			time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
		},
		IsDayOff: true,
		Reason:   strPtr("Training day"),
	})
	require.NoError(t, err)
	// 2 dates are contiguous → 1 range. 3 members × 1 range = 3 records.
	require.Len(t, overrides, 3, "3 members × 1 contiguous range")

	// Each override should target a different member.
	memberIDs := map[uuid.UUID]bool{}
	for _, o := range overrides {
		require.NotNil(t, o.TenantMemberID)
		memberIDs[*o.TenantMemberID] = true
		assert.True(t, o.IsDayOff)
	}
	assert.Len(t, memberIDs, 3, "all 3 members should have overrides")

	// Non-contiguous dates: 3 members × 2 separate ranges = 6 records.
	overrides2, err := h.svc.CreateOverrides(ctx, internal.CreateOverridesInput{
		TenantID:        tenantID,
		ShiftID:         shift.ID,
		TenantMemberIDs: []uuid.UUID{memberA, memberB, memberC},
		Dates: []time.Time{
			time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC), // gap
			time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC),
		},
		IsDayOff: true,
	})
	require.NoError(t, err)
	require.Len(t, overrides2, 6, "3 members × 2 non-contiguous dates = 6 records")

	// Whole-shift override (no member IDs) — should create 1 record.
	overrides3, err := h.svc.CreateOverrides(ctx, internal.CreateOverridesInput{
		TenantID: tenantID,
		ShiftID:  shift.ID,
		Dates: []time.Time{
			time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		},
		IsDayOff: true,
		Reason:   strPtr("National holiday"),
	})
	require.NoError(t, err)
	require.Len(t, overrides3, 1)
	assert.Nil(t, overrides3[0].TenantMemberID, "whole-shift override should have nil member")
}

// ═══════════════════════════════════════════════════════════════════════════════
// Roster Tests
// ═══════════════════════════════════════════════════════════════════════════════

func TestRosterDateRangeValidation(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	tenantID := uuid.New()

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC) // 365 days — too long.

	_, err := h.svc.GetRoster(ctx, tenantID, start, end, nil)
	assert.True(t, isBadRequest(err), "expected 400 for range > 42 days")
}

func TestTenantIsolation(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	tenantA := uuid.New()
	tenantB := uuid.New()

	_, err := h.svc.CreateShift(ctx, internal.CreateShiftInput{
		TenantID:    tenantA,
		Title:       "A's Shift",
		ShiftType:   types.ShiftTypePermanent,
		WorkingDays: []int{1},
		StartTime:   "09:00",
		EndTime:     "17:00",
	})
	require.NoError(t, err)

	_, err = h.svc.CreateShift(ctx, internal.CreateShiftInput{
		TenantID:    tenantB,
		Title:       "B's Shift",
		ShiftType:   types.ShiftTypePermanent,
		WorkingDays: []int{1},
		StartTime:   "09:00",
		EndTime:     "17:00",
	})
	require.NoError(t, err)

	result, err := h.svc.ListShifts(ctx, tenantA, sdk.PageRequest{Page: 1, PerPage: 10})
	require.NoError(t, err)
	assert.Equal(t, int64(1), result.Meta.TotalCount)
	assert.Equal(t, "A's Shift", result.Items[0].Title)
}

// ═══════════════════════════════════════════════════════════════════════════════
// Grace Window Cascade Tests
// ═══════════════════════════════════════════════════════════════════════════════

func TestGraceWindow_HardDefault(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	tenantID := uuid.New()
	memberID := uuid.New()
	userID := uuid.New()

	// Create shift with NO grace metadata — should cascade to 5 min default.
	shift, err := h.svc.CreateShift(ctx, internal.CreateShiftInput{
		TenantID:    tenantID,
		Title:       "No Grace Override",
		ShiftType:   types.ShiftTypePermanent,
		WorkingDays: []int{1, 2, 3, 4, 5}, // Mon-Fri
		StartTime:   "08:00",
		EndTime:     "16:00",
	})
	require.NoError(t, err)

	_, err = h.svc.AssignMember(ctx, internal.AssignMemberInput{
		TenantID:       tenantID,
		ShiftID:        shift.ID,
		TenantMemberID: memberID,
		UserID:         userID,
		EffectiveFrom:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)

	// Get roster for a Monday.
	mon := time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC) // Monday
	roster, err := h.svc.GetRoster(ctx, tenantID, mon, mon, nil)
	require.NoError(t, err)
	require.Len(t, roster, 1)

	gw := roster[0].GraceWindow
	assert.Equal(t, types.DefaultGraceMinutes, gw.EarlyCheckinAllowanceMins, "should default to 5")
	assert.Equal(t, types.DefaultGraceMinutes, gw.LateCheckinGraceMins, "should default to 5")
	assert.Equal(t, types.DefaultGraceMinutes, gw.EarlyCheckoutAllowanceMins, "should default to 5")
	assert.Equal(t, types.DefaultGraceMinutes, gw.LateCheckoutAllowanceMins, "should default to 5")
}

func TestGraceWindow_ShiftOverride(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	tenantID := uuid.New()
	memberID := uuid.New()
	userID := uuid.New()

	// Create shift with custom grace in metadata — should override defaults.
	shift, err := h.svc.CreateShift(ctx, internal.CreateShiftInput{
		TenantID:    tenantID,
		Title:       "Custom Grace",
		ShiftType:   types.ShiftTypePermanent,
		WorkingDays: []int{1, 2, 3, 4, 5},
		StartTime:   "08:00",
		EndTime:     "16:00",
		Metadata: map[string]any{
			"early_checkin_allowance_mins":  15,
			"late_checkin_grace_mins":       10,
			"early_checkout_allowance_mins": 15,
			"late_checkout_allowance_mins":  10,
		},
	})
	require.NoError(t, err)

	_, err = h.svc.AssignMember(ctx, internal.AssignMemberInput{
		TenantID:       tenantID,
		ShiftID:        shift.ID,
		TenantMemberID: memberID,
		UserID:         userID,
		EffectiveFrom:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)

	mon := time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC)
	roster, err := h.svc.GetRoster(ctx, tenantID, mon, mon, nil)
	require.NoError(t, err)
	require.Len(t, roster, 1)

	gw := roster[0].GraceWindow
	assert.Equal(t, 15, gw.EarlyCheckinAllowanceMins, "shift metadata should override default")
	assert.Equal(t, 10, gw.LateCheckinGraceMins, "shift metadata should override default")
	assert.Equal(t, 15, gw.EarlyCheckoutAllowanceMins, "shift metadata should override default")
	assert.Equal(t, 10, gw.LateCheckoutAllowanceMins, "shift metadata should override default")
}

// ═══════════════════════════════════════════════════════════════════════════════
// Roster Resolution Test (full scenario)
// ═══════════════════════════════════════════════════════════════════════════════

func TestRosterResolution_WithOverride(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	tenantID := uuid.New()
	memberID := uuid.New()
	userID := uuid.New()

	// Create shift: Mon-Fri 08:00-16:00.
	shift, err := h.svc.CreateShift(ctx, internal.CreateShiftInput{
		TenantID:    tenantID,
		Title:       "Standard",
		ShiftType:   types.ShiftTypePermanent,
		WorkingDays: []int{1, 2, 3, 4, 5},
		StartTime:   "08:00",
		EndTime:     "16:00",
	})
	require.NoError(t, err)

	// Assign member.
	_, err = h.svc.AssignMember(ctx, internal.AssignMemberInput{
		TenantID:       tenantID,
		ShiftID:        shift.ID,
		TenantMemberID: memberID,
		UserID:         userID,
		EffectiveFrom:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)

	// Create a day-off override for Wednesday Apr 22.
	_, err = h.svc.CreateOverrides(ctx, internal.CreateOverridesInput{
		TenantID: tenantID,
		ShiftID:  shift.ID,
		Dates: []time.Time{
			time.Date(2026, 4, 22, 0, 0, 0, 0, time.UTC), // Wed
		},
		IsDayOff: true,
		Reason:   strPtr("Team building day"),
	})
	require.NoError(t, err)

	// Get roster for Mon-Fri (Apr 20-26 includes Sat/Sun which aren't working days).
	startDate := time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC) // Mon
	endDate := time.Date(2026, 4, 26, 0, 0, 0, 0, time.UTC)   // Sun

	roster, err := h.svc.GetRoster(ctx, tenantID, startDate, endDate, nil)
	require.NoError(t, err)

	// Should have 5 entries: Mon, Tue, Wed (day off), Thu, Fri.
	// Saturday and Sunday are excluded (not working days).
	require.Len(t, roster, 5)

	// Wednesday should be marked as day off.
	wedEntry := roster[2] // Index 2 = Wednesday
	assert.True(t, wedEntry.IsDayOff)
	assert.NotNil(t, wedEntry.OverrideReason)
	assert.Equal(t, "Team building day", *wedEntry.OverrideReason)

	// Other days should NOT be day off.
	assert.False(t, roster[0].IsDayOff) // Mon
	assert.False(t, roster[1].IsDayOff) // Tue
	assert.False(t, roster[3].IsDayOff) // Thu
	assert.False(t, roster[4].IsDayOff) // Fri
}

// ═══════════════════════════════════════════════════════════════════════════════
// Specific Dates Tests
// ═══════════════════════════════════════════════════════════════════════════════

func TestSpecificDates_NonContiguous(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	tenantID := uuid.New()
	memberID := uuid.New()
	userID := uuid.New()

	// Create shift with non-contiguous specific dates.
	// May 5 (Tue), May 8 (Fri), May 12 (Tue) — no pattern, different weekdays.
	shift, err := h.svc.CreateShift(ctx, internal.CreateShiftInput{
		TenantID:  tenantID,
		Title:     "Event Shifts",
		ShiftType: types.ShiftTypeSpecificDates,
		SpecificDates: []string{
			"2026-05-05", // Tue
			"2026-05-08", // Fri
			"2026-05-12", // Tue
		},
		StartTime:        "10:00",
		EndTime:          "18:00",
		WorkLocationType: "onsite",
	})
	require.NoError(t, err)

	_, err = h.svc.AssignMember(ctx, internal.AssignMemberInput{
		TenantID:       tenantID,
		ShiftID:        shift.ID,
		TenantMemberID: memberID,
		UserID:         userID,
		EffectiveFrom:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)

	// Query a wide range that covers all three dates (May 1–15).
	roster, err := h.svc.GetRoster(ctx, tenantID,
		time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC),
		nil,
	)
	require.NoError(t, err)

	// Should only return exactly 3 entries — the three specific dates.
	require.Len(t, roster, 3, "should only include the 3 specific dates, not the range")

	expectedDays := []int{5, 8, 12}
	for i, rs := range roster {
		assert.Equal(t, expectedDays[i], rs.Date.Day(), "day %d mismatch", i)
		assert.Equal(t, "10:00", rs.StartTime)
		assert.Equal(t, "18:00", rs.EndTime)
	}
}

func TestSpecificDates_RangeMode(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	tenantID := uuid.New()
	memberID := uuid.New()
	userID := uuid.New()

	// Create a range-based specific_dates shift (no specific_dates list).
	// Tuesdays in May 5–19.
	sd := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)
	ed := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)
	shift, err := h.svc.CreateShift(ctx, internal.CreateShiftInput{
		TenantID:         tenantID,
		Title:            "Tuesdays in May",
		ShiftType:        types.ShiftTypeSpecificDates,
		StartDate:        &sd,
		EndDate:          &ed,
		WorkingDays:      []int{2}, // Tuesday
		StartTime:        "09:00",
		EndTime:          "17:00",
		WorkLocationType: "onsite",
	})
	require.NoError(t, err)

	_, err = h.svc.AssignMember(ctx, internal.AssignMemberInput{
		TenantID:       tenantID,
		ShiftID:        shift.ID,
		TenantMemberID: memberID,
		UserID:         userID,
		EffectiveFrom:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)

	roster, err := h.svc.GetRoster(ctx, tenantID,
		time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
		nil,
	)
	require.NoError(t, err)

	// May 6 (Tue), May 13 (Tue), May 19 is Mon so excluded.
	// Actually: May 5=Mon, 6=Tue, 12=Tue, 13=Wed... let me recalculate.
	// 2026-05-05 is a Tuesday (Go confirms). So: May 5, May 12, May 19 (all Tuesdays).
	require.Len(t, roster, 3, "should return 3 Tuesdays in range")
	assert.Equal(t, 5, roster[0].Date.Day())
	assert.Equal(t, 12, roster[1].Date.Day())
	assert.Equal(t, 19, roster[2].Date.Day())
}

func TestIsSpecificDate(t *testing.T) {
	dates := []time.Time{
		time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 8, 0, 0, 0, 0, time.UTC),
	}

	assert.True(t, internal.IsSpecificDate(dates, time.Date(2026, 5, 5, 10, 30, 0, 0, time.UTC)))
	assert.True(t, internal.IsSpecificDate(dates, time.Date(2026, 5, 8, 0, 0, 0, 0, time.UTC)))
	assert.False(t, internal.IsSpecificDate(dates, time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC)))
	assert.False(t, internal.IsSpecificDate(dates, time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)))
}

// ── helpers ───────────────────────────────────────────────────────────────────

func strPtr(s string) *string { return &s }
