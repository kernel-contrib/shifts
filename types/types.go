// Package types defines the shared domain types for the shifts module.
// It lives in its own sub-package so that reader consumers and other
// modules can import types without creating a cycle back to the parent package.
package types

import (
	"encoding/json"
	"time"

	"github.com/edgescaleDev/kernel/sdk"
	"github.com/google/uuid"
)

// ── Enums ─────────────────────────────────────────────────────────────────────

// WorkLocationType enumerates where an employee works during a shift.
type WorkLocationType string

const (
	LocationOnsite WorkLocationType = "onsite"
	LocationRemote WorkLocationType = "remote"
	LocationHybrid WorkLocationType = "hybrid"
	LocationField  WorkLocationType = "field"
)

// ShiftType enumerates schedule recurrence modes.
type ShiftType string

const (
	// ShiftTypePermanent repeats weekly on working_days with no end date required.
	ShiftTypePermanent ShiftType = "permanent"
	// ShiftTypeSpecificDates is active only between start_date and end_date.
	ShiftTypeSpecificDates ShiftType = "specific_dates"
)

// ── Shift ─────────────────────────────────────────────────────────────────────

// Shift is the core definition of a work schedule.
// All models embed sdk.BaseModel which provides:
//   - ID        uuid.UUID (primary key)
//   - CreatedAt time.Time
//   - UpdatedAt time.Time
//   - DeletedAt gorm.DeletedAt (soft deletes)
type Shift struct {
	sdk.BaseModel
	TenantID         uuid.UUID        `json:"tenant_id"          gorm:"type:uuid;not null"`
	Title            string           `json:"title"              gorm:"not null"`
	ShiftType        ShiftType        `json:"shift_type"         gorm:"not null;default:permanent"`
	StartDate        *time.Time       `json:"start_date,omitempty" gorm:"type:date"`
	EndDate          *time.Time       `json:"end_date,omitempty"   gorm:"type:date"`
	WorkingDays      sdk.JSONB        `json:"working_days"       gorm:"type:jsonb;not null;default:'[]'"`
	SpecificDates    sdk.JSONB        `json:"specific_dates"     gorm:"type:jsonb;not null;default:'[]'"` // ["2026-05-05","2026-05-08"]
	StartTime        string           `json:"start_time"         gorm:"type:time;not null"`
	EndTime          string           `json:"end_time"           gorm:"type:time;not null"`
	WorkLocationType WorkLocationType `json:"work_location_type" gorm:"not null;default:onsite"`
	Metadata         sdk.JSONB        `json:"metadata,omitempty" gorm:"type:jsonb;not null;default:'{}'"`

	// Associations (loaded on demand via Preload).
	Members   []ShiftMember   `json:"members,omitempty"   gorm:"foreignKey:ShiftID"`
	Overrides []ShiftOverride `json:"overrides,omitempty" gorm:"foreignKey:ShiftID"`
}

// IsOvernight returns true when the shift crosses midnight (end_time < start_time).
func (s *Shift) IsOvernight() bool {
	return s.EndTime < s.StartTime
}

// HasSpecificDates returns true if this shift uses an explicit date list
// rather than the range + working_days model.
func (s *Shift) HasSpecificDates() bool {
	if len(s.SpecificDates) == 0 {
		return false
	}
	// Check for JSON null or empty array.
	var dates []string
	if err := json.Unmarshal(s.SpecificDates, &dates); err != nil || len(dates) == 0 {
		return false
	}
	return true
}

// ── ShiftMember ───────────────────────────────────────────────────────────────

// ShiftMember binds a user to a shift with an effective date range.
type ShiftMember struct {
	sdk.BaseModel
	TenantID       uuid.UUID  `json:"tenant_id"        gorm:"type:uuid;not null"`
	ShiftID        uuid.UUID  `json:"shift_id"         gorm:"type:uuid;not null"`
	TenantMemberID uuid.UUID  `json:"tenant_member_id" gorm:"type:uuid;not null"`
	UserID         uuid.UUID  `json:"user_id"          gorm:"type:uuid;not null"` // global identity for cross-tenant conflict detection
	EffectiveFrom  time.Time  `json:"effective_from"    gorm:"type:date;not null;default:CURRENT_DATE"`
	EffectiveTo    *time.Time `json:"effective_to,omitempty" gorm:"type:date"`
}

// ── ShiftOverride ─────────────────────────────────────────────────────────────

// ShiftOverride represents a date-range exception to the base shift schedule.
// Resolution hierarchy: member-specific → whole-shift → base rules.
type ShiftOverride struct {
	sdk.BaseModel
	TenantID       uuid.UUID  `json:"tenant_id"                  gorm:"type:uuid;not null"`
	ShiftID        uuid.UUID  `json:"shift_id"                   gorm:"type:uuid;not null"`
	TenantMemberID *uuid.UUID `json:"tenant_member_id,omitempty" gorm:"type:uuid"`
	StartDate      time.Time  `json:"start_date"                 gorm:"type:date;not null"`
	EndDate        time.Time  `json:"end_date"                   gorm:"type:date;not null"`
	IsDayOff       bool       `json:"is_day_off"                 gorm:"not null;default:false"`
	NewStartTime   *string    `json:"new_start_time,omitempty"   gorm:"type:time"`
	NewEndTime     *string    `json:"new_end_time,omitempty"     gorm:"type:time"`
	Reason         *string    `json:"reason,omitempty"`
	Metadata       sdk.JSONB  `json:"metadata,omitempty"         gorm:"type:jsonb;not null;default:'{}'"`
}

// ── ResolvedShift (reader output) ─────────────────────────────────────────────

// ResolvedShift is the effective schedule for a specific member on a specific day,
// after applying the override resolution hierarchy.
type ResolvedShift struct {
	ShiftID          uuid.UUID        `json:"shift_id"`
	ShiftTitle       string           `json:"shift_title"`
	TenantID         uuid.UUID        `json:"tenant_id"`
	TenantMemberID   uuid.UUID        `json:"tenant_member_id"`
	UserID           uuid.UUID        `json:"user_id"`
	Date             time.Time        `json:"date"`
	StartTime        string           `json:"start_time"`
	EndTime          string           `json:"end_time"`
	IsDayOff         bool             `json:"is_day_off"`
	IsOvernight      bool             `json:"is_overnight"`
	WorkLocationType WorkLocationType `json:"work_location_type"`
	Metadata         sdk.JSONB        `json:"metadata,omitempty"`
	OverrideID       *uuid.UUID       `json:"override_id,omitempty"`
	OverrideReason   *string          `json:"override_reason,omitempty"`

	// GraceWindow contains the resolved grace periods for attendance validation.
	// Values are pre-resolved: shift.metadata → tenant config → hard default (5 min).
	GraceWindow GraceWindow `json:"grace_window"`
}

// GraceWindow defines the check-in / check-out tolerance windows.
// All values are in minutes. Pre-resolved from the shift→tenant→default cascade.
//
// Example: shift 08:00-16:00 with all values = 15
//
//	Check-in window:  [07:45 ──── 08:00 ──── 08:15]
//	Check-out window: [15:45 ──── 16:00 ──── 16:15]
type GraceWindow struct {
	// EarlyCheckinAllowanceMins: minutes before start_time a check-in is accepted.
	EarlyCheckinAllowanceMins int `json:"early_checkin_allowance_mins"`
	// LateCheckinGraceMins: minutes after start_time before marking as late.
	LateCheckinGraceMins int `json:"late_checkin_grace_mins"`
	// EarlyCheckoutAllowanceMins: minutes before end_time a check-out is accepted.
	EarlyCheckoutAllowanceMins int `json:"early_checkout_allowance_mins"`
	// LateCheckoutAllowanceMins: minutes after end_time a check-out is accepted.
	LateCheckoutAllowanceMins int `json:"late_checkout_allowance_mins"`
}

// DefaultGraceMinutes is the hard default when neither shift nor tenant config specifies a value.
const DefaultGraceMinutes = 5
