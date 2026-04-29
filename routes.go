package shifts

import "go.edgescale.dev/kernel/sdk"

// RouteHandlers returns the route handler registrations for this module.
func (m *Module) RouteHandlers() []sdk.RouteHandler {
	return []sdk.RouteHandler{
		{Type: sdk.RouteClient, Register: m.registerClientRoutes},
	}
}

func (m *Module) registerClientRoutes(r *sdk.Router) {
	// ── Global routes (not tenant-scoped) ─────────────────────────────────
	// My Schedule — employee self-view (any authenticated user).
	r.GET("/my-schedule", sdk.Self, m.handleMySchedule)

	// ── Tenant-scoped routes ──────────────────────────────────────────────
	t := r.Tenant()

	// Shifts CRUD.
	t.GET("/shifts", "shifts.shifts.read", m.handleListShifts)
	t.POST("/shifts", "shifts.shifts.manage", m.handleCreateShift)
	t.GET("/shifts/:id", "shifts.shifts.read", m.handleGetShift)
	t.PUT("/shifts/:id", "shifts.shifts.manage", m.handleUpdateShift)
	t.DELETE("/shifts/:id", "shifts.shifts.manage", m.handleDeleteShift)

	// Members.
	t.GET("/shifts/:id/members", "shifts.members.read", m.handleListMembers)
	t.POST("/shifts/:id/members", "shifts.members.manage", m.handleAssignMembers)
	t.DELETE("/shifts/:id/members/:member_id", "shifts.members.manage", m.handleRemoveMember)

	// Overrides.
	t.GET("/shifts/:id/overrides", "shifts.overrides.read", m.handleListOverrides)
	t.POST("/shifts/:id/overrides", "shifts.overrides.manage", m.handleCreateOverrides)
	t.PUT("/overrides/:id", "shifts.overrides.manage", m.handleUpdateOverride)
	t.DELETE("/overrides/:id", "shifts.overrides.manage", m.handleDeleteOverride)

	// Roster (manager view).
	t.GET("/roster", "shifts.roster.read", m.handleGetRoster)
}
