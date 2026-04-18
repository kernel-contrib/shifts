package mymodule

import "go.edgescale.dev/kernel/sdk"

// RegisterRoutes mounts all HTTP endpoints on the kernel router.
//
// Routes are split into two groups:
//   - Global: authenticated, not tenant-scoped (/v1/mymodule/...)
//   - Tenant-scoped: require tenant context (/v1/:tenant_id/mymodule/...)
//
// Route types:
//   - sdk.RouteClient  — client-facing API (standard auth middleware)
//   - sdk.RoutePlatform — platform admin API (platform-level auth)
func (m *Module) RegisterRoutes(router *sdk.Router) []sdk.RouteHandler {
	return []sdk.RouteHandler{
		{
			Type: sdk.RouteClient, Register: m.registerClientRoutes,
		},
		// Uncomment to add platform admin routes:
		// {
		// 	Type: sdk.RoutePlatform, Register: m.registerPlatformRoutes,
		// },
	}
}

func (m *Module) registerClientRoutes(router *sdk.Router) {
	// ── Global routes (not tenant-scoped) ─────────────────────────────────
	// These do not require a tenant context.
	// Permission "self" means any authenticated user can access.
	// Permission "public" means no auth required.

	// ── Tenant-scoped routes ──────────────────────────────────────────────
	// router.Tenant() returns a sub-router that automatically scopes
	// routes under /v1/:tenant_id/mymodule/...
	// The tenant_id is extracted by the kernel middleware and available
	// via the tenantID(c) helper.
	t := router.Tenant()
	t.GET("/items", "mymodule.items.read", m.handleListItems)
	t.POST("/items", "mymodule.items.manage", m.handleCreateItem)
	t.GET("/items/:id", "mymodule.items.read", m.handleGetItem)
	t.PATCH("/items/:id", "mymodule.items.manage", m.handleUpdateItem)
	t.DELETE("/items/:id", "mymodule.items.manage", m.handleDeleteItem)
}
