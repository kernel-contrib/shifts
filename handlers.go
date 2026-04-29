package shifts

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/kernel-contrib/shifts/internal"
	"github.com/kernel-contrib/shifts/types"
	"go.edgescale.dev/kernel/sdk"
)

// == Request types ==============================================================================================

type createShiftRequest struct {
	Title            string         `json:"title" binding:"required"`
	ShiftType        string         `json:"shift_type" binding:"required,oneof=permanent specific_dates"`
	StartDate        *string        `json:"start_date"`
	EndDate          *string        `json:"end_date"`
	WorkingDays      []int          `json:"working_days"`
	SpecificDates    []string       `json:"specific_dates"` // ["2026-05-05","2026-05-08"]
	StartTime        string         `json:"start_time" binding:"required"`
	EndTime          string         `json:"end_time" binding:"required"`
	WorkLocationType string         `json:"work_location_type" binding:"required,oneof=onsite remote hybrid field"`
	Metadata         map[string]any `json:"metadata"`
}

type updateShiftRequest struct {
	Title            *string        `json:"title"`
	ShiftType        *string        `json:"shift_type" binding:"omitempty,oneof=permanent specific_dates"`
	StartDate        *string        `json:"start_date"`
	EndDate          *string        `json:"end_date"`
	WorkingDays      *[]int         `json:"working_days"`
	StartTime        *string        `json:"start_time"`
	EndTime          *string        `json:"end_time"`
	WorkLocationType *string        `json:"work_location_type" binding:"omitempty,oneof=onsite remote hybrid field"`
	Metadata         map[string]any `json:"metadata"`
}

type assignMembersRequest struct {
	MemberIDs     []uuid.UUID `json:"member_ids" binding:"required,min=1"`
	EffectiveFrom *string     `json:"effective_from"`
	EffectiveTo   *string     `json:"effective_to"`
}

type createOverridesRequest struct {
	Dates           []string       `json:"dates" binding:"required,min=1"`
	TenantMemberIDs []uuid.UUID    `json:"tenant_member_ids"` // empty/null = all members
	IsDayOff        bool           `json:"is_day_off"`
	NewStartTime    *string        `json:"new_start_time"`
	NewEndTime      *string        `json:"new_end_time"`
	Reason          *string        `json:"reason"`
	Metadata        map[string]any `json:"metadata"`
}

type updateOverrideRequest struct {
	IsDayOff     *bool   `json:"is_day_off"`
	NewStartTime *string `json:"new_start_time"`
	NewEndTime   *string `json:"new_end_time"`
	Reason       *string `json:"reason"`
}

type rosterQuery struct {
	StartDate string     `form:"start_date" binding:"required"`
	EndDate   string     `form:"end_date" binding:"required"`
	ShiftID   *uuid.UUID `form:"shift_id"`
}

type myScheduleQuery struct {
	StartDate string `form:"start_date" binding:"required"`
	EndDate   string `form:"end_date" binding:"required"`
}

// == Shift Handlers ==============================================================================================

func (m *Module) handleListShifts(c *gin.Context) {
	tid := tenantID(c)
	page := sdk.ParsePageRequest(c)

	result, err := m.svc.ListShifts(c.Request.Context(), tid, page)
	if err != nil {
		sdk.FromError(c, err)
		return
	}

	sdk.List(c, result.Items, result.Meta)
}

func (m *Module) handleCreateShift(c *gin.Context) {
	tid := tenantID(c)

	var req createShiftRequest
	if !sdk.BindAndValidate(c, &req) {
		return
	}

	input := internal.CreateShiftInput{
		TenantID:         tid,
		Title:            req.Title,
		ShiftType:        types.ShiftType(req.ShiftType),
		WorkingDays:      req.WorkingDays,
		SpecificDates:    req.SpecificDates,
		StartTime:        req.StartTime,
		EndTime:          req.EndTime,
		WorkLocationType: types.WorkLocationType(req.WorkLocationType),
		Metadata:         req.Metadata,
	}

	if req.StartDate != nil {
		t, err := parseDate(*req.StartDate)
		if err != nil {
			sdk.Error(c, sdk.BadRequest("invalid start_date format, expected YYYY-MM-DD"))
			return
		}
		input.StartDate = &t
	}
	if req.EndDate != nil {
		t, err := parseDate(*req.EndDate)
		if err != nil {
			sdk.Error(c, sdk.BadRequest("invalid end_date format, expected YYYY-MM-DD"))
			return
		}
		input.EndDate = &t
	}

	shift, err := m.svc.CreateShift(c.Request.Context(), input)
	if err != nil {
		sdk.FromError(c, err)
		return
	}

	m.ctx.Audit.Log(c.Request.Context(), sdk.AuditEntry{
		Action:     sdk.AuditCreate,
		Resource:   "shift",
		ResourceID: shift.ID.String(),
	})

	sdk.Created(c, shift)
}

func (m *Module) handleGetShift(c *gin.Context) {
	tid := tenantID(c)
	id, err := parseUUID(c, "id")
	if err != nil {
		return
	}

	shift, err := m.svc.GetShiftByID(c.Request.Context(), tid, id)
	if err != nil {
		sdk.FromError(c, err)
		return
	}

	sdk.OK(c, shift)
}

func (m *Module) handleUpdateShift(c *gin.Context) {
	tid := tenantID(c)
	id, err := parseUUID(c, "id")
	if err != nil {
		return
	}

	var req updateShiftRequest
	if !sdk.BindAndValidate(c, &req) {
		return
	}

	input := internal.UpdateShiftInput{
		Title:    req.Title,
		Metadata: req.Metadata,
	}

	if req.ShiftType != nil {
		st := types.ShiftType(*req.ShiftType)
		input.ShiftType = &st
	}
	if req.StartDate != nil {
		t, err := parseDate(*req.StartDate)
		if err != nil {
			sdk.Error(c, sdk.BadRequest("invalid start_date format, expected YYYY-MM-DD"))
			return
		}
		input.StartDate = &t
	}
	if req.EndDate != nil {
		t, err := parseDate(*req.EndDate)
		if err != nil {
			sdk.Error(c, sdk.BadRequest("invalid end_date format, expected YYYY-MM-DD"))
			return
		}
		input.EndDate = &t
	}
	if req.WorkingDays != nil {
		input.WorkingDays = req.WorkingDays
	}
	if req.StartTime != nil {
		input.StartTime = req.StartTime
	}
	if req.EndTime != nil {
		input.EndTime = req.EndTime
	}
	if req.WorkLocationType != nil {
		wlt := types.WorkLocationType(*req.WorkLocationType)
		input.WorkLocationType = &wlt
	}

	shift, err := m.svc.UpdateShift(c.Request.Context(), tid, id, input)
	if err != nil {
		sdk.FromError(c, err)
		return
	}

	m.ctx.Audit.Log(c.Request.Context(), sdk.AuditEntry{
		Action:     sdk.AuditUpdate,
		Resource:   "shift",
		ResourceID: id.String(),
	})

	sdk.OK(c, shift)
}

func (m *Module) handleDeleteShift(c *gin.Context) {
	tid := tenantID(c)
	id, err := parseUUID(c, "id")
	if err != nil {
		return
	}

	if err := m.svc.DeleteShift(c.Request.Context(), tid, id); err != nil {
		sdk.FromError(c, err)
		return
	}

	m.ctx.Audit.Log(c.Request.Context(), sdk.AuditEntry{
		Action:     sdk.AuditDelete,
		Resource:   "shift",
		ResourceID: id.String(),
	})

	sdk.NoContent(c)
}

// ── Member Handlers ───────────────────────────────────────────────────────────

func (m *Module) handleListMembers(c *gin.Context) {
	tid := tenantID(c)
	shiftID, err := parseUUID(c, "id")
	if err != nil {
		return
	}

	members, err := m.svc.ListMembers(c.Request.Context(), tid, shiftID)
	if err != nil {
		sdk.FromError(c, err)
		return
	}

	sdk.OK(c, members)
}

func (m *Module) handleAssignMembers(c *gin.Context) {
	tid := tenantID(c)
	shiftID, err := parseUUID(c, "id")
	if err != nil {
		return
	}

	var req assignMembersRequest
	if !sdk.BindAndValidate(c, &req) {
		return
	}

	// Parse effective dates.
	effectiveFrom := time.Now()
	if req.EffectiveFrom != nil {
		ef, err := parseDate(*req.EffectiveFrom)
		if err != nil {
			sdk.Error(c, sdk.BadRequest("invalid effective_from format, expected YYYY-MM-DD"))
			return
		}
		effectiveFrom = ef
	}

	var effectiveTo *time.Time
	if req.EffectiveTo != nil {
		et, err := parseDate(*req.EffectiveTo)
		if err != nil {
			sdk.Error(c, sdk.BadRequest("invalid effective_to format, expected YYYY-MM-DD"))
			return
		}
		effectiveTo = &et
	}

	// Resolve each tenant_member_id → user_id via IAM reader.
	// The IAM reader provides the global user identity needed for cross-tenant conflict detection.
	iamReader, iamErr := sdk.Reader[iamMemberReader](&m.ctx, "iam")

	var assigned []types.ShiftMember
	for _, memberID := range req.MemberIDs {
		// Resolve user_id from IAM.
		var userID uuid.UUID
		if iamErr == nil {
			uid, err := iamReader.GetMemberUserID(c.Request.Context(), memberID)
			if err != nil {
				sdk.Error(c, sdk.BadRequest(fmt.Sprintf("could not resolve user for member %s", memberID)))
				return
			}
			userID = uid
		} else {
			// Fallback: use tenant_member_id as user_id (development/testing).
			m.ctx.Logger.Warn("IAM reader unavailable, using tenant_member_id as user_id fallback",
				"tenant_member_id", memberID)
			userID = memberID
		}

		member, err := m.svc.AssignMember(c.Request.Context(), internal.AssignMemberInput{
			TenantID:       tid,
			ShiftID:        shiftID,
			TenantMemberID: memberID,
			UserID:         userID,
			EffectiveFrom:  effectiveFrom,
			EffectiveTo:    effectiveTo,
		})
		if err != nil {
			sdk.FromError(c, err)
			return
		}

		m.ctx.Audit.Log(c.Request.Context(), sdk.AuditEntry{
			Action:     sdk.AuditCreate,
			Resource:   "shift_member",
			ResourceID: member.ID.String(),
		})

		assigned = append(assigned, *member)
	}

	sdk.Created(c, assigned)
}

func (m *Module) handleRemoveMember(c *gin.Context) {
	tid := tenantID(c)
	membershipID, err := parseUUID(c, "member_id")
	if err != nil {
		return
	}

	if err := m.svc.RemoveMember(c.Request.Context(), tid, membershipID); err != nil {
		sdk.FromError(c, err)
		return
	}

	m.ctx.Audit.Log(c.Request.Context(), sdk.AuditEntry{
		Action:     sdk.AuditDelete,
		Resource:   "shift_member",
		ResourceID: membershipID.String(),
	})

	sdk.NoContent(c)
}

// ── Override Handlers ─────────────────────────────────────────────────────────

func (m *Module) handleListOverrides(c *gin.Context) {
	tid := tenantID(c)
	shiftID, err := parseUUID(c, "id")
	if err != nil {
		return
	}

	overrides, err := m.svc.ListOverrides(c.Request.Context(), tid, shiftID)
	if err != nil {
		sdk.FromError(c, err)
		return
	}

	sdk.OK(c, overrides)
}

func (m *Module) handleCreateOverrides(c *gin.Context) {
	tid := tenantID(c)
	shiftID, err := parseUUID(c, "id")
	if err != nil {
		return
	}

	var req createOverridesRequest
	if !sdk.BindAndValidate(c, &req) {
		return
	}

	// Parse date strings.
	dates := make([]time.Time, len(req.Dates))
	for i, ds := range req.Dates {
		d, err := parseDate(ds)
		if err != nil {
			sdk.Error(c, sdk.BadRequest(fmt.Sprintf("invalid date format at index %d, expected YYYY-MM-DD", i)))
			return
		}
		dates[i] = d
	}

	overrides, err := m.svc.CreateOverrides(c.Request.Context(), internal.CreateOverridesInput{
		TenantID:        tid,
		ShiftID:         shiftID,
		TenantMemberIDs: req.TenantMemberIDs,
		Dates:           dates,
		IsDayOff:        req.IsDayOff,
		NewStartTime:    req.NewStartTime,
		NewEndTime:      req.NewEndTime,
		Reason:          req.Reason,
		Metadata:        req.Metadata,
	})
	if err != nil {
		sdk.FromError(c, err)
		return
	}

	m.ctx.Audit.Log(c.Request.Context(), sdk.AuditEntry{
		Action:     sdk.AuditCreate,
		Resource:   "shift_override",
		ResourceID: fmt.Sprintf("bulk:%d", len(overrides)),
	})

	sdk.Created(c, overrides)
}

func (m *Module) handleUpdateOverride(c *gin.Context) {
	tid := tenantID(c)
	id, err := parseUUID(c, "id")
	if err != nil {
		return
	}

	var req updateOverrideRequest
	if !sdk.BindAndValidate(c, &req) {
		return
	}

	override, err := m.svc.UpdateOverride(c.Request.Context(), tid, id, internal.UpdateOverrideInput{
		IsDayOff:     req.IsDayOff,
		NewStartTime: req.NewStartTime,
		NewEndTime:   req.NewEndTime,
		Reason:       req.Reason,
	})
	if err != nil {
		sdk.FromError(c, err)
		return
	}

	m.ctx.Audit.Log(c.Request.Context(), sdk.AuditEntry{
		Action:     sdk.AuditUpdate,
		Resource:   "shift_override",
		ResourceID: id.String(),
	})

	sdk.OK(c, override)
}

func (m *Module) handleDeleteOverride(c *gin.Context) {
	tid := tenantID(c)
	id, err := parseUUID(c, "id")
	if err != nil {
		return
	}

	if err := m.svc.DeleteOverride(c.Request.Context(), tid, id); err != nil {
		sdk.FromError(c, err)
		return
	}

	m.ctx.Audit.Log(c.Request.Context(), sdk.AuditEntry{
		Action:     sdk.AuditDelete,
		Resource:   "shift_override",
		ResourceID: id.String(),
	})

	sdk.NoContent(c)
}

// ── Roster & Schedule Handlers ────────────────────────────────────────────────

func (m *Module) handleGetRoster(c *gin.Context) {
	tid := tenantID(c)

	var q rosterQuery
	if !sdk.BindQuery(c, &q) {
		return
	}

	startDate, err := parseDate(q.StartDate)
	if err != nil {
		sdk.Error(c, sdk.BadRequest("invalid start_date format, expected YYYY-MM-DD"))
		return
	}
	endDate, err := parseDate(q.EndDate)
	if err != nil {
		sdk.Error(c, sdk.BadRequest("invalid end_date format, expected YYYY-MM-DD"))
		return
	}

	roster, err := m.svc.GetRoster(c.Request.Context(), tid, startDate, endDate, q.ShiftID)
	if err != nil {
		sdk.FromError(c, err)
		return
	}

	sdk.OK(c, roster)
}

func (m *Module) handleMySchedule(c *gin.Context) {
	var q myScheduleQuery
	if !sdk.BindQuery(c, &q) {
		return
	}

	startDate, err := parseDate(q.StartDate)
	if err != nil {
		sdk.Error(c, sdk.BadRequest("invalid start_date format, expected YYYY-MM-DD"))
		return
	}
	endDate, err := parseDate(q.EndDate)
	if err != nil {
		sdk.Error(c, sdk.BadRequest("invalid end_date format, expected YYYY-MM-DD"))
		return
	}

	// For the self-schedule endpoint, we need the internal_user_id and to find
	// which tenant(s) the user belongs to. This is resolved from context.
	internalUserID := c.MustGet("internal_user_id").(uuid.UUID)

	// The my-schedule endpoint is global (not tenant-scoped), so we need to
	// query all tenants the user belongs to. For now, we resolve via the
	// tenant_id if present, or return an error.
	tid, exists := c.Get("tenant_id")
	if !exists {
		sdk.Error(c, sdk.BadRequest("tenant_id is required for my-schedule"))
		return
	}
	tenantUUID := tid.(uuid.UUID)

	schedule, err := m.svc.GetMySchedule(c.Request.Context(), tenantUUID, internalUserID, startDate, endDate)
	if err != nil {
		sdk.FromError(c, err)
		return
	}

	sdk.OK(c, schedule)
}
