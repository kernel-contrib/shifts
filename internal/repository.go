package internal

import (
	"context"
	"fmt"
	"time"

	"github.com/edgescaleDev/kernel/sdk"
	"github.com/google/uuid"
	"github.com/kernel-contrib/shifts/types"
	"gorm.io/gorm"
)

// Repository is the data-access layer for the shifts module.
// All database interactions happen through this struct.
type Repository struct {
	db *gorm.DB
}

// NewRepository creates a Repository backed by the provided *gorm.DB.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// ═══════════════════════════════════════════════════════════════════════════════
// Shifts
// ═══════════════════════════════════════════════════════════════════════════════

// CreateShift inserts a new shift.
func (r *Repository) CreateShift(ctx context.Context, shift *types.Shift) error {
	if err := r.db.WithContext(ctx).Create(shift).Error; err != nil {
		return fmt.Errorf("shifts: create shift: %w", err)
	}
	return nil
}

// FindShiftByID looks up a shift by its UUID, scoped to a tenant.
func (r *Repository) FindShiftByID(ctx context.Context, tenantID, id uuid.UUID) (*types.Shift, error) {
	var shift types.Shift
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND id = ?", tenantID, id).
		First(&shift).Error
	if err != nil {
		return nil, fmt.Errorf("shifts: find shift by id: %w", err)
	}
	return &shift, nil
}

// FindShiftByIDWithAssociations loads a shift with its members and overrides.
func (r *Repository) FindShiftByIDWithAssociations(ctx context.Context, tenantID, id uuid.UUID) (*types.Shift, error) {
	var shift types.Shift
	err := r.db.WithContext(ctx).
		Preload("Members", "deleted_at IS NULL").
		Preload("Overrides", "deleted_at IS NULL").
		Where("tenant_id = ? AND id = ?", tenantID, id).
		First(&shift).Error
	if err != nil {
		return nil, fmt.Errorf("shifts: find shift with associations: %w", err)
	}
	return &shift, nil
}

// ListShifts returns a paginated list of shifts for a tenant.
func (r *Repository) ListShifts(ctx context.Context, tenantID uuid.UUID, page sdk.PageRequest) (*sdk.PageResult[types.Shift], error) {
	return sdk.Paginate[types.Shift](
		r.db.WithContext(ctx).Model(&types.Shift{}).Where("tenant_id = ?", tenantID),
		page,
	)
}

// UpdateShift patches a shift by ID.
func (r *Repository) UpdateShift(ctx context.Context, tenantID, id uuid.UUID, updates map[string]any) (*types.Shift, error) {
	if err := r.db.WithContext(ctx).
		Model(&types.Shift{}).
		Where("tenant_id = ? AND id = ?", tenantID, id).
		Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("shifts: update shift: %w", err)
	}
	return r.FindShiftByID(ctx, tenantID, id)
}

// SoftDeleteShift soft-deletes a shift and cascades to members and overrides.
func (r *Repository) SoftDeleteShift(ctx context.Context, tenantID, id uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Cascade: soft-delete members.
		if err := tx.Where("tenant_id = ? AND shift_id = ?", tenantID, id).
			Delete(&types.ShiftMember{}).Error; err != nil {
			return fmt.Errorf("shifts: cascade delete members: %w", err)
		}
		// Cascade: soft-delete overrides.
		if err := tx.Where("tenant_id = ? AND shift_id = ?", tenantID, id).
			Delete(&types.ShiftOverride{}).Error; err != nil {
			return fmt.Errorf("shifts: cascade delete overrides: %w", err)
		}
		// Delete the shift itself.
		if err := tx.Where("tenant_id = ? AND id = ?", tenantID, id).
			Delete(&types.Shift{}).Error; err != nil {
			return fmt.Errorf("shifts: delete shift: %w", err)
		}
		return nil
	})
}

// FindShiftsByIDs batch-fetches shifts by their IDs, scoped to a tenant.
// Returns a map keyed by shift ID for efficient consumer lookups.
func (r *Repository) FindShiftsByIDs(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]types.Shift, error) {
	if len(ids) == 0 {
		return make(map[uuid.UUID]types.Shift), nil
	}

	var shifts []types.Shift
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND id IN ?", tenantID, ids).
		Find(&shifts).Error
	if err != nil {
		return nil, fmt.Errorf("shifts: find shifts by ids: %w", err)
	}

	result := make(map[uuid.UUID]types.Shift, len(shifts))
	for _, s := range shifts {
		result[s.ID] = s
	}
	return result, nil
}

// ═══════════════════════════════════════════════════════════════════════════════
// Shift Members
// ═══════════════════════════════════════════════════════════════════════════════

// AssignMember inserts a new shift member assignment.
func (r *Repository) AssignMember(ctx context.Context, member *types.ShiftMember) error {
	if err := r.db.WithContext(ctx).Create(member).Error; err != nil {
		return fmt.Errorf("shifts: assign member: %w", err)
	}
	return nil
}

// RemoveMember soft-deletes a member assignment.
func (r *Repository) RemoveMember(ctx context.Context, tenantID, membershipID uuid.UUID) error {
	result := r.db.WithContext(ctx).
		Where("tenant_id = ? AND id = ?", tenantID, membershipID).
		Delete(&types.ShiftMember{})
	if result.Error != nil {
		return fmt.Errorf("shifts: remove member: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// ListMembersByShift returns all active members of a shift.
func (r *Repository) ListMembersByShift(ctx context.Context, tenantID, shiftID uuid.UUID) ([]types.ShiftMember, error) {
	var members []types.ShiftMember
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND shift_id = ?", tenantID, shiftID).
		Find(&members).Error
	if err != nil {
		return nil, fmt.Errorf("shifts: list members: %w", err)
	}
	return members, nil
}

// FindMemberByID looks up a member assignment by ID.
func (r *Repository) FindMemberByID(ctx context.Context, tenantID, id uuid.UUID) (*types.ShiftMember, error) {
	var member types.ShiftMember
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND id = ?", tenantID, id).
		First(&member).Error
	if err != nil {
		return nil, fmt.Errorf("shifts: find member by id: %w", err)
	}
	return &member, nil
}

// CheckCrossTenantConflict checks if a user (by global user_id) has any
// existing shift assignments across ALL tenants that conflict with the
// given shift's schedule (working days + time windows + date ranges).
// Returns the first conflicting member assignment or nil.
func (r *Repository) CheckCrossTenantConflict(
	ctx context.Context,
	userID uuid.UUID,
	shiftWorkingDays []int,
	shiftStartTime, shiftEndTime string,
	effectiveFrom time.Time,
	effectiveTo *time.Time,
) ([]types.ShiftMember, error) {
	// Find all active assignments for this user across all tenants.
	var members []types.ShiftMember
	query := r.db.WithContext(ctx).
		Where("user_id = ?", userID)

	if err := query.Find(&members).Error; err != nil {
		return nil, fmt.Errorf("shifts: check cross-tenant conflict: %w", err)
	}

	if len(members) == 0 {
		return nil, nil
	}

	// For each existing assignment, load the shift to compare schedules.
	var conflicts []types.ShiftMember
	for _, m := range members {
		// Check date range overlap.
		if !DateRangesOverlap(effectiveFrom, effectiveTo, m.EffectiveFrom, m.EffectiveTo) {
			continue
		}

		// Load the existing shift to check working days and times.
		var existingShift types.Shift
		err := r.db.WithContext(ctx).
			Where("id = ?", m.ShiftID).
			First(&existingShift).Error
		if err != nil {
			continue // Skip if the shift can't be loaded (e.g., deleted).
		}

		existingDays := ParseWorkingDays(existingShift.WorkingDays)
		if !WorkingDaysOverlap(shiftWorkingDays, existingDays) {
			continue
		}

		if !TimeOverlaps(shiftStartTime, shiftEndTime, existingShift.StartTime, existingShift.EndTime) {
			continue
		}

		conflicts = append(conflicts, m)
	}

	return conflicts, nil
}

// CheckWithinTenantOverlap checks if a member already has an active assignment
// within the same tenant that overlaps the given date range.
func (r *Repository) CheckWithinTenantOverlap(
	ctx context.Context,
	tenantID, tenantMemberID uuid.UUID,
	effectiveFrom time.Time,
	effectiveTo *time.Time,
	excludeShiftID *uuid.UUID,
) ([]types.ShiftMember, error) {
	query := r.db.WithContext(ctx).
		Where("tenant_id = ? AND tenant_member_id = ?", tenantID, tenantMemberID)

	if excludeShiftID != nil {
		query = query.Where("shift_id != ?", *excludeShiftID)
	}

	var members []types.ShiftMember
	if err := query.Find(&members).Error; err != nil {
		return nil, fmt.Errorf("shifts: check within-tenant overlap: %w", err)
	}

	var overlapping []types.ShiftMember
	for _, m := range members {
		if DateRangesOverlap(effectiveFrom, effectiveTo, m.EffectiveFrom, m.EffectiveTo) {
			overlapping = append(overlapping, m)
		}
	}

	return overlapping, nil
}

// ═══════════════════════════════════════════════════════════════════════════════
// Shift Overrides
// ═══════════════════════════════════════════════════════════════════════════════

// BulkCreateOverrides inserts multiple overrides in a single operation.
func (r *Repository) BulkCreateOverrides(ctx context.Context, overrides []types.ShiftOverride) error {
	if len(overrides) == 0 {
		return nil
	}
	if err := r.db.WithContext(ctx).Create(&overrides).Error; err != nil {
		return fmt.Errorf("shifts: bulk create overrides: %w", err)
	}
	return nil
}

// FindOverrideByID looks up an override by ID.
func (r *Repository) FindOverrideByID(ctx context.Context, id uuid.UUID) (*types.ShiftOverride, error) {
	var override types.ShiftOverride
	err := r.db.WithContext(ctx).
		Where("id = ?", id).
		First(&override).Error
	if err != nil {
		return nil, fmt.Errorf("shifts: find override by id: %w", err)
	}
	return &override, nil
}

// UpdateOverride patches an override by ID.
func (r *Repository) UpdateOverride(ctx context.Context, id uuid.UUID, updates map[string]any) (*types.ShiftOverride, error) {
	if err := r.db.WithContext(ctx).
		Model(&types.ShiftOverride{}).
		Where("id = ?", id).
		Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("shifts: update override: %w", err)
	}
	return r.FindOverrideByID(ctx, id)
}

// SoftDeleteOverride soft-deletes an override.
func (r *Repository) SoftDeleteOverride(ctx context.Context, tenantID, id uuid.UUID) error {
	result := r.db.WithContext(ctx).
		Where("tenant_id = ? AND id = ?", tenantID, id).
		Delete(&types.ShiftOverride{})
	if result.Error != nil {
		return fmt.Errorf("shifts: delete override: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// ListOverridesByShift returns all active overrides for a shift.
func (r *Repository) ListOverridesByShift(ctx context.Context, tenantID, shiftID uuid.UUID) ([]types.ShiftOverride, error) {
	var overrides []types.ShiftOverride
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND shift_id = ?", tenantID, shiftID).
		Order("start_date ASC").
		Find(&overrides).Error
	if err != nil {
		return nil, fmt.Errorf("shifts: list overrides: %w", err)
	}
	return overrides, nil
}

// CheckOverrideConflict checks if any existing override for the same shift
// overlaps with the given date range and member scope.
// Returns conflicting override IDs.
func (r *Repository) CheckOverrideConflict(
	ctx context.Context,
	shiftID uuid.UUID,
	tenantMemberID *uuid.UUID,
	startDate, endDate time.Time,
	excludeID *uuid.UUID,
) ([]types.ShiftOverride, error) {
	query := r.db.WithContext(ctx).
		Where("shift_id = ?", shiftID).
		Where("start_date <= ? AND end_date >= ?", endDate, startDate)

	if tenantMemberID != nil {
		// Member-specific: conflicts with same member or whole-shift overrides.
		query = query.Where("(tenant_member_id = ? OR tenant_member_id IS NULL)", *tenantMemberID)
	} else {
		// Whole-shift: conflicts with any override in the range.
	}

	if excludeID != nil {
		query = query.Where("id != ?", *excludeID)
	}

	var conflicts []types.ShiftOverride
	if err := query.Find(&conflicts).Error; err != nil {
		return nil, fmt.Errorf("shifts: check override conflict: %w", err)
	}
	return conflicts, nil
}

// ═══════════════════════════════════════════════════════════════════════════════
// Resolution Queries
// ═══════════════════════════════════════════════════════════════════════════════

// FindActiveShiftAssignments returns all active shift assignments for a member
// on a specific date.
func (r *Repository) FindActiveShiftAssignments(
	ctx context.Context,
	tenantID, tenantMemberID uuid.UUID,
	date time.Time,
) ([]types.ShiftMember, error) {
	d := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)

	var members []types.ShiftMember
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND tenant_member_id = ?", tenantID, tenantMemberID).
		Where("effective_from <= ?", d).
		Where("effective_to IS NULL OR effective_to >= ?", d).
		Find(&members).Error
	if err != nil {
		return nil, fmt.Errorf("shifts: find active assignments: %w", err)
	}
	return members, nil
}

// FindOverridesForShiftInRange returns all overrides for a shift within a date range.
func (r *Repository) FindOverridesForShiftInRange(
	ctx context.Context,
	shiftID uuid.UUID,
	startDate, endDate time.Time,
) ([]types.ShiftOverride, error) {
	var overrides []types.ShiftOverride
	err := r.db.WithContext(ctx).
		Where("shift_id = ? AND start_date <= ? AND end_date >= ?", shiftID, endDate, startDate).
		Find(&overrides).Error
	if err != nil {
		return nil, fmt.Errorf("shifts: find overrides in range: %w", err)
	}
	return overrides, nil
}

// FindShiftsStartingBetween returns all shifts (across all tenants) whose
// start_time falls within the given window. Used by the reminders cron.
func (r *Repository) FindShiftsStartingBetween(ctx context.Context, fromTime, toTime string) ([]types.Shift, error) {
	var shifts []types.Shift
	err := r.db.WithContext(ctx).
		Where("start_time >= ? AND start_time <= ?", fromTime, toTime).
		Preload("Members", "deleted_at IS NULL AND (effective_to IS NULL OR effective_to >= ?)", time.Now()).
		Find(&shifts).Error
	if err != nil {
		return nil, fmt.Errorf("shifts: find shifts starting between: %w", err)
	}
	return shifts, nil
}

// FindAllActiveAssignmentsInRange returns all active member-shift pairs for
// a tenant within a date range. Used for the roster endpoint.
func (r *Repository) FindAllActiveAssignmentsInRange(
	ctx context.Context,
	tenantID uuid.UUID,
	startDate, endDate time.Time,
	shiftID *uuid.UUID,
) ([]types.ShiftMember, error) {
	query := r.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Where("effective_from <= ?", endDate).
		Where("effective_to IS NULL OR effective_to >= ?", startDate)

	if shiftID != nil {
		query = query.Where("shift_id = ?", *shiftID)
	}

	var members []types.ShiftMember
	if err := query.Find(&members).Error; err != nil {
		return nil, fmt.Errorf("shifts: find active assignments in range: %w", err)
	}
	return members, nil
}
