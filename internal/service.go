package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/edgescaleDev/kernel/sdk"
	"github.com/google/uuid"
	"github.com/kernel-contrib/shifts/types"
)

// Service provides business logic for the shifts module.
type Service struct {
	repo   *Repository
	bus    sdk.EventBus
	redis  sdk.NamespacedRedis
	config func(uuid.UUID) map[string]any
	log    *slog.Logger
}

// NewService constructs a Service.
func NewService(repo *Repository, bus sdk.EventBus, redis sdk.NamespacedRedis, config func(uuid.UUID) map[string]any, log *slog.Logger) *Service {
	return &Service{repo: repo, bus: bus, redis: redis, config: config, log: log}
}

// ===============================================================================
// Shift CRUD
// ===============================================================================

// CreateShiftInput contains the fields for creating a new shift.
type CreateShiftInput struct {
	TenantID         uuid.UUID
	Title            string
	ShiftType        types.ShiftType
	StartDate        *time.Time
	EndDate          *time.Time
	WorkingDays      []int
	SpecificDates    []string // ["2026-05-05","2026-05-08"] - when set, overrides range + working_days
	StartTime        string
	EndTime          string
	WorkLocationType types.WorkLocationType
	Metadata         map[string]any
}

// CreateShift creates a new shift.
func (s *Service) CreateShift(ctx context.Context, in CreateShiftInput) (*types.Shift, error) {
	if in.Title == "" {
		return nil, sdk.BadRequest("title is required")
	}
	if in.ShiftType == types.ShiftTypeSpecificDates && len(in.SpecificDates) == 0 && (in.StartDate == nil || in.EndDate == nil) {
		return nil, sdk.BadRequest("specific_dates shifts require either specific_dates list or start_date and end_date")
	}

	wdJSON, err := json.Marshal(in.WorkingDays)
	if err != nil {
		return nil, fmt.Errorf("shifts: marshal working days: %w", err)
	}

	var sdJSON []byte
	if len(in.SpecificDates) > 0 {
		sdJSON, err = json.Marshal(in.SpecificDates)
		if err != nil {
			return nil, fmt.Errorf("shifts: marshal specific dates: %w", err)
		}
	} else {
		sdJSON = []byte("[]")
	}

	var metaJSON []byte
	if in.Metadata != nil {
		metaJSON, err = json.Marshal(in.Metadata)
		if err != nil {
			return nil, fmt.Errorf("shifts: marshal metadata: %w", err)
		}
	} else {
		metaJSON = []byte("{}")
	}

	shift := &types.Shift{
		TenantID:         in.TenantID,
		Title:            in.Title,
		ShiftType:        in.ShiftType,
		StartDate:        in.StartDate,
		EndDate:          in.EndDate,
		WorkingDays:      wdJSON,
		SpecificDates:    sdJSON,
		StartTime:        in.StartTime,
		EndTime:          in.EndTime,
		WorkLocationType: in.WorkLocationType,
		Metadata:         metaJSON,
	}

	if err := s.repo.CreateShift(ctx, shift); err != nil {
		return nil, err
	}

	s.publish(ctx, "shifts.shift.created", map[string]any{
		"shift_id":  shift.ID,
		"tenant_id": shift.TenantID,
		"title":     shift.Title,
	})

	return shift, nil
}

// GetShiftByID returns a shift by ID.
func (s *Service) GetShiftByID(ctx context.Context, tenantID, id uuid.UUID) (*types.Shift, error) {
	shift, err := s.repo.FindShiftByIDWithAssociations(ctx, tenantID, id)
	if IsNotFoundErr(err) {
		return nil, sdk.NotFound("shift", id)
	}
	return shift, err
}

// ListShifts returns a paginated list of shifts for a tenant.
func (s *Service) ListShifts(ctx context.Context, tenantID uuid.UUID, page sdk.PageRequest) (*sdk.PageResult[types.Shift], error) {
	return s.repo.ListShifts(ctx, tenantID, page)
}

// UpdateShiftInput is a partial update for shift fields.
type UpdateShiftInput struct {
	Title            *string
	ShiftType        *types.ShiftType
	StartDate        *time.Time
	EndDate          *time.Time
	WorkingDays      *[]int
	SpecificDates    *[]string
	StartTime        *string
	EndTime          *string
	WorkLocationType *types.WorkLocationType
	Metadata         map[string]any
}

// UpdateShift patches a shift.
func (s *Service) UpdateShift(ctx context.Context, tenantID, id uuid.UUID, in UpdateShiftInput) (*types.Shift, error) {
	// Verify existence.
	if _, err := s.repo.FindShiftByID(ctx, tenantID, id); IsNotFoundErr(err) {
		return nil, sdk.NotFound("shift", id)
	} else if err != nil {
		return nil, err
	}

	updates := make(map[string]any)
	if in.Title != nil {
		updates["title"] = *in.Title
	}
	if in.ShiftType != nil {
		updates["shift_type"] = *in.ShiftType
	}
	if in.StartDate != nil {
		updates["start_date"] = *in.StartDate
	}
	if in.EndDate != nil {
		updates["end_date"] = *in.EndDate
	}
	if in.WorkingDays != nil {
		wdJSON, err := json.Marshal(*in.WorkingDays)
		if err != nil {
			return nil, fmt.Errorf("shifts: marshal working days: %w", err)
		}
		updates["working_days"] = wdJSON
	}
	if in.SpecificDates != nil {
		sdJSON, err := json.Marshal(*in.SpecificDates)
		if err != nil {
			return nil, fmt.Errorf("shifts: marshal specific dates: %w", err)
		}
		updates["specific_dates"] = sdJSON
	}
	if in.StartTime != nil {
		updates["start_time"] = *in.StartTime
	}
	if in.EndTime != nil {
		updates["end_time"] = *in.EndTime
	}
	if in.WorkLocationType != nil {
		updates["work_location_type"] = *in.WorkLocationType
	}
	if in.Metadata != nil {
		metaJSON, err := json.Marshal(in.Metadata)
		if err != nil {
			return nil, fmt.Errorf("shifts: marshal metadata: %w", err)
		}
		updates["metadata"] = metaJSON
	}

	if len(updates) == 0 {
		return s.repo.FindShiftByID(ctx, tenantID, id)
	}

	shift, err := s.repo.UpdateShift(ctx, tenantID, id, updates)
	if err != nil {
		return nil, err
	}

	s.publish(ctx, "shifts.shift.updated", map[string]any{
		"shift_id":  id,
		"tenant_id": tenantID,
	})

	return shift, nil
}

// DeleteShift soft-deletes a shift with cascading delete of members and overrides.
func (s *Service) DeleteShift(ctx context.Context, tenantID, id uuid.UUID) error {
	if _, err := s.repo.FindShiftByID(ctx, tenantID, id); IsNotFoundErr(err) {
		return sdk.NotFound("shift", id)
	} else if err != nil {
		return err
	}

	if err := s.repo.SoftDeleteShift(ctx, tenantID, id); err != nil {
		return err
	}

	s.publish(ctx, "shifts.shift.deleted", map[string]any{
		"shift_id":  id,
		"tenant_id": tenantID,
	})

	return nil
}

// ===============================================================================
// Member Assignment
// ===============================================================================

// AssignMemberInput for assigning a single member to a shift.
type AssignMemberInput struct {
	TenantID       uuid.UUID
	ShiftID        uuid.UUID
	TenantMemberID uuid.UUID
	UserID         uuid.UUID // resolved from IAM reader
	EffectiveFrom  time.Time
	EffectiveTo    *time.Time
}

// AssignMember assigns a member to a shift after checking for conflicts.
func (s *Service) AssignMember(ctx context.Context, in AssignMemberInput) (*types.ShiftMember, error) {
	// Load the shift to get schedule details for conflict checks.
	shift, err := s.repo.FindShiftByID(ctx, in.TenantID, in.ShiftID)
	if IsNotFoundErr(err) {
		return nil, sdk.NotFound("shift", in.ShiftID)
	} else if err != nil {
		return nil, err
	}

	shiftDays := ParseWorkingDays(shift.WorkingDays)

	// 1. Cross-tenant conflict check (ALWAYS enforced).
	conflicts, err := s.repo.CheckCrossTenantConflict(
		ctx,
		in.UserID,
		shiftDays,
		shift.StartTime, shift.EndTime,
		in.EffectiveFrom, in.EffectiveTo,
	)
	if err != nil {
		return nil, err
	}
	// Filter out assignments to the same shift (not a conflict).
	var realConflicts []types.ShiftMember
	for _, c := range conflicts {
		if c.ShiftID != in.ShiftID {
			realConflicts = append(realConflicts, c)
		}
	}
	if len(realConflicts) > 0 {
		return nil, sdk.Conflict("this user has shift conflicts with a shift in another business")
	}

	// 2. Within-tenant overlap check (if config disallows overlapping).
	allowOverlap := s.getAllowOverlapping(in.TenantID)
	if !allowOverlap {
		overlaps, err := s.repo.CheckWithinTenantOverlap(
			ctx, in.TenantID, in.TenantMemberID,
			in.EffectiveFrom, in.EffectiveTo, &in.ShiftID,
		)
		if err != nil {
			return nil, err
		}
		if len(overlaps) > 0 {
			return nil, sdk.Conflict("this member already has an overlapping shift assignment in this business")
		}
	}

	// 3. Insert the assignment.
	member := &types.ShiftMember{
		TenantID:       in.TenantID,
		ShiftID:        in.ShiftID,
		TenantMemberID: in.TenantMemberID,
		UserID:         in.UserID,
		EffectiveFrom:  in.EffectiveFrom,
		EffectiveTo:    in.EffectiveTo,
	}

	if err := s.repo.AssignMember(ctx, member); err != nil {
		return nil, err
	}

	s.publish(ctx, "shifts.member.assigned", map[string]any{
		"shift_id":         in.ShiftID,
		"tenant_member_id": in.TenantMemberID,
		"tenant_id":        in.TenantID,
	})

	return member, nil
}

// RemoveMember removes a member assignment.
func (s *Service) RemoveMember(ctx context.Context, tenantID, membershipID uuid.UUID) error {
	member, err := s.repo.FindMemberByID(ctx, tenantID, membershipID)
	if IsNotFoundErr(err) {
		return sdk.NotFound("shift_member", membershipID)
	} else if err != nil {
		return err
	}

	if err := s.repo.RemoveMember(ctx, tenantID, membershipID); err != nil {
		return err
	}

	s.publish(ctx, "shifts.member.removed", map[string]any{
		"shift_id":         member.ShiftID,
		"tenant_member_id": member.TenantMemberID,
		"tenant_id":        tenantID,
	})

	return nil
}

// ListMembers returns all members of a shift.
func (s *Service) ListMembers(ctx context.Context, tenantID, shiftID uuid.UUID) ([]types.ShiftMember, error) {
	return s.repo.ListMembersByShift(ctx, tenantID, shiftID)
}

// ===============================================================================
// Overrides
// ===============================================================================

// CreateOverridesInput for creating overrides from a list of dates.
type CreateOverridesInput struct {
	TenantID        uuid.UUID
	ShiftID         uuid.UUID
	TenantMemberIDs []uuid.UUID // empty = all members (whole-shift override)
	Dates           []time.Time
	IsDayOff        bool
	NewStartTime    *string
	NewEndTime      *string
	Reason          *string
	Metadata        map[string]any
}

// CreateOverrides groups dates into contiguous ranges and creates overrides.
// If TenantMemberIDs is empty, creates whole-shift overrides (tenant_member_id = NULL).
// If TenantMemberIDs has entries, creates one override per member × date range.
// Returns 409 if any range conflicts with an existing override.
func (s *Service) CreateOverrides(ctx context.Context, in CreateOverridesInput) ([]types.ShiftOverride, error) {
	// Verify the shift exists.
	if _, err := s.repo.FindShiftByID(ctx, in.TenantID, in.ShiftID); IsNotFoundErr(err) {
		return nil, sdk.NotFound("shift", in.ShiftID)
	} else if err != nil {
		return nil, err
	}

	// Group dates into contiguous ranges.
	ranges := GroupContiguousDates(in.Dates)
	if len(ranges) == 0 {
		return nil, sdk.BadRequest("at least one date is required")
	}

	// Build the list of member pointers to iterate.
	// Empty = one nil entry (whole-shift override).
	memberPtrs := make([]*uuid.UUID, 0, len(in.TenantMemberIDs))
	if len(in.TenantMemberIDs) == 0 {
		memberPtrs = append(memberPtrs, nil) // whole-shift
	} else {
		for i := range in.TenantMemberIDs {
			memberPtrs = append(memberPtrs, &in.TenantMemberIDs[i])
		}
	}

	// Check for conflicts: each member × each range.
	for _, memberPtr := range memberPtrs {
		for _, r := range ranges {
			conflicts, err := s.repo.CheckOverrideConflict(
				ctx, in.ShiftID, memberPtr,
				r.Start, r.End, nil,
			)
			if err != nil {
				return nil, err
			}
			if len(conflicts) > 0 {
				conflictIDs := make([]string, len(conflicts))
				for i, c := range conflicts {
					conflictIDs[i] = c.ID.String()
				}
				return nil, sdk.Conflict(fmt.Sprintf(
					"override conflicts with existing overrides: %v", conflictIDs,
				))
			}
		}
	}

	// Build override records: members × ranges.
	var metaJSON []byte
	var err error
	if in.Metadata != nil {
		metaJSON, err = json.Marshal(in.Metadata)
		if err != nil {
			return nil, fmt.Errorf("shifts: marshal metadata: %w", err)
		}
	} else {
		metaJSON = []byte("{}")
	}

	overrides := make([]types.ShiftOverride, 0, len(memberPtrs)*len(ranges))
	for _, memberPtr := range memberPtrs {
		for _, r := range ranges {
			overrides = append(overrides, types.ShiftOverride{
				TenantID:       in.TenantID,
				ShiftID:        in.ShiftID,
				TenantMemberID: memberPtr,
				StartDate:      r.Start,
				EndDate:        r.End,
				IsDayOff:       in.IsDayOff,
				NewStartTime:   in.NewStartTime,
				NewEndTime:     in.NewEndTime,
				Reason:         in.Reason,
				Metadata:       metaJSON,
			})
		}
	}

	if err := s.repo.BulkCreateOverrides(ctx, overrides); err != nil {
		return nil, err
	}

	s.publish(ctx, "shifts.override.created", map[string]any{
		"shift_id":  in.ShiftID,
		"tenant_id": in.TenantID,
		"count":     len(overrides),
	})

	return overrides, nil
}

// UpdateOverrideInput for updating an existing override.
type UpdateOverrideInput struct {
	IsDayOff     *bool
	NewStartTime *string
	NewEndTime   *string
	Reason       *string
}

// UpdateOverride modifies an existing override.
func (s *Service) UpdateOverride(ctx context.Context, tenantID, id uuid.UUID, in UpdateOverrideInput) (*types.ShiftOverride, error) {
	existing, err := s.repo.FindOverrideByID(ctx, id)
	if IsNotFoundErr(err) {
		return nil, sdk.NotFound("shift_override", id)
	} else if err != nil {
		return nil, err
	}

	if existing.TenantID != tenantID {
		return nil, sdk.NotFound("shift_override", id)
	}

	updates := make(map[string]any)
	if in.IsDayOff != nil {
		updates["is_day_off"] = *in.IsDayOff
	}
	if in.NewStartTime != nil {
		updates["new_start_time"] = *in.NewStartTime
	}
	if in.NewEndTime != nil {
		updates["new_end_time"] = *in.NewEndTime
	}
	if in.Reason != nil {
		updates["reason"] = *in.Reason
	}

	if len(updates) == 0 {
		return existing, nil
	}

	override, err := s.repo.UpdateOverride(ctx, id, updates)
	if err != nil {
		return nil, err
	}

	s.publish(ctx, "shifts.override.updated", map[string]any{
		"override_id": id,
		"shift_id":    override.ShiftID,
		"tenant_id":   tenantID,
	})

	return override, nil
}

// DeleteOverride soft-deletes an override.
func (s *Service) DeleteOverride(ctx context.Context, tenantID, id uuid.UUID) error {
	existing, err := s.repo.FindOverrideByID(ctx, id)
	if IsNotFoundErr(err) {
		return sdk.NotFound("shift_override", id)
	} else if err != nil {
		return err
	}

	if existing.TenantID != tenantID {
		return sdk.NotFound("shift_override", id)
	}

	if err := s.repo.SoftDeleteOverride(ctx, tenantID, id); err != nil {
		return err
	}

	s.publish(ctx, "shifts.override.deleted", map[string]any{
		"override_id": id,
		"shift_id":    existing.ShiftID,
		"tenant_id":   tenantID,
	})

	return nil
}

// ListOverrides returns all overrides for a shift.
func (s *Service) ListOverrides(ctx context.Context, tenantID, shiftID uuid.UUID) ([]types.ShiftOverride, error) {
	return s.repo.ListOverridesByShift(ctx, tenantID, shiftID)
}

// ===============================================================================
// Roster & Schedule
// ===============================================================================

// MaxRosterDays is the maximum number of days for a roster query (6 calendar weeks).
const MaxRosterDays = 42

// GetRoster returns the resolved roster for all members in a tenant
// within a date range. Applies override resolution hierarchy.
func (s *Service) GetRoster(
	ctx context.Context,
	tenantID uuid.UUID,
	startDate, endDate time.Time,
	shiftID *uuid.UUID,
) ([]types.ResolvedShift, error) {
	days := int(endDate.Sub(startDate).Hours()/24) + 1
	if days > MaxRosterDays || days < 1 {
		return nil, sdk.BadRequest(fmt.Sprintf("date range must be between 1 and %d days", MaxRosterDays))
	}

	// Find all active assignments in the range.
	assignments, err := s.repo.FindAllActiveAssignmentsInRange(ctx, tenantID, startDate, endDate, shiftID)
	if err != nil {
		return nil, err
	}

	return s.resolveAssignments(ctx, assignments, startDate, endDate)
}

// GetMySchedule returns the resolved schedule for a single member.
func (s *Service) GetMySchedule(
	ctx context.Context,
	tenantID, tenantMemberID uuid.UUID,
	startDate, endDate time.Time,
) ([]types.ResolvedShift, error) {
	days := int(endDate.Sub(startDate).Hours()/24) + 1
	if days > MaxRosterDays || days < 1 {
		return nil, sdk.BadRequest(fmt.Sprintf("date range must be between 1 and %d days", MaxRosterDays))
	}

	assignments, err := s.repo.FindAllActiveAssignmentsInRange(ctx, tenantID, startDate, endDate, nil)
	if err != nil {
		return nil, err
	}

	// Filter to the target member only.
	var myAssignments []types.ShiftMember
	for _, a := range assignments {
		if a.TenantMemberID == tenantMemberID {
			myAssignments = append(myAssignments, a)
		}
	}

	return s.resolveAssignments(ctx, myAssignments, startDate, endDate)
}

// resolveAssignments resolves assignments into ResolvedShift entries for each day.
func (s *Service) resolveAssignments(
	ctx context.Context,
	assignments []types.ShiftMember,
	startDate, endDate time.Time,
) ([]types.ResolvedShift, error) {
	if len(assignments) == 0 {
		return []types.ResolvedShift{}, nil
	}

	// Collect unique shift IDs.
	shiftMap := make(map[uuid.UUID]*types.Shift)
	overrideMap := make(map[uuid.UUID][]types.ShiftOverride)

	for _, a := range assignments {
		if _, exists := shiftMap[a.ShiftID]; !exists {
			shift, err := s.repo.FindShiftByID(ctx, a.TenantID, a.ShiftID)
			if err != nil {
				continue // Skip if shift is inaccessible.
			}
			shiftMap[a.ShiftID] = shift

			overrides, err := s.repo.FindOverridesForShiftInRange(ctx, a.ShiftID, startDate, endDate)
			if err != nil {
				return nil, err
			}
			overrideMap[a.ShiftID] = overrides
		}
	}

	// For each assignment, iterate through each day in the range.
	var resolved []types.ResolvedShift

	for _, a := range assignments {
		shift, ok := shiftMap[a.ShiftID]
		if !ok {
			continue
		}

		shiftDays := ParseWorkingDays(shift.WorkingDays)
		specificDates := ParseSpecificDates(shift.SpecificDates)
		useSpecificDates := shift.HasSpecificDates()
		overrides := overrideMap[a.ShiftID]

		for d := startDate; !d.After(endDate); d = d.AddDate(0, 0, 1) {
			day := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)

			// Check if this day is within the assignment window.
			if day.Before(truncateDate(a.EffectiveFrom)) {
				continue
			}
			if a.EffectiveTo != nil && day.After(truncateDate(*a.EffectiveTo)) {
				continue
			}

			// Check if this day is active for the shift.
			if shift.ShiftType == types.ShiftTypeSpecificDates {
				if useSpecificDates {
					// Explicit date list mode: only active on listed dates.
					if !IsSpecificDate(specificDates, day) {
						// Still check for overrides (an override might add a day).
						override := ResolveOverride(overrides, day, a.TenantMemberID)
						if override == nil {
							continue
						}
					}
				} else {
					// Range mode: within start_date/end_date window.
					if shift.StartDate != nil && day.Before(truncateDate(*shift.StartDate)) {
						continue
					}
					if shift.EndDate != nil && day.After(truncateDate(*shift.EndDate)) {
						continue
					}
				}
			}

			// Check override first (member-specific -> whole-shift -> base).
			override := ResolveOverride(overrides, day, a.TenantMemberID)

			rs := types.ResolvedShift{
				ShiftID:          shift.ID,
				ShiftTitle:       shift.Title,
				TenantID:         a.TenantID,
				TenantMemberID:   a.TenantMemberID,
				UserID:           a.UserID,
				Date:             day,
				StartTime:        shift.StartTime,
				EndTime:          shift.EndTime,
				IsOvernight:      shift.IsOvernight(),
				WorkLocationType: shift.WorkLocationType,
				Metadata:         shift.Metadata,
				GraceWindow:      s.resolveGraceWindow(shift.Metadata, a.TenantID),
			}

			if override != nil {
				rs.OverrideID = &override.ID
				rs.OverrideReason = override.Reason
				rs.IsDayOff = override.IsDayOff

				if override.NewStartTime != nil {
					rs.StartTime = *override.NewStartTime
				}
				if override.NewEndTime != nil {
					rs.EndTime = *override.NewEndTime
				}
				// Recalculate overnight with overridden times.
				rs.IsOvernight = rs.EndTime < rs.StartTime

				// If it's a day off, include it in results (UI needs to show it).
				resolved = append(resolved, rs)
				continue
			}

			// Base rules: check if this is a regular working day.
			// For specific_dates with explicit list, the date is already validated above.
			if useSpecificDates {
				// Already confirmed this date is in the list.
				resolved = append(resolved, rs)
			} else if IsWorkingDay(shiftDays, day) {
				resolved = append(resolved, rs)
			}
			// Else: not a working day, no override -> skip.
		}
	}

	return resolved, nil
}

// ===============================================================================
// Reader Support
// ===============================================================================

// ResolveShiftForDay returns the effective shift for a member on a specific date.
func (s *Service) ResolveShiftForDay(ctx context.Context, tenantID, tenantMemberID uuid.UUID, date time.Time) (*types.ResolvedShift, error) {
	d := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)

	assignments, err := s.repo.FindActiveShiftAssignments(ctx, tenantID, tenantMemberID, d)
	if err != nil {
		return nil, err
	}
	if len(assignments) == 0 {
		return nil, nil
	}

	// Resolve each assignment and return the first active one.
	results, err := s.resolveAssignments(ctx, assignments, d, d)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}

	return &results[0], nil
}

// GetShiftsByIDs returns a map of shifts keyed by their ID for the given tenant.
// Used by cross-module readers that need to enrich records with shift metadata.
func (s *Service) GetShiftsByIDs(ctx context.Context, tenantID uuid.UUID, shiftIDs []uuid.UUID) (map[uuid.UUID]types.Shift, error) {
	return s.repo.FindShiftsByIDs(ctx, tenantID, shiftIDs)
}

// GetShiftsStartingWithinHour returns all resolved shifts that start within
// the next 60 minutes. Used by the reminders cron.
func (s *Service) GetShiftsStartingWithinHour(ctx context.Context, now time.Time) ([]types.ResolvedShift, error) {
	fromTime := now.Format("15:04")
	toTime := now.Add(60 * time.Minute).Format("15:04")

	shifts, err := s.repo.FindShiftsStartingBetween(ctx, fromTime, toTime)
	if err != nil {
		return nil, err
	}

	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	var resolved []types.ResolvedShift
	for _, shift := range shifts {
		shiftDays := ParseWorkingDays(shift.WorkingDays)
		if !IsWorkingDay(shiftDays, today) {
			continue
		}

		// Check overrides for today.
		overrides, err := s.repo.FindOverridesForShiftInRange(ctx, shift.ID, today, today)
		if err != nil {
			s.log.Error("failed to fetch overrides for reminder", "shift_id", shift.ID, "error", err)
			continue
		}

		for _, m := range shift.Members {
			override := ResolveOverride(overrides, today, m.TenantMemberID)

			rs := types.ResolvedShift{
				ShiftID:          shift.ID,
				ShiftTitle:       shift.Title,
				TenantID:         shift.TenantID,
				TenantMemberID:   m.TenantMemberID,
				UserID:           m.UserID,
				Date:             today,
				StartTime:        shift.StartTime,
				EndTime:          shift.EndTime,
				IsOvernight:      shift.IsOvernight(),
				WorkLocationType: shift.WorkLocationType,
				Metadata:         shift.Metadata,
				GraceWindow:      s.resolveGraceWindow(shift.Metadata, shift.TenantID),
			}

			if override != nil {
				if override.IsDayOff {
					continue // Skip - day off, no reminder needed.
				}
				rs.OverrideID = &override.ID
				if override.NewStartTime != nil {
					rs.StartTime = *override.NewStartTime
				}
				if override.NewEndTime != nil {
					rs.EndTime = *override.NewEndTime
				}
			}

			resolved = append(resolved, rs)
		}
	}

	return resolved, nil
}

// ── internal ──────────────────────────────────────────────────────────────────

func (s *Service) publish(ctx context.Context, subject string, payload map[string]any) {
	if s.bus == nil {
		return
	}
	s.bus.Publish(ctx, subject, payload)
}

func (s *Service) getAllowOverlapping(tenantID uuid.UUID) bool {
	if s.config == nil {
		return false
	}
	cfg := s.config(tenantID)
	if v, ok := cfg["shifts.allow_overlapping_assignments"]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// resolveGraceWindow resolves each grace key through the cascade:
// shift.metadata -> tenant config -> hard default (5 min).
func (s *Service) resolveGraceWindow(shiftMeta sdk.JSONB, tenantID uuid.UUID) types.GraceWindow {
	// Parse shift-level metadata.
	var meta map[string]any
	if shiftMeta != nil {
		_ = json.Unmarshal(shiftMeta, &meta)
	}

	// Get tenant config.
	var tenantCfg map[string]any
	if s.config != nil {
		tenantCfg = s.config(tenantID)
	}

	return types.GraceWindow{
		EarlyCheckinAllowanceMins:  resolveGraceInt(meta, tenantCfg, "early_checkin_allowance_mins"),
		LateCheckinGraceMins:       resolveGraceInt(meta, tenantCfg, "late_checkin_grace_mins"),
		EarlyCheckoutAllowanceMins: resolveGraceInt(meta, tenantCfg, "early_checkout_allowance_mins"),
		LateCheckoutAllowanceMins:  resolveGraceInt(meta, tenantCfg, "late_checkout_allowance_mins"),
	}
}

// resolveGraceInt resolves a single grace key: shift metadata -> tenant config -> 5 min.
func resolveGraceInt(shiftMeta, tenantCfg map[string]any, key string) int {
	// 1. Shift-level override.
	if v := getMetadataInt(shiftMeta, key); v > 0 {
		return v
	}
	// 2. Tenant config (prefixed with "shifts.").
	if v := getMetadataInt(tenantCfg, "shifts."+key); v > 0 {
		return v
	}
	// 3. Hard default.
	return types.DefaultGraceMinutes
}

// getMetadataInt extracts an int from a map, handling JSON number types.
func getMetadataInt(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return 0
}
