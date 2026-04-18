package mymodule_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.edgescale.dev/kernel-contrib/mymodule/internal"
	"go.edgescale.dev/kernel/sdk"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// ── test DB setup ─────────────────────────────────────────────────────────────

// newTestDB opens an in-memory SQLite database and creates the module tables.
// We use raw DDL instead of AutoMigrate because sdk.BaseModel includes
// `default:gen_random_uuid()` which is PostgreSQL-only. UUIDs are generated
// in Go via BeforeCreate hooks.
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err, "open in-memory sqlite")

	ddl := []string{
		`CREATE TABLE items (
			id TEXT PRIMARY KEY,
			created_at DATETIME, updated_at DATETIME, deleted_at DATETIME,
			tenant_id TEXT NOT NULL,
			name TEXT NOT NULL,
			description TEXT,
			status TEXT NOT NULL DEFAULT 'active',
			metadata BLOB
		)`,
	}

	for _, stmt := range ddl {
		require.NoError(t, db.Exec(stmt).Error, "DDL: %s", stmt[:40])
	}
	return db
}

// ── test harness ──────────────────────────────────────────────────────────────

type testHarness struct {
	db   *gorm.DB
	ctx  *sdk.Context
	repo *internal.Repository
	svc  *internal.Service
}

func newTestHarness(t *testing.T) *testHarness {
	t.Helper()
	db := newTestDB(t)
	tctx := sdk.NewTestContext("mymodule")
	tctx.DB = db

	repo := internal.NewRepository(db)
	svc := internal.NewService(repo, tctx.Bus, tctx.Redis, tctx.Logger)

	return &testHarness{
		db:   db,
		ctx:  tctx,
		repo: repo,
		svc:  svc,
	}
}

func (h *testHarness) bus() *sdk.TestBus {
	return h.ctx.Bus.(*sdk.TestBus)
}

// ── error helpers ─────────────────────────────────────────────────────────────

func isNotFound(err error) bool {
	se, ok := sdk.IsServiceError(err)
	return ok && se.HTTPStatus == 404
}

func isBadRequest(err error) bool {
	se, ok := sdk.IsServiceError(err)
	return ok && se.HTTPStatus == 400
}

func isConflict(err error) bool {
	se, ok := sdk.IsServiceError(err)
	return ok && se.HTTPStatus == 409
}

// ═══════════════════════════════════════════════════════════════════════════════
// Item CRUD Tests
// ═══════════════════════════════════════════════════════════════════════════════

func TestItemCreate(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	tenantID := uuid.New()

	item, err := h.svc.Create(ctx, internal.CreateItemInput{
		TenantID: tenantID,
		Name:     "Test Item",
	})
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, item.ID)
	assert.Equal(t, "Test Item", item.Name)
	assert.Equal(t, tenantID, item.TenantID)
	assert.Equal(t, "active", item.Status)

	// Event published.
	events := h.bus().Events()
	require.Len(t, events, 1)
	assert.Equal(t, "mymodule.item.created", events[0].Subject)
}

func TestItemCreate_MissingName(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	_, err := h.svc.Create(ctx, internal.CreateItemInput{
		TenantID: uuid.New(),
		Name:     "",
	})
	assert.True(t, isBadRequest(err), "expected 400, got: %v", err)
}

func TestItemGetByID(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	created, err := h.svc.Create(ctx, internal.CreateItemInput{
		TenantID: uuid.New(),
		Name:     "Findable",
	})
	require.NoError(t, err)

	found, err := h.svc.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, found.ID)
	assert.Equal(t, "Findable", found.Name)
}

func TestItemGetByID_NotFound(t *testing.T) {
	h := newTestHarness(t)
	_, err := h.svc.GetByID(context.Background(), uuid.New())
	assert.True(t, isNotFound(err))
}

func TestItemUpdate(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	created, err := h.svc.Create(ctx, internal.CreateItemInput{
		TenantID: uuid.New(),
		Name:     "Original",
	})
	require.NoError(t, err)

	newName := "Updated"
	updated, err := h.svc.Update(ctx, created.ID, internal.UpdateItemInput{
		Name: &newName,
	})
	require.NoError(t, err)
	assert.Equal(t, "Updated", updated.Name)
}

func TestItemDelete(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	created, err := h.svc.Create(ctx, internal.CreateItemInput{
		TenantID: uuid.New(),
		Name:     "Deletable",
	})
	require.NoError(t, err)

	err = h.svc.Delete(ctx, created.ID)
	require.NoError(t, err)

	// Should not be findable after soft-delete.
	_, err = h.svc.GetByID(ctx, created.ID)
	assert.True(t, isNotFound(err))
}

func TestItemDelete_NotFound(t *testing.T) {
	h := newTestHarness(t)
	err := h.svc.Delete(context.Background(), uuid.New())
	assert.True(t, isNotFound(err))
}

func TestItemList(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	tenantID := uuid.New()

	// Create 3 items.
	for i := 0; i < 3; i++ {
		_, err := h.svc.Create(ctx, internal.CreateItemInput{
			TenantID: tenantID,
			Name:     "Item " + string(rune('A'+i)),
		})
		require.NoError(t, err)
	}

	// List with pagination.
	result, err := h.svc.List(ctx, tenantID, sdk.PageRequest{Page: 1, PerPage: 10})
	require.NoError(t, err)
	assert.Equal(t, int64(3), result.Meta.TotalCount)
	assert.Len(t, result.Items, 3)
}

func TestItemList_TenantIsolation(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	tenantA := uuid.New()
	tenantB := uuid.New()

	_, _ = h.svc.Create(ctx, internal.CreateItemInput{TenantID: tenantA, Name: "A's Item"})
	_, _ = h.svc.Create(ctx, internal.CreateItemInput{TenantID: tenantB, Name: "B's Item"})

	result, err := h.svc.List(ctx, tenantA, sdk.PageRequest{Page: 1, PerPage: 10})
	require.NoError(t, err)
	assert.Equal(t, int64(1), result.Meta.TotalCount)
	assert.Equal(t, "A's Item", result.Items[0].Name)
}
