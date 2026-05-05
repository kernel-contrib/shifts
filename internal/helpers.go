package internal

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/edgescaleDev/kernel/sdk"
	"github.com/google/uuid"
	"github.com/kernel-contrib/shifts/types"
	"gorm.io/gorm"
)

// ── Error helpers ─────────────────────────────────────────────────────────────

// IsNotFoundErr checks whether the error chain contains gorm.ErrRecordNotFound.
func IsNotFoundErr(err error) bool {
	return err != nil && errors.Is(err, gorm.ErrRecordNotFound)
}

// IsDuplicateError detects unique-constraint violations across both
// PostgreSQL (SQLSTATE 23505) and SQLite (UNIQUE constraint failed).
func IsDuplicateError(err error) bool {
	if err == nil {
		return false
	}
	if containsErrCode(err, "23505") {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "unique constraint")
}

func containsErrCode(err error, code string) bool {
	type pgErr interface{ SQLState() string }
	var pe pgErr
	if errors.As(err, &pe) {
		return pe.SQLState() == code
	}
	return false
}

// ── Date range helpers ────────────────────────────────────────────────────────

// DateRange represents a contiguous block of dates.
type DateRange struct {
	Start time.Time
	End   time.Time
}

// GroupContiguousDates takes a slice of dates and groups them into contiguous
// date ranges. For example, [May 16, May 17, May 20, May 21, May 25] becomes:
// [{May 16, May 17}, {May 20, May 21}, {May 25, May 25}].
func GroupContiguousDates(dates []time.Time) []DateRange {
	if len(dates) == 0 {
		return nil
	}

	// Normalize to date-only (strip time component) and sort.
	normalized := make([]time.Time, len(dates))
	for i, d := range dates {
		normalized[i] = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
	}
	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i].Before(normalized[j])
	})

	// Deduplicate.
	deduped := []time.Time{normalized[0]}
	for i := 1; i < len(normalized); i++ {
		if !normalized[i].Equal(normalized[i-1]) {
			deduped = append(deduped, normalized[i])
		}
	}

	// Group contiguous.
	var ranges []DateRange
	start := deduped[0]
	end := deduped[0]

	for i := 1; i < len(deduped); i++ {
		nextDay := end.AddDate(0, 0, 1)
		if deduped[i].Equal(nextDay) {
			end = deduped[i]
		} else {
			ranges = append(ranges, DateRange{Start: start, End: end})
			start = deduped[i]
			end = deduped[i]
		}
	}
	ranges = append(ranges, DateRange{Start: start, End: end})

	return ranges
}

// ── Working day helpers ───────────────────────────────────────────────────────

// ParseWorkingDays decodes the JSONB working_days array into a Go int slice.
func ParseWorkingDays(raw sdk.JSONB) []int {
	var days []int
	if raw == nil {
		return days
	}
	// sdk.JSONB is []byte — unmarshal the JSON int array.
	if err := json.Unmarshal(raw, &days); err != nil {
		return nil
	}
	return days
}

// ParseSpecificDates decodes the JSONB specific_dates array into a Go time slice.
// Dates are stored as "YYYY-MM-DD" strings in JSON.
func ParseSpecificDates(raw sdk.JSONB) []time.Time {
	if len(raw) <= 2 {
		return nil
	}
	var dateStrings []string
	if err := json.Unmarshal(raw, &dateStrings); err != nil {
		return nil
	}
	dates := make([]time.Time, 0, len(dateStrings))
	for _, s := range dateStrings {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			continue
		}
		dates = append(dates, t)
	}
	return dates
}

// IsSpecificDate checks if the given date is in the specific dates list.
func IsSpecificDate(specificDates []time.Time, date time.Time) bool {
	d := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	for _, sd := range specificDates {
		sd = time.Date(sd.Year(), sd.Month(), sd.Day(), 0, 0, 0, 0, time.UTC)
		if sd.Equal(d) {
			return true
		}
	}
	return false
}

// IsWorkingDay checks if the given date falls on one of the shift's working days.
// Uses ISO weekday numbering: Monday=1, Sunday=7.
func IsWorkingDay(workingDays []int, date time.Time) bool {
	// Go's time.Weekday: Sunday=0, Monday=1, ..., Saturday=6.
	// ISO: Monday=1, ..., Sunday=7.
	wd := int(date.Weekday())
	if wd == 0 {
		wd = 7 // Sunday → 7
	}
	for _, d := range workingDays {
		if d == wd {
			return true
		}
	}
	return false
}

// ── Time overlap helpers ──────────────────────────────────────────────────────

// TimeOverlaps checks if two time windows overlap, accounting for overnight shifts.
// Times are "HH:MM" strings. A shift is overnight if end < start.
func TimeOverlaps(startA, endA, startB, endB string) bool {
	// Convert to minutes since midnight for easier comparison.
	sA := timeToMinutes(startA)
	eA := timeToMinutes(endA)
	sB := timeToMinutes(startB)
	eB := timeToMinutes(endB)

	overnightA := eA <= sA
	overnightB := eB <= sB

	// Expand overnight shifts to a 0-2880 range (two days in minutes).
	if overnightA {
		eA += 1440 // Add 24 hours
	}
	if overnightB {
		eB += 1440
	}

	// Check overlap in the expanded range.
	if overlaps(sA, eA, sB, eB) {
		return true
	}

	// Also check if B shifted by +24h overlaps A (or vice versa) to handle
	// the case where one window spans midnight from "today" and the other
	// starts fresh "tomorrow".
	if overlaps(sA, eA, sB+1440, eB+1440) {
		return true
	}
	if overlaps(sA+1440, eA+1440, sB, eB) {
		return true
	}

	return false
}

func overlaps(s1, e1, s2, e2 int) bool {
	return s1 < e2 && s2 < e1
}

func timeToMinutes(t string) int {
	// Parse "HH:MM" format.
	if len(t) < 5 {
		return 0
	}
	h := int(t[0]-'0')*10 + int(t[1]-'0')
	m := int(t[3]-'0')*10 + int(t[4]-'0')
	return h*60 + m
}

// ── Date range overlap helpers ────────────────────────────────────────────────

// DateRangesOverlap checks if two date ranges overlap.
// A nil end date means "indefinite" (unbounded).
func DateRangesOverlap(fromA time.Time, toA *time.Time, fromB time.Time, toB *time.Time) bool {
	// Normalize to date-only.
	fA := truncateDate(fromA)
	fB := truncateDate(fromB)

	// Check: fromA <= endB (or endB is nil) AND fromB <= endA (or endA is nil)
	if toB != nil {
		tB := truncateDate(*toB)
		if fA.After(tB) {
			return false
		}
	}
	if toA != nil {
		tA := truncateDate(*toA)
		if fB.After(tA) {
			return false
		}
	}
	return true
}

func truncateDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// ── Override resolution ───────────────────────────────────────────────────────

// ResolveOverride finds the highest-priority override for a specific date and member.
// Priority: member-specific → whole-shift (tenant_member_id IS NULL).
// Returns nil if no override applies.
func ResolveOverride(overrides []types.ShiftOverride, date time.Time, memberID uuid.UUID) *types.ShiftOverride {
	d := truncateDate(date)

	// First pass: member-specific override.
	for i := range overrides {
		o := &overrides[i]
		if o.TenantMemberID == nil {
			continue
		}
		if *o.TenantMemberID != memberID {
			continue
		}
		if !d.Before(truncateDate(o.StartDate)) && !d.After(truncateDate(o.EndDate)) {
			return o
		}
	}

	// Second pass: whole-shift override.
	for i := range overrides {
		o := &overrides[i]
		if o.TenantMemberID != nil {
			continue
		}
		if !d.Before(truncateDate(o.StartDate)) && !d.After(truncateDate(o.EndDate)) {
			return o
		}
	}

	return nil
}

// ── Working days overlap ──────────────────────────────────────────────────────

// WorkingDaysOverlap checks if two sets of ISO working day numbers share any day.
func WorkingDaysOverlap(a, b []int) bool {
	set := make(map[int]bool, len(a))
	for _, d := range a {
		set[d] = true
	}
	for _, d := range b {
		if set[d] {
			return true
		}
	}
	return false
}
