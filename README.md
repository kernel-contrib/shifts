# Kernel Module Template

A production-ready template for building [EdgeScale Kernel](https://go.edgescale.dev/kernel) modules. This template provides the complete scaffolding so you can focus on your domain logic instead of boilerplate.

## Quick Start

### 1. Create your module from this template

```bash
# Clone the template
git clone https://github.com/kernel-contrib/module-template.git mymodule
cd mymodule
rm -rf .git && git init

# Run the init script to rename everything
./init.sh mymodule "My Module" "go.edgescale.dev/kernel-contrib/mymodule"
```

The init script will:

- Replace all `mymodule` / `myModule` / `MyModule` references with your module name
- Update the Go module path
- Update the `go.mod` file
- Self-delete after completion

### 2. Install dependencies

```bash
go mod tidy
```

### 3. Run tests

```bash
go test -v ./...
```

### 4. Start building

Edit the files to add your domain logic:

| File | Purpose |
| --- | --- |
| `module.go` | Module lifecycle (Manifest, Init, Shutdown) |
| `types/types.go` | GORM domain models (shared) |
| `internal/repository.go` | Data access layer |
| `internal/service.go` | Business logic, validation, events |
| `handlers.go` | HTTP handlers (thin controllers) |
| `routes.go` | Route registration |
| `reader.go` | Cross-module read-only API |
| `hooks.go` | Kernel lifecycle hooks |
| `helpers.go` | Shared utilities |
| `module_test.go` | Unit & integration tests |
| `migrations/` | SQL migration files |

## Architecture

```bash
┌──────────────────────────────────────────────────────┐
│                    Kernel (SDK)                      │
│  ┌─────────┐  ┌──────────┐  ┌────────┐  ┌────────┐   │
│  │ Router  │  │ EventBus │  │ Hooks  │  │ Redis  │   │
│  └────┬────┘  └────┬─────┘  └────┬───┘  └────┬───┘   │
│       │            │             │            │      │
├───────┼────────────┼─────────────┼────────────┼──────┤
│       ▼            ▼             ▼            ▼      │
│  ┌─────────────────────────────────────────────────┐ │
│  │                Your Module                      │ │
│  │                                                 │ │
│  │  handlers.go → internal/service → internal/repo │ │
│  │      │            │              │              │ │
│  │      │            ├─ events ─────┘              │ │
│  │      │            ├─ validation                 │ │
│  │      │            └─ business rules             │ │
│  │      │                                          │ │
│  │  reader.go  ← cross-module queries              │ │
│  │  hooks.go   ← lifecycle hooks                   │ │
│  │  types/types.go ← GORM domain models            │ │
│  └─────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────┘
```

### Layer Responsibilities

| Layer | Rules |
| --- | --- |
| **Handlers** | Parse HTTP requests, delegate to service, return responses. No business logic. |
| **Service** | Validate input, enforce business rules, publish events, call repository. |
| **Repository** | Pure data access via GORM. No business logic. No event publishing. |
| **Reader** | Read-only cross-module API. No writes. No events. Back with cache if needed. |
| **Hooks** | Subscribe to kernel lifecycle events. React to provisioning, guard deletions. |

## Module Interface

Your module must implement these kernel interfaces:

```go
// Required (all modules)
Manifest() sdk.Manifest           // Module metadata
Migrations() fs.FS                // SQL migration files
Init(ctx sdk.Context) error       // Wire dependencies
Shutdown() error                  // Cleanup

// Optional (implement as needed)
RegisterRoutes(router *sdk.Router) []sdk.RouteHandler  // HTTP endpoints
RegisterHooks(hooks *sdk.HookRegistry)                 // Lifecycle hooks
```

## Patterns

### Permissions

Convention: `<module_id>.<resource>.<action>`

```go
{Key: "mymodule.items.read", Label: sdk.T("View items")}
{Key: "mymodule.items.manage", Label: sdk.T("Manage items")}
```

### Events

Convention: `<module_id>.<resource>.<past_tense_verb>`

```go
s.bus.Publish(ctx, "mymodule.item.created", map[string]any{
    "item_id":   item.ID,
    "tenant_id": item.TenantID,
})
```

### SDK Error Helpers

```go
sdk.NotFound("item", id)                    // 404
sdk.BadRequest("name is required")          // 400
sdk.Conflict("item already exists")         // 409
sdk.Forbidden("insufficient permissions")   // 403
```

### SDK Response Helpers

```go
sdk.OK(c, data)          // 200
sdk.Created(c, data)     // 201
sdk.NoContent(c)         // 204
sdk.FromError(c, err)    // Maps sdk errors to HTTP status codes
```

### Pagination

```go
// In handler:
page := sdk.ParsePageRequest(c)

// In repository:
return sdk.Paginate[Item](
    r.db.WithContext(ctx).Model(&Item{}).Where("tenant_id = ?", tenantID),
    page,
)

// In handler response:
sdk.List(c, result.Items, result.Meta)
```

### Cross-Module Reader

```go
// In another module's handler:
reader, err := sdk.Reader[mymodule.MyModuleReader](&m.ctx, "mymodule")
item, err := reader.GetItem(ctx, itemID)
```

### Translatable Fields

```go
// English only:
sdk.T("View items")

// With Arabic translation:
sdk.T("View items", "ar", "عرض العناصر")
```

### Audit Logging

```go
m.ctx.Audit.Log(c.Request.Context(), sdk.AuditEntry{
    Action:     sdk.AuditCreate,
    Resource:   "item",
    ResourceID: item.ID.String(),
})
```

## Migrations

SQL migrations live in `migrations/` and follow this naming convention:

```bash
XXX_description.up.sql    # Forward migration
XXX_description.down.sql  # Rollback migration
```

- Use PostgreSQL-native types: `UUID`, `JSONB`, `TIMESTAMPTZ`
- Always include `created_at`, `updated_at`, `deleted_at` columns
- Create partial indexes with `WHERE deleted_at IS NULL` for soft-delete support
- The kernel runs migrations in order at startup

## Testing

Tests use an in-memory SQLite database with a test harness:

```go
func TestMyFeature(t *testing.T) {
    h := newTestHarness(t)
    ctx := context.Background()

    item, err := h.svc.Create(ctx, mymodule.CreateItemInput{
        TenantID: uuid.New(),
        Name:     "Test Item",
    })
    require.NoError(t, err)
    assert.Equal(t, "Test Item", item.Name)

    // Verify event was published.
    events := h.bus().Events()
    require.Len(t, events, 1)
    assert.Equal(t, "mymodule.item.created", events[0].Subject)
}
```

**Key testing patterns:**

- Use `sdk.NewTestContext("mymodule")` for test SDK context
- Use SQLite DDL (TEXT instead of UUID, BLOB instead of JSONB) in test setup
- Models need `BeforeCreate` hooks for UUID generation (SQLite doesn't have `gen_random_uuid()`)
- Use `sdk.TestBus` to verify published events
- Always test tenant isolation

## Requirements

- Go 1.26+
- EdgeScale Kernel SDK v0.1.2+
