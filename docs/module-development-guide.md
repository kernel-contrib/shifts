# Module Development Guide

> Build feature modules for the EdgeScale Kernel framework.
>
> Compatible with **kernel SDK v0.0.1** · Last updated: 2026-04-13

This guide walks you through everything you need to build, test, and ship a kernel module. By the end, you'll have a production-ready module that plugs into the kernel's lifecycle with zero modifications to the kernel itself.

---

## Table of Contents

1. [Architecture Overview](#architecture-overview)
2. [Quick Start](#quick-start)
3. [Headless Modules (No Routes)](#headless-modules-no-routes)
4. [Project Structure](#project-structure)
5. [The Module Interface](#the-module-interface)
6. [Manifest Configuration](#manifest-configuration)
7. [Database Migrations](#database-migrations)
8. [Initialization & Context](#initialization--context)
9. [Routing & Handlers](#routing--handlers)
10. [Response & Error Handling](#response--error-handling)
11. [Events & Async Communication](#events--async-communication)
12. [Hooks & Sync Interception](#hooks--sync-interception)
13. [Cross-Module Communication (Readers)](#cross-module-communication-readers)
14. [Background Tasks](#background-tasks)
15. [Operations (Long-Running)](#operations-long-running)
16. [Feature Flags](#feature-flags)
17. [Transactional Outbox](#transactional-outbox)
18. [Caching](#caching)
19. [Audit Logging](#audit-logging)
20. [Full-Text Search](#full-text-search)
21. [Translatable Fields](#translatable-fields)
22. [GORM Model Helpers](#gorm-model-helpers)
23. [Rate Limiting](#rate-limiting)
24. [Idempotency](#idempotency)
25. [Kernel API Endpoints](#kernel-api-endpoints)
26. [Resolver Interfaces](#resolver-interfaces)
27. [Testing](#testing)
28. [Integration into a Consumer App](#integration-into-a-consumer-app)
29. [Best Practices & Conventions](#best-practices--conventions)

---

## Architecture Overview

```bash
┌─────────────────────────────────────────────────────┐
│                  Consumer Application               │
│   main.go → kernel.New() → Register() → Execute()   │
└───────────────────────┬─────────────────────────────┘
                        │
┌───────────────────────▼──────────────────────────────┐
│                      Kernel                          │
│  Config · Boot · Migrate · Serve · Shutdown          │
│  ┌────────────────────────────────────────────────┐  │
│  │ Middleware Chain                               │  │
│  │ RequestID → Locale → Auth → ResolveTenant →    │  │
│  │ ResolveUser → ModuleActivation → Permission    │  │
│  └────────────────────────────────────────────────┘  │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐              │
│  │ EventBus │ │  Hooks   │ │ Readers  │              │
│  └──────────┘ └──────────┘ └──────────┘              │
└───────────────────────┬──────────────────────────────┘
                        │
        ┌───────────────┼───────────────┐
        ▼               ▼               ▼
┌──────────────┐ ┌──────────────┐ ┌──────────────┐
│  IAM Module  │ │ Billing Mod. │ │  Your Module │
│  module_iam  │ │module_billing│ │ module_xxx   │
└──────────────┘ └──────────────┘ └──────────────┘
```

**Key concepts:**

- A **module** is a self-contained unit that implements `sdk.Module`
- Each module gets its own **PostgreSQL schema** (e.g., `module_billing`)
- Each module gets a **namespaced Redis** prefix (e.g., `module:billing:`)
- The kernel handles **authentication, authorization, database connections, migrations, and lifecycle** — your module focuses on business logic
- Modules communicate via **events** (async), **hooks** (sync), and **readers** (cross-module queries)

---

## Quick Start

Here's the smallest possible module — only 4 methods are required:

```go
package notes

import (
    "io/fs"

    "github.com/edgescaleDev/kernel/sdk"
)

type Module struct{}

func New() *Module { return &Module{} }

func (m *Module) Manifest() sdk.Manifest {
    return sdk.Manifest{
        ID:          "notes",
        Name:        "Notes",
        Version:     "0.1.0",
        Type:        sdk.TypeFeature,
        Schema:      "module_notes",
        Description: "Simple note-taking module.",
    }
}

func (m *Module) Migrations() fs.FS          { return nil }
func (m *Module) Init(ctx sdk.Context) error { return nil }
func (m *Module) Shutdown() error            { return nil }
```

> **Note:** You do NOT need to implement `RouteHandlers`, `RegisterEvents`, `RegisterHooks`, or workflow methods. These are **optional capability interfaces** — only implement the ones your module needs. See [The Module Interface](#the-module-interface) for details.

Register it in your consumer app:

```go
k := kernel.New(kernel.LoadConfig())
k.MustRegister(notes.New())
k.Execute()
```

That's it — the kernel discovers your module, creates its schema, mounts its routes, and manages its lifecycle.

---

## Headless Modules (No Routes)

A module doesn't have to expose HTTP endpoints. It can be **pure business logic** — owning its own database tables, providing a reader interface for other modules, and participating in the event/hook lifecycle. Simply don't implement `sdk.HttpModule` (i.e., don't provide `RouteHandlers()`) — no empty stubs needed.

This pattern is ideal for **shared domain capabilities** that multiple modules consume but that don't need their own API surface. Examples:

- **Notes** — any module can attach notes to its entities
- **Attachments** — centralized file metadata without its own endpoints
- **Tags / Labels** — reusable tagging system
- **Approval Workflows** — shared approval state machine

### Full Example: Notes Module

Let's build a `notes` module that stores notes against any entity from any module. Other modules (e.g., `billing`, `hr`) use the `NotesReader` interface to add/retrieve notes on their own entities.

#### Project Structure

```bash
module-notes/
├── go.mod
├── module.go           # Module struct, Manifest, Init, lifecycle stubs
├── migrations/
│   ├── embed.go
│   └── 001_create_notes.down.sql
│   └── 001_create_notes.up.sql
├── models.go           # GORM model
├── repository.go       # DB access layer
├── reader.go           # Public reader interface + implementation
└── module_test.go
```

> No `handlers.go` — this module has no routes.

#### Migration

```sql
-- migrations/001_create_notes.up.sql
CREATE TABLE notes (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   UUID        NOT NULL,
    module_id   TEXT        NOT NULL,   -- which module owns the entity (e.g., "billing")
    entity_type TEXT        NOT NULL,   -- entity kind (e.g., "invoice")
    entity_id   TEXT        NOT NULL,   -- entity PK (as string for flexibility)
    body        TEXT        NOT NULL,
    author_id   UUID        NOT NULL,   -- internal user UUID
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ
);

CREATE INDEX idx_notes_entity ON notes(tenant_id, module_id, entity_type, entity_id)
    WHERE deleted_at IS NULL;
```

The `tenant_id + module_id + entity_type + entity_id` composite lets any module attach notes to any of its entities within a tenant.

#### Model

```go
// models.go
package notes

import (
    "github.com/google/uuid"
    "github.com/edgescaleDev/kernel/sdk"
)

type Note struct {
    sdk.BaseModel
    TenantID   uuid.UUID `json:"tenant_id"   gorm:"type:uuid;not null"`
    ModuleID   string    `json:"module_id"   gorm:"not null"`
    EntityType string    `json:"entity_type" gorm:"not null"`
    EntityID   string    `json:"entity_id"   gorm:"not null"`
    Body       string    `json:"body"        gorm:"not null"`
    AuthorID   uuid.UUID `json:"author_id"   gorm:"type:uuid;not null"`
}
```

#### Repository

```go
// repository.go
package notes

import (
    "context"

    "github.com/google/uuid"
    "gorm.io/gorm"
)

type Repository struct {
    db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
    return &Repository{db: db}
}

func (r *Repository) Create(ctx context.Context, note *Note) error {
    return r.db.WithContext(ctx).Create(note).Error
}

func (r *Repository) ListForEntity(ctx context.Context, tenantID uuid.UUID, moduleID, entityType, entityID string) ([]Note, error) {
    var notes []Note
    err := r.db.WithContext(ctx).
        Where("tenant_id = ? AND module_id = ? AND entity_type = ? AND entity_id = ?",
            tenantID, moduleID, entityType, entityID).
        Order("created_at DESC").
        Find(&notes).Error
    return notes, err
}

func (r *Repository) Delete(ctx context.Context, tenantID uuid.UUID, noteID uint64) error {
    return r.db.WithContext(ctx).
        Where("tenant_id = ? AND id = ?", tenantID, noteID).
        Delete(&Note{}).Error
}
```

#### Reader Interface

This is the public contract other modules depend on:

```go
// reader.go
package notes

import (
    "context"

    "github.com/google/uuid"
)

// NotesReader is the cross-module interface for the notes module.
// Other modules depend on this interface — not the implementation.
type NotesReader interface {
    AddNote(ctx context.Context, tenantID uuid.UUID, moduleID, entityType, entityID string, body string, authorID uuid.UUID) error
    GetNotes(ctx context.Context, tenantID uuid.UUID, moduleID, entityType, entityID string) ([]Note, error)
    DeleteNote(ctx context.Context, tenantID uuid.UUID, noteID uint64) error
}

// notesReader implements NotesReader using the repository.
type notesReader struct {
    repo *Repository
}

func (r *notesReader) AddNote(ctx context.Context, tenantID uuid.UUID, moduleID, entityType, entityID string, body string, authorID uuid.UUID) error {
    return r.repo.Create(ctx, &Note{
        TenantID:   tenantID,
        ModuleID:   moduleID,
        EntityType: entityType,
        EntityID:   entityID,
        Body:       body,
        AuthorID:   authorID,
    })
}

func (r *notesReader) GetNotes(ctx context.Context, tenantID uuid.UUID, moduleID, entityType, entityID string) ([]Note, error) {
    return r.repo.ListForEntity(ctx, tenantID, moduleID, entityType, entityID)
}

func (r *notesReader) DeleteNote(ctx context.Context, tenantID uuid.UUID, noteID uint64) error {
    return r.repo.Delete(ctx, tenantID, noteID)
}
```

#### Module (Wiring It All Together)

```go
// module.go
package notes

import (
    "context"
    "encoding/json"
    "fmt"
    "io/fs"

    "github.com/edgescaleDev/kernel/sdk"
    "go.edgescale.dev/module-notes/migrations"
)

type Module struct {
    ctx  sdk.Context
    repo *Repository
}

func New() *Module { return &Module{} }

func (m *Module) Manifest() sdk.Manifest {
    return sdk.Manifest{
        ID:          "notes",
        Name:        "Notes",
        Version:     "1.0.0",
        Type:        sdk.TypeCore,       // always available, no activation needed
        Schema:      "module_notes",
        Description: "Shared note-taking capability for all modules.",
        // No Permissions — this module has no routes to protect.
        // No UINav     — this module has no frontend pages.
        // No Config    — no per-tenant settings needed.
    }
}

func (m *Module) Migrations() fs.FS { return migrations.FS }

func (m *Module) Init(ctx sdk.Context) error {
    m.ctx = ctx
    m.repo = NewRepository(ctx.DB)

    // This is the key line: expose the reader for other modules
    ctx.RegisterReader(&notesReader{repo: m.repo})

    ctx.Logger.Info("notes module initialized (headless — no routes)")
    return nil
}

// No routes — this module is consumed via the reader interface.
// (Don't implement sdk.HttpModule — just omit RouteHandlers entirely)

// Optionally subscribe to events (e.g., clean up notes when an entity is deleted)
func (m *Module) RegisterEvents(bus sdk.EventBus) {
    // Example: auto-delete notes when an invoice is deleted
    bus.Subscribe("notes", "billing.invoice.deleted", func(ctx context.Context, env sdk.EventEnvelope) error {
        var payload struct {
            InvoiceID string `json:"invoice_id"`
        }
        if err := json.Unmarshal(env.Payload, &payload); err != nil {
            return fmt.Errorf("unmarshal invoice deleted event: %w", err)
        }
        // Clean up associated notes...
        return nil
    })
}

func (m *Module) Shutdown() error { return nil }
```

#### Consumer Module Usage

The billing module adds notes to invoices through the reader:

```go
// Inside the billing module's handler:
func (m *BillingModule) handleAddInvoiceNote(c *gin.Context) {
    tenantID, _ := c.Get("tenant_id")  // uuid.UUID — set by kernel middleware
    userID, _ := c.Get("internal_user_id") // uuid.UUID — set by kernel middleware
    invoiceID := c.Param("id")

    var req struct {
        Body string `json:"body" binding:"required"`
    }
    if !sdk.BindAndValidate(c, &req) {
        return
    }

    // Get the notes reader — defined in the notes module
    reader, err := sdk.Reader[notes.NotesReader](&m.ctx, "notes")
    if err != nil {
        sdk.Error(c, sdk.Internal("notes module not available"))
        return
    }

    err = reader.AddNote(
        c.Request.Context(),
        tenantID.(uuid.UUID),
        "billing",           // module_id — identifies us
        "invoice",           // entity_type
        invoiceID,           // entity_id
        req.Body,
        userID.(uuid.UUID),
    )
    if err != nil {
        sdk.FromError(c, err)
        return
    }

    sdk.Created(c, gin.H{"message": "note added"})
}
```

The billing module's routes surface notes through its own API — the notes module itself stays headless.

### When to Use Headless Modules

| Use Case | Why Headless |
| --- | --- |
| **Shared domain** (notes, tags, attachments) | Multiple modules consume it; no single module "owns" the API |
| **Infrastructure services** (audit, notifications) | Consumed internally, no user-facing endpoints |
| **Data enrichment** (currency conversion, geo-lookup) | Pure computation, other modules call the reader |
| **Cross-cutting concerns** (approval workflows) | State machine shared by orders, invoices, POs, etc. |

### Key Rules for Headless Modules

1. **Implement the 4 `sdk.Module` methods** — `Manifest()`, `Migrations()`, `Init()`, `Shutdown()`.
2. **Don't implement `sdk.HttpModule`** — just omit `RouteHandlers()` entirely. No empty stubs needed.
3. **Implement `sdk.EventModule` if needed** — subscribe to cleanup events (e.g., entity deletions).
4. **No `Permissions` needed in the Manifest** — permissions protect routes, and you have none.
5. **`TypeCore` is usually the right choice** — headless modules are shared infrastructure, not tenant-activated features.
6. **Register a reader in `Init()`** — this is your module's entire public API.
7. **The consuming module owns the HTTP surface** — it wraps your reader calls in its own routes and permissions.

---

## Project Structure

We recommend the following layout for a module:

```bash
billing/
├── go.mod                      # go.edgescale.dev/kernel-contrib/billing
├── module.go                   # Module struct + Manifest + interface methods
├── migrations/
│   ├── embed.go                # //go:embed directive
│   ├── 001_create_invoices.down.sql
│   ├── 001_create_invoices.up.sql
│   └── 002_add_payment_status.down.sql
│   └── 002_add_payment_status.up.sql
├── models.go                   # GORM models
├── repository.go               # Database access layer
├── service.go                  # Business logic
├── handlers.go                 # HTTP handlers
├── events.go                   # Event subscriptions & publishers
├── reader.go                   # Cross-module reader interface + impl
└── module_test.go              # Tests
```

> **Tip:** Keep your module in its own directory within the `kernel-modules` monorepo. The consumer app imports it by path (e.g., `go.edgescale.dev/kernel-contrib/billing`).

---

## The Module Interface

Every module must implement `sdk.Module` — a minimal 4-method interface:

```go
type Module interface {
    // Immutable metadata — called once at registration
    Manifest() Manifest

    // SQL migration files (embedded filesystem). Return nil if none.
    Migrations() fs.FS

    // Called once at boot — set up repos, services, register readers
    Init(ctx Context) error

    // Graceful cleanup (called in reverse dependency order)
    Shutdown() error
}
```

### Optional Capability Interfaces

Modules opt into additional capabilities by implementing these interfaces. The kernel detects them via type assertion during initialization — you only implement the ones your module needs:

```go
// HttpModule — implement this to expose HTTP endpoints.
// Modules return one or more RouteHandlers declaring the route type
// (client or admin) and a registration function.
type HttpModule interface {
    RouteHandlers() []RouteHandler
}

// RouteHandler associates a route type with a registration function.
type RouteHandler struct {
    Type     RouteType        // RouteClient or RouteAdmin
    Register func(*Router)   // called by kernel with appropriately-scoped Router
}

// EventModule — implement this to subscribe to async events
type EventModule interface {
    RegisterEvents(bus EventBus)
}

// HookModule — implement this to register sync interceptors
type HookModule interface {
    RegisterHooks(hooks *HookRegistry)
}

// WorkflowModule — implement this to register Temporal workflows/activities.
// EXPERIMENTAL: Requires a Temporal server. The consumer app must configure
// the Temporal connection — see kernel configuration docs.
type WorkflowModule interface {
    RegisterWorkflows(reg WorkflowRegistry)
    RegisterActivities(reg ActivityRegistry)
}
```

**Route types:**

| Type | Constant | URL Prefix | Middleware |
| --- | --- | --- | --- |
| **Client** | `sdk.RouteClient` | `/v1/{module_id}/` (global) and `/v1/:tenant_id/{module_id}/` (tenant-scoped) | Auth, tenant resolution, user, activation |
| **Admin** | `sdk.RouteAdmin` | `/admin/v1/{module_id}/` | Auth, platform admin check |

A module can implement any combination. For example, a module with both client and admin routes, plus events:

```go
// Satisfies sdk.Module + sdk.HttpModule + sdk.EventModule
type Module struct{}

func (m *Module) Manifest() sdk.Manifest       { /* ... */ }
func (m *Module) Migrations() fs.FS             { /* ... */ }
func (m *Module) Init(ctx sdk.Context) error    { /* ... */ }
func (m *Module) Shutdown() error               { /* ... */ }
func (m *Module) RouteHandlers() []sdk.RouteHandler {
    return []sdk.RouteHandler{
        {Type: sdk.RouteClient, Register: m.registerClientRoutes},
        {Type: sdk.RouteAdmin, Register: m.registerAdminRoutes},
    }
}
func (m *Module) RegisterEvents(b sdk.EventBus) { /* ... */ }
```

### Lifecycle Order

The kernel calls methods in this order:

```bash
1. Manifest()            → at Register() time, validates ID uniqueness
2. Migrations()          → during `migrate` command, runs in dependency order
3. Init(ctx)             → during Serve(), after migrations, in dependency order
4. RegisterEvents()      → immediately after Init()      (only if EventModule)
5. RegisterHooks()       → immediately after events       (only if HookModule)
6. RegisterWorkflows()   → if WorkflowModule AND Temporal is configured
7. RegisterActivities()  → if WorkflowModule AND Temporal is configured
8. RouteHandlers()       → after ALL modules are initialized (only if HttpModule)
9. Shutdown()            → on SIGINT/SIGTERM, in REVERSE dependency order
```

> **Note:** Steps 4-7 run per-module during `initModules()`. Step 8 runs separately during `setupRouter()` after all modules have completed initialization — this ensures readers and event subscriptions from all modules are available before any routes are mounted.

---

## Manifest Configuration

The `Manifest` is the identity card of your module. It's declared once and never changes at runtime.

```go
func (m *Module) Manifest() sdk.Manifest {
    return sdk.Manifest{
        // Required ─────────────────────────────────────
        ID:      "billing",                // unique slug, used in URLs, DB, Redis
        Name:    "Billing & Invoicing",    // human-readable display name
        Version: "1.2.0",                  // semver for tracking
        Type:    sdk.TypeFeature,          // activation model (see below)
        Schema:  "module_billing",         // PostgreSQL schema name

        // Recommended ──────────────────────────────────
        Description: "Invoice generation, payment tracking, and revenue analytics.",
        DependsOn:   []string{"iam"},      // module IDs this depends on

        // Permissions (every route must reference one) ──
        // Label is TranslatableField — use sdk.T() for i18n support.
        Permissions: []sdk.Permission{
            {Key: "billing.invoices.read",   Label: sdk.T("View invoices", "ar", "عرض الفواتير")},
            {Key: "billing.invoices.create", Label: sdk.T("Create invoices", "ar", "إنشاء فواتير")},
            {Key: "billing.invoices.delete", Label: sdk.T("Delete invoices", "ar", "حذف فواتير")},
            {Key: "billing.payments.read",   Label: sdk.T("View payments", "ar", "عرض المدفوعات")},
        },

        // Public events (available for tenant webhooks) ─
        PublicEvents: []sdk.EventDef{
            {
                Subject:        "billing.invoice.created",
                Description:    sdk.T("Fired when a new invoice is generated"),
                PayloadExample: `{"invoice_id": "uuid", "total": 15000}`,
            },
        },

        // Per-tenant configuration fields ───────────────
        Config: []sdk.ConfigFieldDef{
            {
                Key:         "auto_send",
                Type:        "bool",
                Default:     false,
                Label:       sdk.T("Auto-send invoices", "ar", "إرسال تلقائي للفواتير"),
                Description: sdk.T("Automatically send invoices via email on creation."),
            },
            {
                Key:     "currency",
                Type:    "select",
                Default: "USD",
                Label:   sdk.T("Default currency", "ar", "العملة الافتراضية"),
                Options: []sdk.ConfigOption{
                    {Value: "USD", Label: sdk.T("US Dollar", "ar", "دولار أمريكي")},
                    {Value: "EUR", Label: sdk.T("Euro", "ar", "يورو")},
                    {Value: "SAR", Label: sdk.T("Saudi Riyal", "ar", "ريال سعودي")},
                },
            },
            {
                Key:   "tax_rate",
                Type:  "number",
                Label: sdk.T("Tax rate (%)"),
                Min:   ptr(0.0),
                Max:   ptr(100.0),
            },
        },

        // UI navigation items ───────────────────────────
        UINav: []sdk.NavItem{
            {Label: sdk.T("Invoices", "ar", "الفواتير"), Icon: "receipt", Path: "/billing/invoices", Permission: "billing.invoices.read", SortOrder: 1},
            {Label: sdk.T("Payments", "ar", "المدفوعات"), Icon: "payments", Path: "/billing/payments", Permission: "billing.payments.read", SortOrder: 2},
        },

        // Storage prefix for uploaded files ─────────────
        StoragePrefix: "billing",

        // Custom field support ──────────────────────────
        CustomFieldEntities: []string{"invoice", "payment"},

        // Arbitrary tags for grouping/filtering ─────────
        Tags: []string{"premium", "finance"},
    }
}

func ptr(f float64) *float64 { return &f }
```

### Module Types

| Type | Constant | Behavior |
| ------ | ---------- | ---------- |
| **Core** | `sdk.TypeCore` | Always active for every tenant. No activation check. (e.g., IAM, uploads) |
| **Feature** | `sdk.TypeFeature` | Enabled/disabled per tenant via `module_activations` table. |
| **Integration** | `sdk.TypeIntegration` | Installed on demand. Typically third-party connectors. |
| **Admin** | `sdk.TypeAdmin` | Platform-level management modules. Mounted on `/admin/v1/` without tenant scoping. |

> **Note:** If you just need admin endpoints alongside client endpoints, you don't need `TypeAdmin` — use `RouteHandler{Type: sdk.RouteAdmin}` instead. `TypeAdmin` is for modules that are **exclusively** platform-level (no client routes at all). A single module can expose both client and admin routes by returning multiple `RouteHandler` entries. See [Routing & Handlers](#routing--handlers).

### Schema Modes

| Value | Behavior |
| ------- | ---------- |
| `"module_billing"` | Gets its own PostgreSQL schema. Full isolation. Kernel auto-creates it. |
| `"public"` | Tables live in the public schema alongside kernel tables. Simpler, no isolation. |

---

## Database Migrations

### Embedding Migration Files

Create a `migrations/` directory and embed it:

```bash
migrations/
├── embed.go
├── 001_create_invoices.down.sql
├── 001_create_invoices.up.sql
├── 002_add_line_items.down.sql
├── 002_add_line_items.up.sql
└── 003_add_payment_status.down.sql
└── 003_add_payment_status.up.sql
```

**`migrations/embed.go`:**

```go
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
```

**`module.go`:**

```go
import "go.edgescale.dev/kernel-contrib/billing/migrations"

func (m *Module) Migrations() fs.FS {
    return migrations.FS
}
```

### Migration File Naming

Files **must** follow this convention:

```bash
{version}_{description}.up.sql      # forward migration (required)
{version}_{description}.down.sql    # rollback migration (recommended)
```

- `version` — zero-padded integer, e.g., `001`, `002`
- `description` — snake_case description
- `.up.sql` files are **required** — the kernel applies them in order during `migrate`
- `.down.sql` files are **recommended** — the kernel uses them for `migrate rollback`

> **Tip:** Always write `.down.sql` files during development. It's much harder to write them correctly after the fact when you've forgotten what the `.up.sql` changed.

### Writing Migration SQL

Your SQL runs inside the module's schema automatically. The kernel prepends `SET search_path TO module_billing, public;` before your SQL, so you can reference both your own tables and public kernel tables:

```sql
-- 001_create_invoices.up.sql
CREATE TABLE invoices (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   UUID NOT NULL,
    number      TEXT NOT NULL,
    total       BIGINT NOT NULL DEFAULT 0,
    currency    TEXT NOT NULL DEFAULT 'USD',
    status      TEXT NOT NULL DEFAULT 'draft',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ
);

CREATE INDEX idx_invoices_tenant_id ON invoices(tenant_id);
CREATE UNIQUE INDEX idx_invoices_tenant_number ON invoices(tenant_id, number) WHERE deleted_at IS NULL;
```

The corresponding rollback:

```sql
-- 001_create_invoices.down.sql
DROP INDEX IF EXISTS idx_invoices_tenant_number;
DROP INDEX IF EXISTS idx_invoices_tenant_id;
DROP TABLE IF EXISTS invoices;
```

> **Important:** Always include `tenant_id` and index it. The kernel is multi-tenant — every query must be tenant-scoped.

### Rollback

The kernel supports rolling back migrations via the CLI. Each rollback executes the corresponding `.down.sql` file inside a **transaction** — if the SQL fails, the migration record stays intact and no partial rollback occurs.

#### CLI Usage

```bash
# Rollback the last migration for a module
kernel migrate rollback --module billing

# Rollback the last 3 migrations for a module
kernel migrate rollback --module billing --steps 3

# Rollback a kernel-owned migration
kernel migrate rollback --module kernel --steps 1

# Check current migration status first
kernel migrate status
```

#### How It Works

```bash
1. Finds the N most recently applied migrations for the module (highest version first)
2. For each, derives the .down.sql filename from the recorded .up.sql filename
3. If the .down.sql file is missing → stops with an error (no partial rollback)
4. Sets search_path to the module's schema
5. Executes the .down.sql in a transaction
6. Deletes the migration record from schema_migrations (same transaction)
7. Repeats for each step
```

#### Missing `.down.sql` Files

If you attempt to rollback but the `.down.sql` file doesn't exist, the kernel stops immediately with a clear error:

```bash
rollback: missing 003_add_payment_status.down.sql — cannot rollback version 3 of module "billing".
Create the .down.sql file and rebuild.
```

The fix: add the missing `.down.sql` file to your module's `migrations/` directory, rebuild, and retry.

#### Writing Good `.down.sql` Files

| `.up.sql` | `.down.sql` |
| --- | --- |
| `CREATE TABLE x (...)` | `DROP TABLE IF EXISTS x` |
| `ALTER TABLE x ADD COLUMN y ...` | `ALTER TABLE x DROP COLUMN IF EXISTS y` |
| `CREATE INDEX idx_x ON ...` | `DROP INDEX IF EXISTS idx_x` |
| `INSERT INTO x ...` (seed data) | `DELETE FROM x WHERE ...` |
| `ALTER TABLE x RENAME TO y` | `ALTER TABLE y RENAME TO x` |

> **Caution:** Rollbacks that drop columns or tables **destroy data**. In production, prefer writing a new forward migration to fix issues instead of rolling back.

---

## Initialization & Context

When the kernel calls `Init(ctx)`, you receive a fully-wired `sdk.Context`:

```go
type Context struct {
    DB               *gorm.DB                        // Scoped to your module's schema
    PublicDB         *gorm.DB                        // For JOINs to public.users, public.tenants
    Redis            NamespacedRedis                 // Keys auto-prefixed with "module:{id}:"
    Logger           *slog.Logger                    // Tagged with your module ID
    Audit            AuditLogger                     // Hash-chained audit log
    Config           func(uuid.UUID) map[string]any  // Per-tenant config
    Bus              EventBus                        // Async event publishing
    Tasks            TaskExecutor                    // Background task execution
    Search           SearchEngine                    // Full-text search
    Hooks            *HookRegistry                   // Sync hook registration/firing
    IdentityProvider IdentityProvider                // Token validation
    Outbox           OutboxWriter                    // Durable event publishing (transactional)
    Operations       OperationTracker                // Long-running operation tracking
    Features         FeatureFlags                    // Feature flag checking
    ServiceID        string                          // Your module ID
    ValidPermissionKey func(string) bool             // Validate permission keys
}
```

### Typical Init Pattern

> **Naming convention:** Use `m.ctx` for the module's `sdk.Context` (stored at init time) and the bare `ctx` for Go's standard `context.Context` (per-request). This avoids the common confusion of having two `ctx` variables with different types.

```go
type Module struct {
    ctx     sdk.Context
    repo    *InvoiceRepository
    service *InvoiceService
}

func (m *Module) Init(ctx sdk.Context) error {
    m.ctx = ctx

    // Wire up your layers
    m.repo = NewInvoiceRepository(ctx.DB)
    m.service = NewInvoiceService(m.repo, ctx.Bus, ctx.Audit, ctx.Logger)

    // Register a reader for cross-module access
    ctx.RegisterReader(&invoiceReader{repo: m.repo})

    ctx.Logger.Info("billing module initialized")
    return nil
}
```

> **Rule:** `ctx.DB` is scoped — queries target your schema automatically. Use `ctx.PublicDB` only when you need to JOIN with kernel tables like `users` or `tenants`.

### Using the IdentityProvider

Most modules never need to touch `ctx.IdentityProvider` directly — the kernel's middleware handles token validation and user resolution automatically. However, there are cases where a module needs direct access:

```go
// Validate a token outside of the normal HTTP middleware pipeline
// (e.g., in a WebSocket handler, background job, or webhook processor)
identity, err := m.ctx.IdentityProvider.ValidateToken(ctx, rawBearerToken)
if err != nil {
    // Invalid or expired token
    return err
}
// identity.Subject contains the external user ID
// identity.Provider identifies which IdP issued the token
// identity.SignInMethod tells how the user authenticated ("phone", "password", etc.)
```

> **When to use it:** WebSocket authentication, service-to-service token validation, or any code path that runs outside the kernel's HTTP middleware chain.

---

## Routing & Handlers

### Route Registration

Modules declare route handlers via `RouteHandlers()`. Each handler specifies a **route type** (`RouteClient` or `RouteAdmin`) and a registration function that receives a `*sdk.Router`:

```go
func (m *Module) RouteHandlers() []sdk.RouteHandler {
    return []sdk.RouteHandler{
        {Type: sdk.RouteClient, Register: m.registerClientRoutes},
    }
}

func (m *Module) registerClientRoutes(r *sdk.Router) {
    // Tenant-scoped routes — require tenant context
    t := r.Tenant()
    t.GET("/invoices",     "billing.invoices.read",   m.handleListInvoices)
    t.GET("/invoices/:id", "billing.invoices.read",   m.handleGetInvoice)
    t.POST("/invoices",    "billing.invoices.create", m.handleCreateInvoice)
    t.PUT("/invoices/:id", "billing.invoices.create", m.handleUpdateInvoice)
    t.DELETE("/invoices/:id", "billing.invoices.delete", m.handleDeleteInvoice)

    // Global authenticated routes — no tenant context
    r.GET("/invoices/mine", sdk.Self, m.handleMyInvoices)

    // Public (no auth required — mounted outside auth middleware)
    r.GET("/public/pricing", sdk.Public, m.handlePublicPricing)
}
```

### Router Groups

The `Router` offers two scopes for client routes:

| Method | Group | URL Pattern | Middleware |
| --- | --- | --- | --- |
| `r.GET(...)` | Global authenticated | `/v1/{module_id}/{path}` | Auth only |
| `r.Tenant().GET(...)` | Tenant-scoped | `/v1/:tenant_id/{module_id}/{path}` | Auth + tenant + user + activation |
| `r.GET(..., sdk.Public, ...)` | Public | `/v1/{module_id}/public/{path}` | None |

For admin routes (`RouteAdmin`):

| Method | Group | URL Pattern | Middleware |
| --- | --- | --- | --- |
| `r.GET(...)` | Admin | `/admin/v1/{module_id}/{path}` | Auth + platform admin |

### Modules With Both Client and Admin Routes

A single module can expose both client and admin routes:

```go
func (m *IAM) RouteHandlers() []sdk.RouteHandler {
    return []sdk.RouteHandler{
        {Type: sdk.RouteClient, Register: m.registerClientRoutes},
        {Type: sdk.RouteAdmin, Register: m.registerAdminRoutes},
    }
}

func (m *IAM) registerClientRoutes(r *sdk.Router) {
    // Global (no tenant) — for onboarding, /me
    r.GET("/me", sdk.Self, m.handleMe)
    r.POST("/onboard", sdk.Public, m.handleOnboard)

    // Tenant-scoped
    t := r.Tenant()
    t.GET("/members", "iam.members.list", m.handleListMembers)
    t.POST("/roles", "iam.roles.create", m.handleCreateRole)
}

func (m *IAM) registerAdminRoutes(r *sdk.Router) {
    r.GET("/users", "platform.users.list", m.handleListAllUsers)
    r.POST("/tenants/:id/activate", "platform.tenants.manage", m.handleActivateTenant)
}
```

### URL Mapping Summary

| Route Type | Pattern | Example |
| --- | --- | --- |
| Global authenticated | `/v1/{module_id}/{path}` | `GET /v1/iam/me` |
| Tenant-scoped | `/v1/:tenant_id/{module_id}/{path}` | `GET /v1/550e8400.../billing/invoices` |
| Public | `/v1/{module_id}/public/{path}` | `GET /v1/billing/public/pricing` |
| Admin | `/admin/v1/{module_id}/{path}` | `GET /admin/v1/iam/users` |

### Permission Constants

| Constant | Value | Behavior |
| --- | --- | --- |
| `sdk.Public` | `""` | No authentication required. Route is mounted on the public group. |
| `sdk.Self` | `"self"` | User is authenticated but the handler checks ownership manually. |
| `sdk.ReadOnly` | `"readonly"` | Authenticated access with tenant-scoped filtering. No DB transaction or RLS via `SET LOCAL`. |

### API Versioning

API versioning is a **module concern**, not a kernel concern. Version your endpoints within your path namespace:

```go
func (m *Module) registerClientRoutes(r *sdk.Router) {
    t := r.Tenant()
    // V1 (original)
    t.GET("/invoices", "billing.invoices.read", m.handleListInvoicesV1)

    // V2 (breaking change — different response shape)
    t.GET("/v2/invoices", "billing.invoices.read", m.handleListInvoicesV2)
}
```

### Combining Permissions

Use `sdk.RequireAny()` when a route should be accessible with any one of several permissions:

```go
t.GET("/dashboard", sdk.RequireAny("billing.invoices.read", "billing.payments.read"), m.handleDashboard)
```

### Handler Anatomy

Every handler receives a `*gin.Context` with these values pre-set by the kernel middleware:

```go
func (m *Module) handleCreateInvoice(c *gin.Context) {
    // ── Identity (set by authenticate middleware) ──────
    userID   := c.GetString("user_id")          // IdP subject (external ID)
    provider := c.GetString("auth_provider")     // "firebase", "okta", etc.

    // ── Tenant (set by resolveTenant middleware) ───────
    tenantID, _ := c.Get("tenant_id")            // uuid.UUID

    // ── Internal user (set by resolveUser middleware) ──
    internalUserID, _ := c.Get("internal_user_id") // uuid.UUID (database PK)

    // ── Locale (set by parseLocale middleware) ─────────
    locale := sdk.Locale(c)                       // "en", "ar", etc.

    // ── Request tracking ──────────────────────────────
    requestID := c.GetString("request_id")        // UUID for tracing

    // ... your business logic
}
```

---

## Response & Error Handling

The kernel uses a Cloudflare v4-style envelope for all responses. **Always use the `sdk` helpers** — never write raw JSON:

### Success Responses

```go
// 200 OK — single resource or payload
sdk.OK(c, invoice)

// 200 OK — with informational message
sdk.OKWithMessage(c, invoice, "invoice sent to customer")

// 201 Created — after creating a resource
sdk.Created(c, invoice)

// 202 Accepted — for async operations
sdk.Accepted(c, gin.H{"operation_id": opID})

// 204 No Content — for deletes
sdk.NoContent(c)

// 200 OK — paginated list
page := sdk.ParsePageRequest(c)
result, err := sdk.Paginate[Invoice](
    m.ctx.DB.Where("tenant_id = ?", tenantID),
    page,
)
sdk.List(c, result.Items, result.Meta)
```

All success responses produce this shape:

```json
{
    "success": true,
    "result": { ... },
    "errors": [],
    "messages": [],
    "result_info": { "page": 1, "per_page": 25, "total_count": 42, "total_pages": 2 }
}
```

### Error Responses

```go
// Typed errors (preferred — carries HTTP status automatically)
sdk.Error(c, sdk.NotFound("invoice", id))      // 404
sdk.Error(c, sdk.BadRequest("invalid amount")) // 400
sdk.Error(c, sdk.Forbidden("not the owner"))   // 403
sdk.Error(c, sdk.Conflict("already exists"))   // 409
sdk.Error(c, sdk.Internal("database error"))   // 500
sdk.Error(c, sdk.Unauthorized("expired token"))// 401
sdk.Error(c, sdk.Unprocessable("invalid data"))// 422
sdk.Error(c, sdk.RateLimited("slow down"))     // 429
sdk.Error(c, sdk.Unavailable("try later"))     // 503

// Auto-detect error type (wraps unknown errors as 500)
sdk.FromError(c, err)

// Check error type programmatically
if se, ok := sdk.IsServiceError(err); ok {
    // se.HTTPStatus, se.Code, se.Message
}

// Multiple validation errors
sdk.Errs(c, 400, []sdk.APIError{
    {Code: "validation_error", Message: "field 'amount' is required"},
    {Code: "validation_error", Message: "field 'currency' is invalid"},
})
```

### Request Binding & Validation

```go
// Bind JSON body + validate struct tags
var req CreateInvoiceRequest
if !sdk.BindAndValidate(c, &req) {
    return // 400 already sent
}

// Bind query parameters
var filter ListFilter
if !sdk.BindQuery(c, &filter) {
    return
}

// Bind URI parameters (:id)
var params struct {
    ID uint64 `uri:"id" binding:"required"`
}
if !sdk.BindURI(c, &params) {
    return
}
```

---

## Events & Async Communication

### Publishing Events

```go
// Fire-and-forget (in-process, via EventBus)
m.ctx.Bus.Publish(c.Request.Context(), "billing.invoice.created", map[string]any{
    "invoice_id": invoice.ID,
    "tenant_id":  tenantID,
    "total":      invoice.Total,
})

// Durable (transactional outbox — guaranteed delivery)
m.ctx.Outbox.WriteEvent(c.Request.Context(), "billing.invoice.created", map[string]any{
    "invoice_id": invoice.ID,
    "total":      invoice.Total,
})
```

> **When to use which:**
>
> - `Bus.Publish` — fast, in-process, best-effort. Use for notifications, cache invalidation.
> - `Outbox.WriteEvent` — durable, written in the same DB transaction as your data. Use for critical business events that must not be lost.

### Subscribing to Events

```go
func (m *Module) RegisterEvents(bus sdk.EventBus) {
    bus.Subscribe("billing", "iam.user.created", func(ctx context.Context, env sdk.EventEnvelope) error {
        m.ctx.Logger.Info("new user created, setting up billing profile",
            "user_id", env.UserID,
            "tenant_id", env.TenantID,
        )

        var payload struct {
            UserID string `json:"user_id"`
        }
        json.Unmarshal(env.Payload, &payload)

        // Create billing profile for the new user...
        return nil
    })
}
```

The `EventEnvelope` carries context across async boundaries:

| Field | Description |
| ------- | ------------- |
| `Subject` | Event topic (e.g., `"iam.user.created"`) |
| `Payload` | JSON event data |
| `TenantID` | Tenant that originated the event |
| `UserID` | User who triggered it |
| `RequestID` | Originating HTTP request ID |
| `TraceID` / `SpanID` | Distributed tracing context |
| `ServiceID` | Which module produced the event |
| `Timestamp` | When the event was produced |

---

## Hooks & Sync Interception

Hooks let modules intercept each other's operations **synchronously**. Unlike events, hooks can **abort** operations.

### Registering Hooks

```go
func (m *Module) RegisterHooks(hooks *sdk.HookRegistry) {
    // Intercept BEFORE an order is created (can abort)
    hooks.Before(sdk.BeforeHookPoint("orders", "create"), func(ctx context.Context, payload any) error {
        order := payload.(*CreateOrderPayload) // type depends on publisher

        // Check billing status
        if !m.service.HasActiveSubscription(ctx, order.TenantID) {
            return sdk.Abort(sdk.Forbidden("billing subscription required"))
        }

        return nil // allow the operation to continue
    })

    // React AFTER a tenant is provisioned (cannot abort)
    hooks.After(sdk.AfterHookPoint("kernel", "tenant.provisioned"), func(ctx context.Context, payload any) error {
        event := payload.(sdk.TenantProvisionedEvent)
        return m.service.CreateDefaultBillingPlan(ctx, event.TenantID)
    })
}
```

### Firing Hooks (in your module)

If your module exposes hook points for others to intercept:

```go
func (s *Service) CreateInvoice(ctx context.Context, invoice *Invoice) error {
    // Let other modules intercept before creation
    if err := s.hooks.FireBefore(ctx, "before.billing.invoice.create", invoice); err != nil {
        if ae, ok := sdk.IsAbortError(err); ok {
            return ae.Reason // return the abort reason
        }
        return err
    }

    // ... create the invoice ...

    // Let other modules react after creation
    s.hooks.FireAfter(ctx, "after.billing.invoice.create", invoice)

    return nil
}
```

### Hook Point Naming Convention

```bash
{lifecycle}.{module_id}.{action}
```

| Part | Values | Examples |
| ------ | -------- | --------- |
| `lifecycle` | `before`, `after` | — |
| `module_id` | Your module ID | `billing`, `orders`, `kernel` |
| `action` | The operation | `invoice.create`, `tenant.provisioned` |

Helper functions:

```go
point := sdk.BeforeHookPoint("billing", "invoice.create")
// → "before.billing.invoice.create"

point := sdk.AfterHookPoint("billing", "invoice.create")
// → "after.billing.invoice.create"
```

---

## Cross-Module Communication (Readers)

Readers enable **type-safe, read-only** data access between modules without creating import dependencies.

### Step 1: Define the Reader Interface

In your module's public API:

```go
// reader.go

// BillingReader is the cross-module interface exposed by the billing module.
// Other modules import this interface only — not the implementation.
type BillingReader interface {
    HasActiveSubscription(ctx context.Context, tenantID uuid.UUID) (bool, error)
    GetSubscriptionTier(ctx context.Context, tenantID uuid.UUID) (string, error)
}
```

### Step 2: Implement & Register It

```go
type billingReader struct {
    repo *SubscriptionRepository
}

func (r *billingReader) HasActiveSubscription(ctx context.Context, tenantID uuid.UUID) (bool, error) {
    return r.repo.ExistsActive(ctx, tenantID)
}

func (r *billingReader) GetSubscriptionTier(ctx context.Context, tenantID uuid.UUID) (string, error) {
    sub, err := r.repo.FindByTenant(ctx, tenantID)
    if err != nil { return "", err }
    return sub.Tier, nil
}

// In Init():
func (m *Module) Init(ctx sdk.Context) error {
    // ...
    ctx.RegisterReader(&billingReader{repo: m.repo})
    return nil
}
```

### Step 3: Consume from Another Module

```go
// In your module's Init() or handler:
reader, err := sdk.Reader[BillingReader](m.ctx, "billing")
if err != nil {
    // billing module not registered — handle gracefully
}

active, _ := reader.HasActiveSubscription(ctx, tenantID)
```

> **Important:** The reader interface lives in the **providing module's** package. Consuming modules import the reader type directly from the provider (e.g., `import "go.edgescale.dev/kernel-contrib/billing"` to access `billing.BillingReader`). Go's structural typing ensures compatibility as long as the registered struct satisfies the interface.

---

## Background Tasks

For long-running operations (imports, exports, report generation):

```go
opID, err := m.ctx.Tasks.Execute(c.Request.Context(), sdk.TaskDefinition{
    ID:        uuid.New().String(),
    Name:      "invoice_export",
    ServiceID: "billing",
    TenantID:  tenantID,
    Retries:   3,
    Timeout:   10 * time.Minute,
    Handler: func(ctx context.Context, progress sdk.ProgressReporter) error {
        invoices, err := m.repo.FindAll(ctx, tenantID)
        if err != nil {
            return fmt.Errorf("fetch invoices for export: %w", err)
        }
        for i, inv := range invoices {
            // Export logic...
            progress.Report(ctx, (i+1)*100/len(invoices), fmt.Sprintf("Exporting %d/%d", i+1, len(invoices)))
        }
        return nil
    },
})
if err != nil {
    sdk.FromError(c, err)
    return
}

// Return 202 Accepted with the operation ID
sdk.Accepted(c, gin.H{"operation_id": opID})
```

The `TaskExecutor` is pluggable — the consumer app decides the backend (inline goroutine, Temporal, etc.). Your module doesn't care which implementation is used.

---

## Operations (Long-Running)

The `OperationTracker` on `sdk.Context` lets modules track the lifecycle of long-running async operations — providing status polling, progress updates, and completion/failure recording.

### Creating & Tracking an Operation

```go
func (m *Module) handleBulkImport(c *gin.Context) {
    tenantID := c.MustGet("tenant_id").(uuid.UUID)
    userID := c.MustGet("internal_user_id").(uuid.UUID)

    // 1. Create a tracked operation
    opID, err := m.ctx.Operations.Create(c.Request.Context(), sdk.OperationInput{
        ModuleID: "billing",
        TenantID: tenantID,
        UserID:   userID,
        Type:     "import",
    })
    if err != nil {
        sdk.FromError(c, err)
        return
    }

    // 2. Run work in the background
    go func() {
        ctx := context.Background()
        items, err := m.parseCSV(ctx, c.Request.Body)
        if err != nil {
            m.ctx.Operations.Fail(ctx, opID, err.Error())
            return
        }

        for i, item := range items {
            if err := m.repo.Create(ctx, item); err != nil {
                m.ctx.Operations.Fail(ctx, opID, fmt.Sprintf("row %d: %s", i, err.Error()))
                return
            }
            m.ctx.Operations.UpdateProgress(ctx, opID, i+1, len(items))
        }

        result := fmt.Sprintf("imported %d records", len(items))
        m.ctx.Operations.Complete(ctx, opID, &result)
    }()

    // 3. Return 202 Accepted immediately
    sdk.Accepted(c, gin.H{"operation_id": opID})
}
```

### Polling Operation Status

```go
func (m *Module) handleGetOperation(c *gin.Context) {
    opID, err := uuid.Parse(c.Param("id"))
    if err != nil {
        sdk.Error(c, sdk.BadRequest("invalid operation ID"))
        return
    }

    op, err := m.ctx.Operations.Get(c.Request.Context(), opID)
    if err != nil {
        sdk.FromError(c, err)
        return
    }

    sdk.OK(c, op)
}
```

### Operation Statuses

| Status | Meaning |
| --- | --- |
| `"pending"` | Created, not yet started |
| `"running"` | Work in progress, `Progress`/`TotalItems` are updating |
| `"completed"` | Finished successfully, optional `Result` available |
| `"failed"` | Finished with an error, `Error` contains the message |
| `"cancelled"` | Terminated by the user or system |

---

## Feature Flags

The `FeatureFlags` interface on `sdk.Context` lets modules conditionally enable functionality per-tenant:

```go
func (m *Module) handleCreateInvoice(c *gin.Context) {
    tenantID := c.MustGet("tenant_id").(uuid.UUID)

    // Check if auto-receipts are enabled for this org
    if m.ctx.Features.Enabled(c.Request.Context(), "billing.auto_receipts", tenantID.String()) {
        // Generate receipt automatically...
    }

    // ...
}
```

> **Note:** The kernel provides a **noop implementation** that returns `false` for all flags when no feature flags module is registered. Your code won't panic — it will simply take the "flag off" path. A dedicated feature flags module (e.g., backed by LaunchDarkly, Unleash, or a database table) can be registered by the consumer app.

---

## Transactional Outbox

For events that **must not be lost**, use the transactional outbox. This writes the event to a database table in the **same transaction** as your business data — if the transaction commits, the event is guaranteed to be dispatched.

### When to Use Outbox vs. EventBus

| Mechanism | Guarantees | Use Case |
| --- | --- | --- |
| `Bus.Publish()` | Best-effort, in-process | Notifications, cache invalidation, UI updates |
| `Outbox.WriteEvent()` | Durable, exactly-once intent | Payment confirmations, order state changes, webhook triggers |

### Usage

```go
func (s *Service) CompletePayment(ctx context.Context, tx *gorm.DB, paymentID uuid.UUID) error {
    // 1. Update payment status (in the same transaction)
    if err := tx.WithContext(ctx).
        Model(&Payment{}).
        Where("id = ?", paymentID).
        Update("status", "completed").Error; err != nil {
        return err
    }

    // 2. Write event to outbox (same transaction — atomic with the update above)
    if err := s.outbox.WriteEvent(ctx, "payments.payment.completed", map[string]any{
        "payment_id": paymentID,
        "status":     "completed",
    }); err != nil {
        return fmt.Errorf("outbox write: %w", err)
    }

    return nil
}
```

The outbox module's poller picks up committed events and dispatches them to the `EventBus`. If the transaction rolls back, the event is never dispatched — guaranteeing consistency between your data and your events.

---

## Caching

Use the SDK's generic `Cache` helper with your namespaced Redis:

```go
invoice, err := sdk.Cache(ctx, m.ctx.Redis, "inv:"+id, 5*time.Minute, func() (*Invoice, error) {
    return m.repo.FindByID(ctx, id)
})
```

Invalidate on writes:

```go
// Single key
sdk.Invalidate(ctx, m.ctx.Redis, "inv:"+id)

// Multiple keys
sdk.InvalidateMany(ctx, m.ctx.Redis, "inv:"+id, "inv:list:"+tenantID.String())

// All keys with a prefix
sdk.InvalidatePrefix(ctx, m.ctx.Redis, "inv:")
```

> Your Redis keys are automatically namespaced to `module:billing:`, so `"inv:123"` is actually stored as `"module:billing:inv:123"`. No key collisions between modules.

---

## Audit Logging

The kernel provides a hash-chained audit log. Log significant mutations:

```go
m.ctx.Audit.Log(c.Request.Context(), sdk.AuditEntry{
    Action:     sdk.AuditCreate,
    Resource:   "invoice",
    ResourceID: fmt.Sprintf("%d", invoice.ID),
    Changes: map[string]sdk.AuditChange{
        "total":    {Old: nil, New: invoice.Total},
        "currency": {Old: nil, New: invoice.Currency},
        "status":   {Old: nil, New: "draft"},
    },
})
```

Available audit actions:

```go
sdk.AuditCreate      sdk.AuditUpdate      sdk.AuditDelete
sdk.AuditRestore     sdk.AuditLogin       sdk.AuditLogout
sdk.AuditExport      sdk.AuditImport      sdk.AuditApprove
sdk.AuditReject      sdk.AuditActivate    sdk.AuditDeactivate
```

You can also define custom actions:

```go
const AuditVoid sdk.AuditAction = "void"
```

> The `user_id`, `tenant_id`, `ip_address`, and `request_id` are automatically captured from the request context — you don't need to set them.

---

## Full-Text Search

If the consumer has configured a search engine (Meilisearch, Elasticsearch, etc.):

```go
// Create an index during Init()
m.ctx.Search.CreateIndex(ctx, "billing_invoices", sdk.IndexSettings{
    PrimaryKey:           "id",
    SearchableAttributes: []string{"number", "customer_name", "notes"},
    FilterableAttributes: []string{"tenant_id", "status", "currency"},
    SortableAttributes:   []string{"created_at", "total"},
})

// Index documents on create/update
m.ctx.Search.Index(ctx, "billing_invoices", fmt.Sprintf("%d", invoice.ID), invoice)

// Search
result, err := m.ctx.Search.Search(ctx, "billing_invoices", sdk.SearchQuery{
    Query:   "acme",
    Filters: map[string]any{"tenant_id": tenantID.String(), "status": "paid"},
    Sort:    []string{"created_at:desc"},
    Limit:   25,
})
```

---

## Translatable Fields

The SDK provides `TranslatableField` (`map[string]string`) for multilingual text, stored as JSONB in PostgreSQL.

### In GORM Models (Entity Data)

```go
type Product struct {
    ID    uint64                `json:"id" gorm:"primaryKey"`
    Name  sdk.TranslatableField `json:"name" gorm:"type:jsonb"`
    TenantID uuid.UUID          `json:"tenant_id" gorm:"type:uuid;not null"`
}
```

JSONB storage: `{"en": "Widget", "ar": "أداة"}`

```go
// Create with translations
product := Product{
    Name: sdk.T("Widget", "ar", "أداة", "fr", "Widget"),
}

// Read in the user's locale
locale := sdk.Locale(c)
displayName := product.Name.Get(locale) // falls back to "en" then first available
```

### In Manifest Fields (UI Metadata)

Several manifest fields are `TranslatableField` — these are user-facing strings that appear in admin panels, navigation sidebars, and permission management UIs:

| Struct | Translatable Fields |
| --- | --- |
| `Permission` | `Label` |
| `ConfigFieldDef` | `Label`, `Description`, `Placeholder`, `Group` |
| `ConfigOption` | `Label` |
| `NavItem` | `Label` |
| `EventDef` | `Description` |

Use `sdk.T()` for a quick English-first declaration with optional translations:

```go
Permissions: []sdk.Permission{
    {Key: "orders.create", Label: sdk.T("Create orders", "ar", "إنشاء طلبات")},
    {Key: "orders.read",   Label: sdk.T("View orders")},  // English-only is fine
},
```

> **Note:** Machine identifiers (`Key`, `Value`, `Subject`, `Path`, `Icon`, `Permission`) remain plain `string` — only user-facing display text is translatable.

---

## Testing

The SDK provides in-memory test doubles for all infrastructure:

```go
func TestCreateInvoice(t *testing.T) {
    // Create a test context with in-memory fakes
    ctx := sdk.NewTestContext("billing")

    // Wire up your module
    repo := NewInvoiceRepository(ctx.DB) // you'd use a test DB here
    service := NewInvoiceService(repo, ctx.Bus, ctx.Audit, ctx.Logger)

    // Execute
    invoice, err := service.Create(context.Background(), CreateInvoiceInput{
        TenantID: uuid.New(),
        Total: 5000,
    })
    require.NoError(t, err)
    assert.Equal(t, int64(5000), invoice.Total)

    // Assert events were published
    events := ctx.Bus.(*sdk.TestBus).Events()
    assert.Len(t, events, 1)
    assert.Equal(t, "billing.invoice.created", events[0].Subject)

    // Assert audit entries were logged
    entries := ctx.Audit.(*sdk.TestAuditLogger).Entries()
    assert.Len(t, entries, 1)
    assert.Equal(t, sdk.AuditCreate, entries[0].Action)

    // Assert background tasks
    tasks := ctx.Tasks.(*sdk.TestTaskExecutor).Tasks()
    assert.Len(t, tasks, 0) // no async tasks for this operation
}
```

Available test doubles:

| SDK Interface | Test Double | Inspect Method |
| --- | --- | --- |
| `EventBus` | `*sdk.TestBus` | `.Events()`, `.Reset()` |
| `AuditLogger` | `*sdk.TestAuditLogger` | `.Entries()` |
| `TaskExecutor` | `*sdk.TestTaskExecutor` | `.Tasks()` |
| `SearchEngine` | `*sdk.TestSearchEngine` | no-op (all methods return nil) |

---

## Integration into a Consumer App

Once your module is built, consumers register it in their `main.go`:

```go
package main

import (
    "go.edgescale.dev/kernel"
    "go.edgescale.dev/kernel-contrib/iam"
    "go.edgescale.dev/kernel-contrib/billing"
    "go.edgescale.dev/kernel-contrib/inventory"
)

func main() {
    cfg := kernel.LoadConfig()
    k := kernel.New(cfg)

    // Set pluggable implementations
    k.SetIdentityProvider(firebase.New(...))
    k.SetEventBus(nats.NewBus(...))
    k.SetUserResolver(iamModule.UserResolver())     // typically from IAM module
    k.SetAdminResolver(iamModule.AdminResolver())   // for platform admin routes
    k.SetAuditLogger(auditModule.Logger())           // pluggable audit logging
    k.SetOutboxWriter(outboxModule.Writer())         // transactional outbox
    k.SetOperationTracker(opsModule.Tracker())       // long-running op tracking
    k.SetFeatureFlags(ffModule.Flags())              // feature flag checking

    // Register modules — order doesn't matter (kernel sorts by dependencies)
    k.MustRegister(iam.New())
    k.MustRegister(billing.New())
    k.MustRegister(inventory.New())

    // Optional: register custom CLI commands
    k.AddCommand(myCustomSeedCommand())

    // Run the CLI (handles: serve, migrate, etc.)
    k.Execute()
}
```

### Pluggable Implementations

The kernel uses pluggable interfaces for all infrastructure. Set them before calling `Execute()`:

| Setter | Interface | Fallback |
| --- | --- | --- |
| `SetIdentityProvider()` | `sdk.IdentityProvider` | All auth requests rejected |
| `SetEventBus()` | `sdk.EventBus` | No-op bus (events are silently dropped) |
| `SetUserResolver()` | `sdk.UserResolver` | All tenant-scoped requests rejected |
| `SetAdminResolver()` | `sdk.AdminResolver` | All admin requests rejected |
| `SetAuditLogger()` | `sdk.AuditLogger` | No-op logger (entries discarded) |
| `SetOutboxWriter()` | `sdk.OutboxWriter` | No-op (events discarded) |
| `SetOperationTracker()` | `sdk.OperationTracker` | Unavailable |
| `SetFeatureFlags()` | `sdk.FeatureFlags` | All flags return `false` |
| `SetTaskExecutor()` | `sdk.TaskExecutor` | None |
| `SetSearchEngine()` | `sdk.SearchEngine` | None |

### CLI Commands

```bash
# ── Server ─────────────────────────────────────────────
go run main.go serve                                     # Boot → Init → Serve

# ── Migrations ─────────────────────────────────────────
go run main.go migrate                                   # Run all pending migrations
go run main.go migrate status                            # Show applied migrations
go run main.go migrate rollback --module billing          # Rollback last migration
go run main.go migrate rollback --module billing --steps 3 # Rollback last 3

# ── Module Management ──────────────────────────────────
go run main.go module list                               # List all registered modules
go run main.go module enable billing --tenant <id>       # Enable module for tenant
go run main.go module disable billing --tenant <id>      # Disable module for tenant
go run main.go module status --tenant <id>               # Show activation status
go run main.go module deps                               # Show dependency graph

# ── Tenant Management ─────────────────────────────────
go run main.go tenant provision <id>                     # Provision new tenant
go run main.go tenant provision <id> --admin-email a@b.c # With admin email hint
go run main.go tenant deprovision <id> --confirm yes     # Deactivate all modules
go run main.go tenant list                               # List tenants + active counts

# ── Platform Administration ───────────────────────────
go run main.go platform grant <user-id>                  # Grant platform_admin role
go run main.go platform grant <user-id> --role super     # Grant custom role
go run main.go platform revoke <user-id>                 # Revoke all platform roles
go run main.go platform list                             # List all platform admins
```

> **Note:** The `platform` commands require an `AdminResolver` that implements `sdk.PlatformManager`. The `AddCommand()` method lets consumer apps register custom CLI commands alongside the kernel's built-in commands.

---

## Best Practices & Conventions

### Do's

| Practice | Reason |
| ---------- | -------- |
| Use `ctx.DB` for all queries | Schema isolation is automatic |
| Always include `tenant_id` in every table | Multi-tenancy enforcement |
| Declare all permissions in Manifest | Kernel validates at boot |
| Use `sdk.Error()` / `sdk.OK()` for all responses | Consistent API envelope |
| Use events for cross-module side effects | Loose coupling |
| Use readers for cross-module data access | Type-safe, no circular imports |
| Use `sdk.BindAndValidate()` for request parsing | Consistent error format |
| Use `sdk.Paginate[T]()` for list endpoints | Standard pagination |
| Log with `ctx.Logger` | Structured, tagged with module ID |
| Return `error` from `Init()` on fatal issues | Kernel aborts gracefully |

### Don'ts

| Anti-Pattern | Why |
| -------------- | ----- |
| Import another module directly | Creates compile-time coupling. Use readers/events instead. |
| Use `ctx.PublicDB` for writes | Public DB is for reads/JOINs only. Write to your own schema. |
| Access raw Redis without `ctx.Redis` | Bypasses namespace isolation. |
| Create routes without permissions | Every secure route must declare a permission. |
| Use `c.JSON()` directly | Breaks the standard envelope format. |
| Panic in handlers / Init | Use `error` returns. Panics crash the process. |
| Skip `tenant_id` in queries | Data leaks across tenants. |
| Hardcode schema names in SQL | Use the kernel's search_path mechanism. |

### Naming Conventions

| Item | Convention | Example |
| ------ | ----------- | --------- |
| Module ID | lowercase slug | `billing`, `hr_payroll` |
| Schema | `module_{id}` | `module_billing` |
| Permission key | `{module}.{entities}.{action}` | `billing.invoices.create` |
| Event subject | `{module}.{entity}.{past_action}` | `billing.invoice.created` |
| Hook point | `{lifecycle}.{module}.{action}` | `before.billing.invoice.create` |
| Redis key | descriptive, colon-separated | `inv:{id}`, `list:{tenant_id}` |
| Migration file | `{NNN}_{description}.up.sql` | `001_create_invoices.up.sql` |

> **Why plural for permissions, singular for events?** Permissions protect **resource collections** ("can you act on invoices?"), so they use the plural form. Events describe **something that happened to a single entity** ("this invoice was created"), so they use the singular form. Hooks follow the event convention since they intercept individual operations.

---

## GORM Model Helpers

The SDK provides composable base structs:

```go
// Full base model: uuid.UUID PK + timestamps + soft delete
// ID is generated via gen_random_uuid() (Postgres) or BeforeCreate hook (SQLite)
type Invoice struct {
    sdk.BaseModel                                  // ID (uuid.UUID), CreatedAt, UpdatedAt, DeletedAt
    TenantID uuid.UUID `json:"tenant_id" gorm:"type:uuid;not null;index"`
    Total    int64     `json:"total"`
}

// Just timestamps (you provide your own PK)
type AuditEntry struct {
    ID       uuid.UUID `json:"id" gorm:"primaryKey;type:uuid"`
    sdk.Timestamped                                // CreatedAt, UpdatedAt
}

// Just soft delete
type Archive struct {
    ID uint64
    sdk.SoftDeletable                              // DeletedAt
}

// GDPR data erasure
type UserProfile struct {
    sdk.BaseModel
    Email string
}

func (u *UserProfile) ErasePersonalData() error {
    u.Email = "redacted@deleted.local"
    return nil
}
```

### BaseModel Details

`sdk.BaseModel` provides:

| Field | Type | GORM Tag |
| --- | --- | --- |
| `ID` | `uuid.UUID` | `type:uuid;primaryKey;default:gen_random_uuid()` |
| `CreatedAt` | `time.Time` | `autoCreateTime` |
| `UpdatedAt` | `time.Time` | `autoUpdateTime` |
| `DeletedAt` | `gorm.DeletedAt` | `index` (soft delete) |

The `BeforeCreate` hook auto-generates a UUID if one isn't set, ensuring compatibility with both PostgreSQL and SQLite (useful for tests).

### JSONB Column Type

The SDK provides `sdk.JSONB` — a cross-database JSON column type that handles both `[]byte` (PostgreSQL) and `string` (SQLite) driver values:

```go
type Order struct {
    sdk.BaseModel
    TenantID uuid.UUID `json:"tenant_id" gorm:"type:uuid;not null"`
    Metadata sdk.JSONB `json:"metadata" gorm:"type:jsonb"`
}

// Store arbitrary JSON
order.Metadata = sdk.JSONB(`{"source": "web", "campaign": "spring_sale"}`)

// Parse it back
var meta map[string]any
json.Unmarshal([]byte(order.Metadata), &meta)
```

> **When to use `sdk.JSONB` vs `sdk.TranslatableField`:** Use `JSONB` for arbitrary JSON data. Use `TranslatableField` specifically for `map[string]string` translation data.

---

## Rate Limiting

Protect sensitive endpoints with the SDK's rate limiter:

```go
func (m *Module) registerClientRoutes(r *sdk.Router) {
    t := r.Tenant()
    t.POST("/invoices/send",
        "billing.invoices.create",
        sdk.RateLimit("send_invoice", 10, time.Minute, m.ctx.Redis.Client()),
        m.handleSendInvoice,
    )
}
```

This limits each IP to 10 requests per minute on this endpoint. Uses a Lua script for atomic increment + TTL in a single Redis round-trip. Fails open if Redis is unavailable — a Redis outage won't block traffic.

---

## Idempotency

The SDK provides helpers for idempotent request processing, preventing duplicate operations when clients retry requests:

```go
func (m *Module) handleCreatePayment(c *gin.Context) {
    tenantID := c.MustGet("tenant_id").(uuid.UUID)
    userID := c.MustGet("internal_user_id").(uuid.UUID)
    idempotencyKey := c.GetHeader("Idempotency-Key")

    // Check if this request was already processed
    duplicate, err := sdk.Idempotent(
        c.Request.Context(),
        m.ctx.Redis.Client(),
        tenantID.String(),
        userID.String(),
        idempotencyKey,
        24*time.Hour,
    )
    if err != nil {
        sdk.FromError(c, err)
        return
    }

    if duplicate {
        // Return the cached response from the first processing
        cached, _ := sdk.GetIdempotentResult(
            c.Request.Context(),
            m.ctx.Redis.Client(),
            tenantID.String(),
            userID.String(),
            idempotencyKey,
        )
        if cached != nil {
            c.Data(200, "application/json", cached)
            return
        }
        sdk.Error(c, sdk.Conflict("request already processed"))
        return
    }

    // Process the request...
    result := processPayment(c)

    // Store the result for duplicate requests
    resultBytes, _ := json.Marshal(result)
    sdk.StoreIdempotentResult(
        c.Request.Context(),
        m.ctx.Redis.Client(),
        tenantID.String(),
        userID.String(),
        idempotencyKey,
        resultBytes,
        24*time.Hour,
    )

    sdk.Created(c, result)
}
```

The idempotency key is namespaced by tenant and user to prevent cross-user collisions. If Redis is `nil` or the key is empty, the check is skipped (no-op).

---

## Kernel API Endpoints

The kernel exposes management endpoints under the `/_kernel` namespace. These are authenticated but not tenant-scoped:

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/_kernel/modules` | List all registered modules with metadata |
| `GET` | `/_kernel/modules/active` | List modules active for the requesting tenant |
| `GET` | `/_kernel/permissions` | List all permissions declared by all modules |
| `GET` | `/healthz` | Liveness probe (always 200 if process is running) |
| `GET` | `/readyz` | Readiness probe (200 if DB and Redis are reachable) |

The `/_kernel/permissions` endpoint returns all permissions with their translatable labels, organized by module — useful for building admin UIs that manage role-permission assignments.

---

## Resolver Interfaces

The kernel uses resolver interfaces to decouple identity resolution from the kernel itself. These are typically implemented by the IAM module and injected via `kernel.SetUserResolver()` / `kernel.SetAdminResolver()`.

### UserResolver

Resolves an authenticated identity into an internal user with tenant-scoped permissions:

```go
type UserResolver interface {
    ResolveUser(ctx context.Context, provider, externalID string, tenantID uuid.UUID) (*ResolvedUser, error)
}

type ResolvedUser struct {
    InternalID  uuid.UUID   // kernel-internal UUID
    Permissions []string    // permission keys granted in this context
}
```

The kernel's `resolveUser` middleware calls this on every tenant-scoped request. The IAM module provides the production implementation.

### AdminResolver

Resolves platform-level admin identity (separate from per-tenant user resolution):

```go
type AdminResolver interface {
    ResolveAdmin(ctx context.Context, provider, externalID string) (*ResolvedUser, error)
}
```

### PlatformManager (Optional Extension)

If the `AdminResolver` also implements `PlatformManager`, the kernel's CLI commands (`platform grant/revoke/list`) delegate to it:

```go
type PlatformManager interface {
    GrantRole(ctx context.Context, userID uuid.UUID, roleSlug string) error
    RevokeAllRoles(ctx context.Context, userID uuid.UUID) error
    ListAdmins(ctx context.Context) ([]PlatformAdmin, error)
}
```

### PermissionSet

The SDK provides `PermissionSet` for efficient permission checking in handlers:

```go
// Created by the kernel middleware from the resolved user's permissions
ps := sdk.NewPermissionSet([]string{"billing.invoices.read", "billing.invoices.create"})

ps.Has("billing.invoices.read")    // true — exact match
ps.Has("billing.payments.read")    // false
ps.Has("billing.invoices.delete")  // false

// Wildcard support
ps2 := sdk.NewPermissionSet([]string{"billing.*"})
ps2.Has("billing.invoices.read")   // true — namespace wildcard

ps3 := sdk.NewPermissionSet([]string{"*"})
ps3.Has("anything.here")           // true — global wildcard (owner)

// Check multiple
ps.HasAny("billing.invoices.read", "hr.employees.read") // true
```

---

## Troubleshooting

### "Why is my reader nil?"

Your module's `Init()` runs **before** routes are mounted, but the module you're reading from may not be initialized yet. This happens when:

- You're accessing the reader in `Init()` instead of lazily in a handler
- The providing module isn't listed in your `DependsOn`

**Fix:** Add the provider to `DependsOn` in your manifest, and access readers lazily:

```go
// Correct — reader resolved at request time
func (m *Module) handleOrder(c *gin.Context) {
    reader, err := sdk.Reader[billing.BillingReader](&m.ctx, "billing")
    // ...
}

// Wrong — reader may not be registered yet during Init()
func (m *Module) Init(ctx sdk.Context) error {
    reader, _ := sdk.Reader[billing.BillingReader](&ctx, "billing") // may fail
}
```

### "Why do I get permission denied?"

1. **Permission not in Manifest** — every permission key used in `RouteHandlers` must be listed in `Manifest().Permissions`
2. **Permission not assigned to role** — the user's role must include the permission
3. **Module not activated** — `TypeFeature` modules need an activation row for the tenant

### "How do I debug migration failures?"

```bash
# Check which migrations have been applied
kernel migrate status

# Look for errors in the migration output
kernel migrate --module billing 2>&1 | grep -i error
```

Common causes:

- **Syntax errors in SQL** — test your SQL against a local PostgreSQL first
- **Missing `IF EXISTS` / `IF NOT EXISTS`** — migrations must be idempotent
- **Column type conflicts** — check that your `.down.sql` properly reverses the `.up.sql`

### "My events aren't being received"

1. **Subscriber not registered** — ensure you implement `sdk.EventModule` and the kernel detects it
2. **Wrong subject** — event subjects are case-sensitive: `billing.invoice.created` ≠ `billing.Invoice.Created`
3. **Subscriber error** — if your handler returns an error, the event may be retried or dropped depending on the `EventBus` implementation

---

## Summary

Building a kernel module follows a clear pattern:

1. **Implement `sdk.Module`** — fill in `Manifest()`, `Migrations()`, `Init()`, `Shutdown()`, and optionally implement `HttpModule` (via `RouteHandlers()`), `EventModule`, or `HookModule`
2. **Write your migrations** — embedded `.up.sql` files in your module's schema
3. **Build your business logic** — repositories, services, handlers using the `sdk.Context`
4. **Declare permissions** — every route needs one
5. **Use the SDK helpers** — `sdk.OK()`, `sdk.Error()`, `sdk.Paginate()`, `sdk.Cache()`, `sdk.BindAndValidate()`, `sdk.Idempotent()`
6. **Test with `sdk.NewTestContext()`** — in-memory fakes for all infrastructure
7. **Register in the consumer app** — `k.MustRegister(yourmodule.New())`

The kernel handles everything else: authentication, authorization, tenant isolation, database connections, migrations, registry sync, graceful shutdown, and request lifecycle.

**Happy building 🚀**
