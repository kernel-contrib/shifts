package mymodule

import (
	"github.com/gin-gonic/gin"
	"go.edgescale.dev/kernel-contrib/mymodule/internal"
	"go.edgescale.dev/kernel/sdk"
)

// ── Request types ─────────────────────────────────────────────────────────────

type createItemRequest struct {
	Name        string  `json:"name" binding:"required"`
	Description *string `json:"description"`
}

type updateItemRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

// ── Handlers ──────────────────────────────────────────────────────────────────

// handleListItems returns a paginated list of items for the current tenant.
func (m *Module) handleListItems(c *gin.Context) {
	tid := tenantID(c)
	page := sdk.ParsePageRequest(c)

	result, err := m.svc.List(c.Request.Context(), tid, page)
	if err != nil {
		sdk.FromError(c, err)
		return
	}

	sdk.List(c, result.Items, result.Meta)
}

// handleCreateItem creates a new item in the current tenant.
func (m *Module) handleCreateItem(c *gin.Context) {
	tid := tenantID(c)

	var req createItemRequest
	if !sdk.BindAndValidate(c, &req) {
		return
	}

	item, err := m.svc.Create(c.Request.Context(), internal.CreateItemInput{
		TenantID:    tid,
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		sdk.FromError(c, err)
		return
	}

	// Audit the creation.
	m.ctx.Audit.Log(c.Request.Context(), sdk.AuditEntry{
		Action:     sdk.AuditCreate,
		Resource:   "item",
		ResourceID: item.ID.String(),
	})

	sdk.Created(c, item)
}

// handleGetItem returns a single item by ID.
func (m *Module) handleGetItem(c *gin.Context) {
	id, err := parseUUID(c, "id")
	if err != nil {
		return
	}

	item, err := m.svc.GetByID(c.Request.Context(), id)
	if err != nil {
		sdk.FromError(c, err)
		return
	}

	sdk.OK(c, item)
}

// handleUpdateItem patches an existing item.
func (m *Module) handleUpdateItem(c *gin.Context) {
	id, err := parseUUID(c, "id")
	if err != nil {
		return
	}

	var req updateItemRequest
	if !sdk.BindAndValidate(c, &req) {
		return
	}

	item, err := m.svc.Update(c.Request.Context(), id, internal.UpdateItemInput{
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		sdk.FromError(c, err)
		return
	}

	m.ctx.Audit.Log(c.Request.Context(), sdk.AuditEntry{
		Action:     sdk.AuditUpdate,
		Resource:   "item",
		ResourceID: id.String(),
	})

	sdk.OK(c, item)
}

// handleDeleteItem soft-deletes an item.
func (m *Module) handleDeleteItem(c *gin.Context) {
	id, err := parseUUID(c, "id")
	if err != nil {
		return
	}

	if err := m.svc.Delete(c.Request.Context(), id); err != nil {
		sdk.FromError(c, err)
		return
	}

	m.ctx.Audit.Log(c.Request.Context(), sdk.AuditEntry{
		Action:     sdk.AuditDelete,
		Resource:   "item",
		ResourceID: id.String(),
	})

	sdk.NoContent(c)
}
