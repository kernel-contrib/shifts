package shifts

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/google/uuid"
)

// startReminderCron starts a 1-minute ticker that processes shift reminders.
func (m *Module) startReminderCron() {
	ticker := time.NewTicker(1 * time.Minute)
	m.reminderTicker = ticker

	go func() {
		for range ticker.C {
			m.processSmartReminders(context.Background())
		}
	}()

	m.ctx.Logger.Info("shifts: smart reminders cron started (1-minute interval)")
}

// stopReminderCron stops the reminder ticker.
func (m *Module) stopReminderCron() {
	if m.reminderTicker != nil {
		m.reminderTicker.Stop()
		m.ctx.Logger.Info("shifts: smart reminders cron stopped")
	}
}

// processSmartReminders runs a single iteration of the reminders loop.
//
// Logic:
//  1. Fetch all shifts starting within the next 60 minutes.
//  2. For each tenant, check configured reminder intervals.
//  3. The 30-minute reminder ALWAYS fires (baseline guarantee).
//  4. Other intervals fire if IS_WANTED (config) && IS_ALLOWED (billing, defaults true).
//  5. Publish shifts.reminder.dispatch event for each match.
func (m *Module) processSmartReminders(ctx context.Context) {
	now := time.Now()

	resolved, err := m.svc.GetShiftsStartingWithinHour(ctx, now)
	if err != nil {
		m.ctx.Logger.Error("shifts: reminder cron failed to fetch upcoming shifts", "error", err)
		return
	}

	if len(resolved) == 0 {
		return
	}

	for _, rs := range resolved {
		// Calculate minutes until shift starts.
		shiftStart, err := time.Parse("15:04", rs.StartTime)
		if err != nil {
			continue
		}
		nowTime, _ := time.Parse("15:04", now.Format("15:04"))
		minutesUntilStart := int(shiftStart.Sub(nowTime).Minutes())
		if minutesUntilStart < 0 {
			continue
		}

		// Get tenant's configured reminder intervals.
		wantedIntervals := m.getReminderMinutes(rs.TenantID)

		// Check if this minute matches any configured interval.
		shouldFire := false

		// The 30-minute reminder ALWAYS fires regardless of configuration.
		if minutesUntilStart == 30 {
			shouldFire = true
		}

		// Check other configured intervals.
		if !shouldFire {
			if slices.Contains(wantedIntervals, minutesUntilStart) {
				// IS_WANTED = true. Check IS_ALLOWED (billing).
				// Phase 1: IS_ALLOWED always true (no billing dependency).
				shouldFire = true
			}
		}

		if shouldFire {
			m.ctx.Bus.Publish(ctx, "shifts.reminder.dispatch", map[string]any{
				"shift_id":           rs.ShiftID,
				"shift_title":        rs.ShiftTitle,
				"tenant_id":          rs.TenantID,
				"tenant_member_id":   rs.TenantMemberID,
				"user_id":            rs.UserID,
				"start_time":         rs.StartTime,
				"minutes_until":      minutesUntilStart,
				"work_location_type": rs.WorkLocationType,
			})
		}
	}
}

// getReminderMinutes reads the configured reminder intervals for a tenant.
func (m *Module) getReminderMinutes(tenantID uuid.UUID) []int {
	defaults := []int{30, 15, 1}

	if m.ctx.Config == nil {
		return defaults
	}

	cfg := m.ctx.Config(tenantID)
	rawVal, ok := cfg["shifts.reminder_minutes"]
	if !ok {
		return defaults
	}

	// The config value is stored as a string (JSON array).
	str, ok := rawVal.(string)
	if !ok {
		return defaults
	}

	var intervals []int
	if err := json.Unmarshal([]byte(str), &intervals); err != nil {
		m.ctx.Logger.Warn("shifts: failed to parse reminder_minutes config",
			"tenant_id", tenantID, "value", str, "error", err)
		return defaults
	}

	return intervals
}
