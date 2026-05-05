package shifts

import "github.com/edgescaleDev/kernel/sdk"

// RouteHandlers returns the route handler registrations for this module.
func (m *Module) RouteHandlers() []sdk.RouteHandler {
	return []sdk.RouteHandler{
		{Type: sdk.RouteClient, Register: m.registerClientRoutes},
	}
}

func (m *Module) registerClientRoutes(r *sdk.Router) {
	// Tenant-scoped routes
	t := r.Tenant()

	// Shifts CRUD.
	t.GET("", "shifts.shifts.read", m.handleListShifts)
	t.POST("", "shifts.shifts.manage", m.handleCreateShift)

	// Static paths MUST be registered before /:id to avoid conflicts.
	// Roster (manager view).
	t.GET("/roster", "shifts.roster.read", m.handleGetRoster)
	// My Schedule - employee self-view (authenticated, tenant-scoped).
	t.GET("/my-schedule", sdk.Self, m.handleMySchedule)

	// Parameterized routes.
	t.GET("/:id", "shifts.shifts.read", m.handleGetShift)
	t.PUT("/:id", "shifts.shifts.manage", m.handleUpdateShift)
	t.DELETE("/:id", "shifts.shifts.manage", m.handleDeleteShift)

	// Members.
	t.GET("/:id/members", "shifts.members.read", m.handleListMembers)
	t.POST("/:id/members", "shifts.members.manage", m.handleAssignMembers)
	t.DELETE("/:id/members/:member_id", "shifts.members.manage", m.handleRemoveMember)

	// Overrides.
	t.GET("/:id/overrides", "shifts.overrides.read", m.handleListOverrides)
	t.POST("/:id/overrides", "shifts.overrides.manage", m.handleCreateOverrides)
	t.PUT("/:id/overrides", "shifts.overrides.manage", m.handleUpdateOverride)
	t.DELETE("/:id/overrides", "shifts.overrides.manage", m.handleDeleteOverride)
}
