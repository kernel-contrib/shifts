---
name: kernel-module-development

description: >
  Guide an AI agent to generate correct, production-quality modules for the
  EdgeScale Kernel framework (SDK v0.0.1+). Covers routing, multi-tenancy,
  lifecycle, and all SDK conventions.

metadata:
    model: Claude-opus-4.6
    last_modified: Sun, 13 Apr 2026 19:19:00 GMT+3
---

# 1. What Is a Module?

A module is a self-contained Go package that implements `sdk.Module` (4 methods).
It gets its own PostgreSQL schema, namespaced Redis prefix, and plugs into the kernel lifecycle without modifying kernel code.

---

## 2. Mandatory Interface — `sdk.Module`

Every module **must** implement exactly these 4 methods:

```go
type Module interface {
    Manifest() Manifest       // Immutable metadata — called once at registration
    Migrations() fs.FS        // Embedded SQL files (return nil if none)
    Init(ctx Context) error   // Boot-time wiring (repos, services, readers)
    Shutdown() error          // Graceful cleanup (reverse dependency order)
}
```

### Optional Capability Interfaces

Only implement what the module needs — the kernel discovers them via type assertion:

| Interface | Method | When to use |
| --- | --- | --- |
| `sdk.HttpModule` | `RouteHandlers() []RouteHandler` | Module exposes REST endpoints |
| `sdk.EventModule` | `RegisterEvents(bus EventBus)` | Module subscribes to async events |
| `sdk.HookModule` | `RegisterHooks(hooks *HookRegistry)` | Module intercepts other modules' operations synchronously |
| `sdk.WorkflowModule` | `RegisterWorkflows(reg WorkflowRegistry)`, `RegisterActivities(reg ActivityRegistry)` | Temporal workflows (experimental) |

---

## 3. Project Structure

Generate modules following this layout:

```bash
{module-name}/
├── go.mod                      # go.edgescale.dev/kernel-contrib/{name}
├── module.go                   # Module struct + Manifest + Init + Shutdown
├── migrations/
│   ├── embed.go                # //go:embed *.sql
│   ├── 001_{description}.up.sql
│   └── 001_{description}.down.sql
├── models.go                   # GORM models
├── repository.go               # Database access layer
├── service.go                  # Business logic
├── handlers.go                 # HTTP handlers (only if HttpModule)
├── events.go                   # Event subscriptions & publishers (only if EventModule)
├── reader.go                   # Cross-module reader interface + implementation
└── module_test.go              # Tests
```

- **Headless modules** (no routes): omit `handlers.go`, do NOT implement `sdk.HttpModule`.
- If import cycles arise from reader types being consumed by other files in the same package, extract a `types/` sub-package.

---

## 4. Manifest Configuration

```go
func (m *Module) Manifest() sdk.Manifest {
    return sdk.Manifest{
        // ── Required ─────────────────────────────
        ID:      "{module_id}",              // lowercase slug, used in URLs/DB/Redis
        Name:    "{Human Readable Name}",
        Version: "1.0.0",                    // semver
        Type:    sdk.TypeFeature,            // TypeCore | TypeFeature | TypeIntegration
        Schema:  "module_{module_id}",       // PostgreSQL schema name

        // ── Recommended ──────────────────────────
        Description: "...",
        DependsOn:   []string{"iam"},        // module IDs this depends on

        // ── Permissions (one per secured route) ──
        Permissions: []sdk.Permission{
            {Key: "{module}.{entities}.{action}", Label: sdk.T("English label", "ar", "Arabic label")},
        },

        // ── Public events (webhook-eligible) ─────
        PublicEvents: []sdk.EventDef{
            {Subject: "{module}.{entity}.{past_verb}", Description: sdk.T("..."), PayloadExample: `{...}`},
        },

        // ── Per-tenant config fields ─────────────
        Config: []sdk.ConfigFieldDef{
            {Key: "...", Type: "bool|text|number|select|...", Default: ..., Label: sdk.T("...")},
        },

        // ── UI sidebar navigation ────────────────
        UINav: []sdk.NavItem{
            {Label: sdk.T("..."), Icon: "...", Path: "/...", Permission: "...", SortOrder: 1},
        },

        // ── Optional ────────────────────────────
        StoragePrefix:       "{module}",
        CustomFieldEntities: []string{"entity_name"},
        Tags:                []string{"premium"},
    }
}
```

### Module Types

| Type | Constant | Behavior |
| --- | --- | --- |
| Core | `sdk.TypeCore` | Always active. No activation check. (e.g., IAM, uploads) |
| Feature | `sdk.TypeFeature` | Enabled/disabled per tenant via `module_activations`. |
| Integration | `sdk.TypeIntegration` | Installed on demand. Third-party connectors. |
| Admin | `sdk.TypeAdmin` | Platform-level modules, mounted on `/admin/v1/` without tenant scoping. |

> **Note:** If you just need admin endpoints alongside client routes, use `RouteHandler{Type: sdk.RouteAdmin}` — `TypeAdmin` is only for modules that are **exclusively** platform-level. Any module can expose both client and admin routes.

### Naming Conventions

| Item | Pattern | Example |
| --- | --- | --- |
| Module ID | `lowercase_slug` | `billing`, `hr_payroll` |
| Schema | `module_{id}` | `module_billing` |
| Permission key | `{module}.{entities_plural}.{action}` | `billing.invoices.create` |
| Event subject | `{module}.{entity_singular}.{past_verb}` | `billing.invoice.created` |
| Hook point | `{lifecycle}.{module}.{action}` | `before.billing.invoice.create` |
| Redis key | descriptive, colon-separated | `inv:{id}` |
| Migration file | `{NNN}_{description}.{up\|down}.sql` | `001_create_invoices.up.sql` |

---

## 5. Database Migrations

### Embedding

Create `migrations/embed.go`:

```go
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
```

Wire it in `module.go`:

```go
func (m *Module) Migrations() fs.FS { return migrations.FS }
```

### SQL Rules

- **File naming:** `{NNN}_{description}.up.sql` / `.down.sql` — version is a zero-padded integer.
- `.up.sql` — required. `.down.sql` — strongly recommended.
- The kernel automatically sets `search_path` to the module's schema — write SQL as if tables are in the default schema.
- **Always include `tenant_id`** (type `UUID NOT NULL`) and index it — every query must be tenant-scoped.
- Use `BIGSERIAL PRIMARY KEY` for auto-incrementing IDs.
- Use `TIMESTAMPTZ NOT NULL DEFAULT NOW()` for timestamp columns.
- Include `deleted_at TIMESTAMPTZ` for soft-delete support.
- Use `CREATE INDEX` for frequently queried columns; use partial indexes (`WHERE deleted_at IS NULL`) where appropriate.

### Example

```sql
-- 001_create_invoices.up.sql
CREATE TABLE invoices (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   UUID NOT NULL,
    number      TEXT NOT NULL,
    total       BIGINT NOT NULL DEFAULT 0,
    status      TEXT NOT NULL DEFAULT 'draft',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ
);
CREATE INDEX idx_invoices_tenant_id ON invoices(tenant_id);
CREATE UNIQUE INDEX idx_invoices_tenant_number ON invoices(tenant_id, number) WHERE deleted_at IS NULL;
```

```sql
-- 001_create_invoices.down.sql
DROP INDEX IF EXISTS idx_invoices_tenant_number;
DROP INDEX IF EXISTS idx_invoices_tenant_id;
DROP TABLE IF EXISTS invoices;
```

---

## 6. GORM Models

Use the SDK's composable base structs:

```go
// Full base: ID (uint64) + CreatedAt + UpdatedAt + DeletedAt
type Invoice struct {
    sdk.BaseModel
    TenantID uuid.UUID `json:"tenant_id" gorm:"type:uuid;not null;index"`
    Total    int64     `json:"total"`
}

// Just timestamps (custom PK)
type AuditEntry struct {
    ID uuid.UUID `json:"id" gorm:"primaryKey;type:uuid"`
    sdk.Timestamped
}

// Translatable fields stored as JSONB
type Product struct {
    sdk.BaseModel
    Name     sdk.TranslatableField `json:"name" gorm:"type:jsonb"`
    TenantID uuid.UUID             `json:"tenant_id" gorm:"type:uuid;not null"`
}
```

### TranslatableField

- Type: `map[string]string`, stored as JSONB.
- Create: `sdk.T("English", "ar", "عربي")` or `sdk.Translations("en", "Hello", "ar", "مرحبا")`.
- Read: `field.Get(locale)` — falls back to `"en"`, then first available.
- In models: `gorm:"type:jsonb"`.
- In manifests: used for `Label`, `Description`, `Placeholder`, `Group` on Permission, ConfigFieldDef, ConfigOption, NavItem, EventDef.

### JSONB Column Type

`sdk.JSONB` is a cross-database JSON column type (handles both `[]byte` from Postgres and `string` from SQLite):

```go
type Order struct {
    sdk.BaseModel
    TenantID uuid.UUID `json:"tenant_id" gorm:"type:uuid;not null"`
    Metadata sdk.JSONB `json:"metadata" gorm:"type:jsonb"`
}
```

> Use `JSONB` for arbitrary JSON. Use `TranslatableField` for `map[string]string` translations.

---

## 7. Initialization & Context

The kernel provides `sdk.Context` during `Init()`:

| Field | Type | Purpose |
| --- | --- | --- |
| `DB` | `*gorm.DB` | Scoped to module schema (auto search_path) |
| `PublicDB` | `*gorm.DB` | For JOINs to `public.users`, `public.tenants` |
| `Redis` | `NamespacedRedis` | Keys auto-prefixed `module:{id}:` |
| `Logger` | `*slog.Logger` | Tagged with module ID |
| `Audit` | `AuditLogger` | Hash-chained audit log |
| `Config` | `func(uuid.UUID) map[string]any` | Per-tenant configuration |
| `Bus` | `EventBus` | Fire-and-forget events |
| `Tasks` | `TaskExecutor` | Background task execution |
| `Search` | `SearchEngine` | Full-text search |
| `Hooks` | `*HookRegistry` | Sync hook registration/firing |
| `IdentityProvider` | `IdentityProvider` | Token validation |
| `Outbox` | `OutboxWriter` | Durable transactional events |
| `Operations` | `OperationTracker` | Long-running op tracking |
| `Features` | `FeatureFlags` | Per-tenant feature flags |
| `ServiceID` | `string` | This module's ID |
| `ValidPermissionKey` | `func(string) bool` | Check if permission key is declared |

### Init Pattern

```go
type Module struct {
    ctx     sdk.Context      // sdk.Context — stored at init time
    repo    *Repository
    service *Service
}

func (m *Module) Init(ctx sdk.Context) error {
    m.ctx = ctx
    m.repo = NewRepository(ctx.DB)
    m.service = NewService(m.repo, ctx.Bus, ctx.Audit, ctx.Logger)

    // Expose reader for cross-module access
    ctx.RegisterReader(&myReader{repo: m.repo})

    ctx.Logger.Info("module initialized")
    return nil
}
```

> **Convention:** `m.ctx` = module's `sdk.Context`; bare `ctx` = Go's `context.Context` (per-request).

---

## 8. Routing & Handlers

### Route Registration via RouteHandlers()

Modules declare route handlers via `RouteHandlers()`. Each handler specifies a **route type** and a registration function:

```go
func (m *Module) RouteHandlers() []sdk.RouteHandler {
    return []sdk.RouteHandler{
        {Type: sdk.RouteClient, Register: m.registerClientRoutes},
    }
}
```

### Route Types

| Type | Constant | Groups Created | Middleware |
| --- | --- | --- | --- |
| Client | `sdk.RouteClient` | Global (`/v1/{module}/`) + Tenant (`/v1/:tenant_id/{module}/`) + Public (`/v1/{module}/public/`) | Auth, tenant resolution, user, activation |
| Admin | `sdk.RouteAdmin` | Admin (`/admin/v1/{module}/`) | Auth, platform admin check |

### Router Groups

The `Router` provides two scopes for client routes:

| Method | Scope | URL Pattern |
| --- | --- | --- |
| `r.GET(...)` | Global authenticated | `/v1/{module}/{path}` |
| `r.Tenant().GET(...)` | Tenant-scoped | `/v1/:tenant_id/{module}/{path}` |
| `r.GET(..., sdk.Public, ...)` | Public | `/v1/{module}/public/{path}` |

### Client Route Example

```go
func (m *Module) registerClientRoutes(r *sdk.Router) {
    // Tenant-scoped routes — require tenant context
    t := r.Tenant()
    t.GET("/{entities}",       "{name}.{entities}.read",   m.handleList)
    t.GET("/{entities}/:id",   "{name}.{entities}.read",   m.handleGet)
    t.POST("/{entities}",      "{name}.{entities}.create",  m.handleCreate)
    t.PUT("/{entities}/:id",   "{name}.{entities}.create",  m.handleUpdate)
    t.DELETE("/{entities}/:id", "{name}.{entities}.delete", m.handleDelete)

    // Global authenticated — no tenant context
    r.GET("/me/items", sdk.Self, m.handleMyItems)

    // Public — no auth
    r.GET("/public/catalog", sdk.Public, m.handleCatalog)
}
```

### Admin Route Example

```go
func (m *Module) registerAdminRoutes(r *sdk.Router) {
    r.GET("/users", "platform.users.list", m.handleListAllUsers)
    r.POST("/tenants/:id/activate", "platform.tenants.manage", m.handleActivateTenant)
}
```

### Dual Client+Admin Module

A single module can expose both:

```go
func (m *Module) RouteHandlers() []sdk.RouteHandler {
    return []sdk.RouteHandler{
        {Type: sdk.RouteClient, Register: m.registerClientRoutes},
        {Type: sdk.RouteAdmin, Register: m.registerAdminRoutes},
    }
}
```

### Permission Constants

| Constant | Value | Behavior |
| --- | --- | --- |
| `sdk.Public` | `""` | No auth required |
| `sdk.Self` | `"self"` | Authenticated, handler checks ownership |
| `sdk.ReadOnly` | `"readonly"` | Authenticated, tenant-scoped, no DB tx or RLS |

### API Versioning

The kernel does NOT own API versioning. Version endpoints within your path namespace:

```go
t := r.Tenant()
t.GET("/items", "mod.items.read", m.handleListV1)
t.GET("/v2/items", "mod.items.read", m.handleListV2)
```

### Combining Permissions

```go
t.GET("/dashboard", sdk.RequireAny("mod.items.read", "mod.reports.read"), m.handleDashboard)
```

### Handler Anatomy

```go
func (m *Module) handleCreate(c *gin.Context) {
    // Identity
    userID   := c.GetString("user_id")             // IdP subject (external)
    provider := c.GetString("auth_provider")

    // Tenant (set by resolveTenant middleware on tenant-scoped routes)
    tenantID, _ := c.Get("tenant_id")              // uuid.UUID

    // Internal user (set by resolveUser middleware)
    internalUserID, _ := c.Get("internal_user_id") // uuid.UUID (DB PK)

    // Locale
    locale := sdk.Locale(c)                         // "en", "ar", etc.

    // Request tracking
    requestID := c.GetString("request_id")
}
```

### URL Mapping Summary

| Route Type | Pattern | Example |
| --- | --- | --- |
| Global authenticated | `/v1/{module}/{path}` | `GET /v1/iam/me` |
| Tenant-scoped | `/v1/:tenant_id/{module}/{path}` | `GET /v1/550e8400.../billing/invoices` |
| Public | `/v1/{module}/public/{path}` | `GET /v1/billing/public/pricing` |
| Admin | `/admin/v1/{module}/{path}` | `GET /admin/v1/iam/users` |

---

## 9. Response & Error Handling

### ALWAYS use SDK helpers. NEVER use `c.JSON()` directly

**Success:**

```go
sdk.OK(c, result)                        // 200
sdk.OKWithMessage(c, result, "msg")      // 200 + message
sdk.Created(c, resource)                 // 201
sdk.Accepted(c, gin.H{"op_id": id})      // 202
sdk.NoContent(c)                         // 204

// Paginated list
page := sdk.ParsePageRequest(c)
result, err := sdk.Paginate[Item](m.ctx.DB.Where("tenant_id = ?", tenantID), page)
sdk.List(c, result.Items, result.Meta)
```

**Errors (typed — carries HTTP status automatically):**

```go
sdk.Error(c, sdk.NotFound("item", id))        // 404
sdk.Error(c, sdk.BadRequest("invalid"))        // 400
sdk.Error(c, sdk.Forbidden("not owner"))       // 403
sdk.Error(c, sdk.Conflict("already exists"))   // 409
sdk.Error(c, sdk.Internal("db error"))         // 500
sdk.Error(c, sdk.Unauthorized("expired"))      // 401
sdk.Error(c, sdk.Unprocessable("bad data"))    // 422
sdk.Error(c, sdk.RateLimited("slow down"))     // 429
sdk.Error(c, sdk.Unavailable("try later"))     // 503

// Auto-detect (wraps unknown as 500, logs raw error server-side)
sdk.FromError(c, err)

// Multiple validation errors
sdk.Errs(c, 400, []sdk.APIError{
    {Code: "validation_error", Message: "field 'x' is required"},
})
```

**Request Binding:**

```go
var req CreateRequest
if !sdk.BindAndValidate(c, &req) { return }  // JSON body

var filter ListFilter
if !sdk.BindQuery(c, &filter) { return }     // query params

var params struct { ID uint64 `uri:"id" binding:"required"` }
if !sdk.BindURI(c, &params) { return }       // URI params
```

---

## 10. Events & Async Communication

### Publishing

```go
// Fire-and-forget (in-process, best-effort)
m.ctx.Bus.Publish(c.Request.Context(), "mod.entity.created", payload)

// Durable (transactional outbox — guaranteed delivery)
m.ctx.Outbox.WriteEvent(c.Request.Context(), "mod.entity.created", payload)
```

### Subscribing

```go
func (m *Module) RegisterEvents(bus sdk.EventBus) {
    bus.Subscribe("mymod", "other.entity.created", func(ctx context.Context, env sdk.EventEnvelope) error {
        var payload struct { ID string `json:"id"` }
        json.Unmarshal(env.Payload, &payload)
        // handle event...
        return nil
    })
}
```

### EventEnvelope Fields

`Subject`, `Payload`, `TenantID`, `UserID`, `RequestID`, `TraceID`, `SpanID`, `ServiceID`, `Timestamp`

---

## 11. Hooks & Sync Interception

```go
func (m *Module) RegisterHooks(hooks *sdk.HookRegistry) {
    // Before — can abort
    hooks.Before(sdk.BeforeHookPoint("orders", "create"), func(ctx context.Context, payload any) error {
        if !valid {
            return sdk.Abort(sdk.Forbidden("reason"))
        }
        return nil
    })

    // After — cannot abort
    hooks.After(sdk.AfterHookPoint("kernel", "tenant.provisioned"), func(ctx context.Context, payload any) error {
        event := payload.(sdk.TenantProvisionedEvent)
        // react to tenant provisioning...
        return nil
    })
}
```

### Firing hooks in your module

```go
if err := s.hooks.FireBefore(ctx, "before.mymod.entity.create", payload); err != nil {
    if ae, ok := sdk.IsAbortError(err); ok {
        return ae.Reason
    }
    return err
}
// ... do work ...
s.hooks.FireAfter(ctx, "after.mymod.entity.create", payload)
```

---

## 12. Cross-Module Communication (Readers)

### Define Interface

```go
// reader.go — public contract
type MyModReader interface {
    GetItem(ctx context.Context, tenantID uuid.UUID, id string) (*Item, error)
}
```

### Implement & Register

```go
type myModReader struct { repo *Repository }

func (r *myModReader) GetItem(ctx context.Context, tenantID uuid.UUID, id string) (*Item, error) {
    return r.repo.FindByID(ctx, tenantID, id)
}

// In Init():
ctx.RegisterReader(&myModReader{repo: m.repo})
```

### Consume

```go
reader, err := sdk.Reader[mymod.MyModReader](&m.ctx, "mymod")
if err != nil { /* module not available */ }
item, _ := reader.GetItem(ctx, tenantID, id)
```

> **Important:** Access readers lazily in handlers, NOT during `Init()`. Add the provider module to `DependsOn`.

---

## 13. Caching

```go
// Cache-aside with auto-expiry
item, err := sdk.Cache(ctx, m.ctx.Redis, "item:"+id, 5*time.Minute, func() (*Item, error) {
    return m.repo.FindByID(ctx, id)
})

// Invalidation
sdk.Invalidate(ctx, m.ctx.Redis, "item:"+id)
sdk.InvalidateMany(ctx, m.ctx.Redis, "item:"+id, "list:"+tenantID.String())
sdk.InvalidatePrefix(ctx, m.ctx.Redis, "item:")
```

> Keys auto-namespaced: `"item:123"` → `"module:mymod:item:123"`.

---

## 14. Audit Logging

```go
m.ctx.Audit.Log(c.Request.Context(), sdk.AuditEntry{
    Action:     sdk.AuditCreate,    // AuditCreate|Update|Delete|Restore|Login|Logout|Export|Import|Approve|Reject|Activate|Deactivate
    Resource:   "invoice",
    ResourceID: fmt.Sprintf("%d", invoice.ID),
    Changes: map[string]sdk.AuditChange{
        "total":  {Old: nil, New: invoice.Total},
        "status": {Old: nil, New: "draft"},
    },
})
```

> `user_id`, `tenant_id`, `ip_address`, `request_id` are auto-captured from request context.

---

## 15. Background Tasks & Operations

### Background Task

```go
opID, _ := m.ctx.Tasks.Execute(ctx, sdk.TaskDefinition{
    ID: uuid.New().String(), Name: "export", ServiceID: "mymod",
    TenantID: tenantID, Retries: 3, Timeout: 10 * time.Minute,
    Handler: func(ctx context.Context, progress sdk.ProgressReporter) error {
        // work...
        progress.Report(ctx, 50, "halfway")
        return nil
    },
})
sdk.Accepted(c, gin.H{"operation_id": opID})
```

### Operation Tracking

```go
opID, _ := m.ctx.Operations.Create(ctx, sdk.OperationInput{
    ModuleID: "mymod", TenantID: tenantID, UserID: userID, Type: "import",
})
// In goroutine:
m.ctx.Operations.UpdateProgress(ctx, opID, current, total)
m.ctx.Operations.Complete(ctx, opID, &result)       // or
m.ctx.Operations.Fail(ctx, opID, errMsg)
```

Statuses: `pending` → `running` → `completed|failed|cancelled`

---

## 16. Feature Flags

```go
if m.ctx.Features.Enabled(ctx, "mymod.new_feature", tenantID.String()) {
    // feature-gated logic
}
```

> Returns `false` by default if no feature flag module is registered.

---

## 17. Rate Limiting

```go
func (m *Module) registerClientRoutes(r *sdk.Router) {
    t := r.Tenant()
    t.POST("/send", "mymod.items.create",
        sdk.RateLimit("send_item", 10, time.Minute, m.ctx.Redis.Client()),
        m.handleSend,
    )
}
```

---

## 18. Full-Text Search

```go
// Create index in Init()
m.ctx.Search.CreateIndex(ctx, "mymod_items", sdk.IndexSettings{
    PrimaryKey:           "id",
    SearchableAttributes: []string{"name", "description"},
    FilterableAttributes: []string{"tenant_id", "status"},
    SortableAttributes:   []string{"created_at"},
})

// Index on create/update
m.ctx.Search.Index(ctx, "mymod_items", id, item)

// Search
result, _ := m.ctx.Search.Search(ctx, "mymod_items", sdk.SearchQuery{
    Query:   "keyword",
    Filters: map[string]any{"tenant_id": tenantID.String()},
    Sort:    []string{"created_at:desc"},
    Limit:   25,
})
```

---

## 19. Testing

```go
func TestCreateItem(t *testing.T) {
    ctx := sdk.NewTestContext("mymod")

    repo := NewRepository(ctx.DB)  // use test DB
    svc := NewService(repo, ctx.Bus, ctx.Audit, ctx.Logger)

    item, err := svc.Create(context.Background(), input)
    require.NoError(t, err)

    // Assert events
    events := ctx.Bus.(*sdk.TestBus).Events()
    assert.Len(t, events, 1)
    assert.Equal(t, "mymod.item.created", events[0].Subject)

    // Assert audit
    entries := ctx.Audit.(*sdk.TestAuditLogger).Entries()
    assert.Len(t, entries, 1)
}
```

Test doubles: `*sdk.TestBus`, `*sdk.TestAuditLogger`, `*sdk.TestTaskExecutor`, `*sdk.TestSearchEngine`.

---

## 20. Consumer App Integration

```go
func main() {
    k := kernel.New(kernel.LoadConfig())
    k.SetIdentityProvider(firebase.New(...))
    k.SetEventBus(nats.NewBus(...))
    k.MustRegister(iam.New())
    k.MustRegister(mymod.New())
    k.Execute()
}
```

### CLI Commands

```bash
# Server
go run main.go serve                                     # Start HTTP server

# Migrations
go run main.go migrate                                   # Run migrations
go run main.go migrate status                            # Show applied migrations
go run main.go migrate rollback --module billing          # Rollback last migration
go run main.go migrate rollback --module billing --steps 3 # Rollback last 3

# Module Management
go run main.go module list                               # List modules
go run main.go module enable billing --tenant <id>       # Enable module for tenant
go run main.go module disable billing --tenant <id>      # Disable module for tenant
go run main.go module status --tenant <id>               # Show activation status
go run main.go module deps                               # Show dependency graph

# Tenant Management
go run main.go tenant provision <id>                     # Provision tenant
go run main.go tenant provision <id> --admin-email a@b.c # With admin email
go run main.go tenant deprovision <id> --confirm yes     # Deactivate all modules
go run main.go tenant list                               # List tenants + active counts

# Platform Administration
go run main.go platform grant <user-id>                  # Grant platform_admin role
go run main.go platform grant <user-id> --role super     # Grant custom role
go run main.go platform revoke <user-id>                 # Revoke all platform roles
go run main.go platform list                             # List all platform admins
```

> `platform` commands require `AdminResolver` implementing `sdk.PlatformManager`.
> `k.AddCommand()` lets consumer apps register custom CLI commands.

---

## 21. Critical Rules (Do's & Don'ts)

### DO

- Use `ctx.DB` for all queries — schema isolation is automatic.
- Include `tenant_id` in **every** table — multi-tenancy enforcement.
- Declare **all** permissions in `Manifest()` — kernel validates at boot.
- Use `sdk.OK()`, `sdk.Error()`, `sdk.Created()` etc. — consistent API envelope.
- Use events for cross-module side effects — loose coupling.
- Use readers for cross-module data access — type-safe, no circular imports.
- Use `sdk.BindAndValidate()` for request parsing — consistent error format.
- Use `sdk.Paginate[T]()` for list endpoints — standard pagination.
- Log with `ctx.Logger` — structured, tagged with module ID.
- Return `error` from `Init()` on fatal issues — kernel aborts gracefully.
- Access readers lazily in handlers, not during `Init()`.
- Add provider to `DependsOn` when using its reader.
- Use `r.Tenant()` for tenant-scoped routes, `r.GET()` for global routes.

### DON'T

- Import another module directly — use readers/events instead.
- Use `ctx.PublicDB` for writes — it's read-only (JOINs).
- Access raw Redis — always go through `ctx.Redis`.
- Create routes without permissions — every secure route needs one.
- Use `c.JSON()` directly — breaks the standard envelope.
- Panic in handlers/Init — use error returns.
- Skip `tenant_id` in queries — data leaks across tenants.
- Hardcode schema names in SQL — use kernel's search_path.
- Use `org_id` or `OrgID` — the correct name is `tenant_id` / `TenantID`.
- Use `RegisterRoutes()` — replaced by `RouteHandlers() []RouteHandler`.
- Use `router.V2()` — version endpoints in your path namespace instead.
- Use `VerifyToken` — the method is `ValidateToken` on `IdentityProvider`.

---

## 22. Lifecycle Order Reference

```bash
1. Manifest()            → at Register(), validates ID uniqueness
2. Migrations()          → during `migrate` command, dependency order
3. Init(ctx)             → during Serve(), after migrations, dependency order
4. RegisterEvents()      → immediately after Init()     (if EventModule)
5. RegisterHooks()       → immediately after events      (if HookModule)
6. RegisterWorkflows()   → if WorkflowModule AND Temporal configured
7. RegisterActivities()  → if WorkflowModule AND Temporal configured
8. RouteHandlers()       → after ALL modules initialized  (if HttpModule)
9. Shutdown()            → on SIGINT/SIGTERM, REVERSE dependency order
```

> Steps 4-7 run per-module during `initModules()`. Step 8 runs after **all** modules complete init — readers and events from every module are available before routes are mounted.

---

## 23. Headless Module Checklist

When building a module with no HTTP endpoints:

1. Implement the 4 `sdk.Module` methods.
2. **Do NOT** implement `sdk.HttpModule` — just omit `RouteHandlers()` entirely.
3. Implement `sdk.EventModule` if cleanup subscriptions are needed.
4. No `Permissions` in Manifest — permissions protect routes.
5. Use `sdk.TypeCore` — headless modules are usually shared infrastructure.
6. Register a reader in `Init()` — this is the module's entire public API.
7. The consuming module owns the HTTP surface.

---

## 24. Full Module Skeleton

When asked to generate a new module, produce **all** of these files:

### `module.go`

```go
package {name}

import (
    "io/fs"
    "go.edgescale.dev/kernel/sdk"
    "go.edgescale.dev/kernel-contrib/{name}/migrations"
)

type Module struct {
    ctx     sdk.Context
    repo    *Repository
    service *Service
}

func New() *Module { return &Module{} }

func (m *Module) Manifest() sdk.Manifest {
    return sdk.Manifest{
        ID:          "{name}",
        Name:        "{Display Name}",
        Version:     "1.0.0",
        Type:        sdk.TypeFeature,
        Schema:      "module_{name}",
        Description: "...",
        DependsOn:   []string{"iam"},
        Permissions: []sdk.Permission{
            {Key: "{name}.{entities}.read",   Label: sdk.T("View {entities}")},
            {Key: "{name}.{entities}.create", Label: sdk.T("Create {entities}")},
            {Key: "{name}.{entities}.delete", Label: sdk.T("Delete {entities}")},
        },
    }
}

func (m *Module) Migrations() fs.FS { return migrations.FS }

func (m *Module) Init(ctx sdk.Context) error {
    m.ctx = ctx
    m.repo = NewRepository(ctx.DB)
    m.service = NewService(m.repo, ctx.Bus, ctx.Audit, ctx.Logger)
    ctx.RegisterReader(&{name}Reader{repo: m.repo})
    ctx.Logger.Info("{name} module initialized")
    return nil
}

func (m *Module) RouteHandlers() []sdk.RouteHandler {
    return []sdk.RouteHandler{
        {Type: sdk.RouteClient, Register: m.registerClientRoutes},
    }
}

func (m *Module) registerClientRoutes(r *sdk.Router) {
    t := r.Tenant()
    t.GET("/{entities}",       "{name}.{entities}.read",   m.handleList)
    t.GET("/{entities}/:id",   "{name}.{entities}.read",   m.handleGet)
    t.POST("/{entities}",      "{name}.{entities}.create",  m.handleCreate)
    t.PUT("/{entities}/:id",   "{name}.{entities}.create",  m.handleUpdate)
    t.DELETE("/{entities}/:id", "{name}.{entities}.delete", m.handleDelete)
}

func (m *Module) Shutdown() error { return nil }
```

### `migrations/embed.go`

```go
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
```

### `models.go`

```go
package {name}

import (
    "github.com/google/uuid"
    "go.edgescale.dev/kernel/sdk"
)

type {Entity} struct {
    sdk.BaseModel
    TenantID uuid.UUID `json:"tenant_id" gorm:"type:uuid;not null;index"`
    // domain fields...
}
```

### `repository.go`

```go
package {name}

import (
    "context"
    "github.com/google/uuid"
    "gorm.io/gorm"
)

type Repository struct { db *gorm.DB }

func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }

func (r *Repository) Create(ctx context.Context, entity *{Entity}) error {
    return r.db.WithContext(ctx).Create(entity).Error
}

func (r *Repository) FindByID(ctx context.Context, tenantID uuid.UUID, id uint64) (*{Entity}, error) {
    var entity {Entity}
    err := r.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, id).First(&entity).Error
    return &entity, err
}
```

### `service.go`

```go
package {name}

import (
    "context"
    "log/slog"
    "go.edgescale.dev/kernel/sdk"
)

type Service struct {
    repo   *Repository
    bus    sdk.EventBus
    audit  sdk.AuditLogger
    logger *slog.Logger
}

func NewService(repo *Repository, bus sdk.EventBus, audit sdk.AuditLogger, logger *slog.Logger) *Service {
    return &Service{repo: repo, bus: bus, audit: audit, logger: logger}
}
```

### `handlers.go`

```go
package {name}

import (
    "github.com/gin-gonic/gin"
    "github.com/google/uuid"
    "go.edgescale.dev/kernel/sdk"
)

func (m *Module) handleList(c *gin.Context) {
    tenantID := c.MustGet("tenant_id").(uuid.UUID)
    page := sdk.ParsePageRequest(c)
    result, err := sdk.Paginate[{Entity}](m.ctx.DB.Where("tenant_id = ?", tenantID), page)
    if err != nil {
        sdk.FromError(c, err)
        return
    }
    sdk.List(c, result.Items, result.Meta)
}

func (m *Module) handleGet(c *gin.Context) {
    tenantID := c.MustGet("tenant_id").(uuid.UUID)
    var params struct { ID uint64 `uri:"id" binding:"required"` }
    if !sdk.BindURI(c, &params) { return }

    entity, err := m.repo.FindByID(c.Request.Context(), tenantID, params.ID)
    if err != nil {
        sdk.FromError(c, err)
        return
    }
    sdk.OK(c, entity)
}

func (m *Module) handleCreate(c *gin.Context) {
    tenantID := c.MustGet("tenant_id").(uuid.UUID)
    var req CreateRequest
    if !sdk.BindAndValidate(c, &req) { return }
    // ... create logic ...
    sdk.Created(c, entity)
}
```

### `reader.go`

```go
package {name}

import (
    "context"
    "github.com/google/uuid"
)

type {Name}Reader interface {
    Get{Entity}(ctx context.Context, tenantID uuid.UUID, id uint64) (*{Entity}, error)
}

type {name}Reader struct { repo *Repository }

func (r *{name}Reader) Get{Entity}(ctx context.Context, tenantID uuid.UUID, id uint64) (*{Entity}, error) {
    return r.repo.FindByID(ctx, tenantID, id)
}
```
