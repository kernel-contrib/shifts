package mymodule

import (
	"io/fs"

	"go.edgescale.dev/kernel-contrib/mymodule/internal"
	"go.edgescale.dev/kernel-contrib/mymodule/migrations"
	"go.edgescale.dev/kernel/sdk"
)

// Module is the main entry point for the mymodule kernel module.
// TODO: Replace "mymodule" with your module name throughout this file.
type Module struct {
	ctx  sdk.Context
	repo *internal.Repository
	svc  *internal.Service
}

// New constructs the module.
func New() *Module {
	return &Module{}
}

// Manifest returns immutable metadata for this module.
// The kernel reads this at startup to register routes, permissions, events, etc.
func (m *Module) Manifest() sdk.Manifest {
	return sdk.Manifest{
		// ID is the unique identifier for this module. Must be lowercase, no spaces.
		// Other modules reference this when calling sdk.Reader[YourReader](&ctx, "mymodule").
		ID: "mymodule",

		// Type declares the module's role. Options:
		//   sdk.TypeCore     — core infrastructure module (IAM, billing, etc.)
		//   sdk.TypeFeature  — feature module (most modules use this)
		Type: sdk.TypeFeature,

		// Schema is the prefix for database tables. The kernel's migration runner uses
		// this to namespace migrations. Convention: "module_<id>"
		Schema: "module_mymodule",

		// Display metadata for admin panel.
		Name:        "My Module",
		Description: "A brief description of what this module does",
		Version:     "0.1.0",

		// Permissions define granular access keys for RBAC.
		// Convention: "<module_id>.<resource>.<action>"
		Permissions: []sdk.Permission{
			{Key: "mymodule.items.read", Label: sdk.T("View items", "ar", "عرض العناصر")},
			{Key: "mymodule.items.manage", Label: sdk.T("Create, update, and delete items", "ar", "إنشاء وتعديل وحذف العناصر")},
		},

		// PublicEvents are events this module publishes to the outbox.
		// Other modules can subscribe to these via hooks or event listeners.
		PublicEvents: []sdk.EventDef{
			{Subject: "mymodule.item.created", Description: sdk.T("A new item was created")},
			{Subject: "mymodule.item.updated", Description: sdk.T("An item was updated")},
			{Subject: "mymodule.item.deleted", Description: sdk.T("An item was deleted")},
		},

		// Config defines user-configurable settings for this module.
		// These appear in the admin panel and are read via ctx.Config.Get().
		Config: []sdk.ConfigFieldDef{
			{
				Key:     "mymodule.max_items",
				Type:    "number",
				Default: "100",
				Label:   sdk.T("Maximum items per tenant", "ar", "الحد الأقصى للعناصر لكل مستأجر"),
				Description: sdk.T(
					"The maximum number of items allowed per tenant.",
					"ar", "الحد الأقصى لعدد العناصر المسموح بها لكل مستأجر.",
				),
			},
		},

		// UINav defines sidebar navigation items for the admin panel.
		UINav: []sdk.NavItem{
			{Label: sdk.T("Items", "ar", "العناصر"), Icon: "list", Path: "/mymodule/items", Permission: "mymodule.items.read", SortOrder: 1},
		},
	}
}

// Migrations returns the embedded SQL migration files.
// The kernel reads these at startup and applies them in order.
func (m *Module) Migrations() fs.FS {
	return migrations.FS
}

// Init wires the module's internal services. Called once at startup by the kernel.
// Use this to initialize repositories, services, and register readers.
func (m *Module) Init(ctx sdk.Context) error {
	m.ctx = ctx
	m.repo = internal.NewRepository(ctx.DB)

	m.svc = internal.NewService(m.repo, ctx.Bus, ctx.Redis, ctx.Logger)

	// Register a reader so other modules can consume your data.
	// Other modules resolve it via: sdk.Reader[mymodule.MyModuleReader](&ctx, "mymodule")
	ctx.RegisterReader(&moduleReader{
		repo: m.repo,
	})

	ctx.Logger.Info("mymodule initialized")
	return nil
}

// Shutdown performs cleanup before the kernel stops.
// Close connections, flush buffers, etc.
func (m *Module) Shutdown() error {
	return nil
}
