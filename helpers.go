package mymodule

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.edgescale.dev/kernel/sdk"
)

// ── UUID parsing ──────────────────────────────────────────────────────────────

// parseUUID extracts and validates a UUID route parameter.
func parseUUID(c *gin.Context, param string) (uuid.UUID, error) {
	raw := c.Param(param)
	id, err := uuid.Parse(raw)
	if err != nil {
		sdk.Error(c, sdk.BadRequest(fmt.Sprintf("invalid %s: must be a UUID", param)))
		return uuid.Nil, err
	}
	return id, nil
}

// ── Context helpers ───────────────────────────────────────────────────────────
// These centralize context key extraction so changes to the kernel's
// context keys only require updates in one place.

// tenantID extracts the tenant UUID from the gin context.
// Set by the kernel's tenant middleware for tenant-scoped routes.
func tenantID(c *gin.Context) uuid.UUID {
	return get(c, "tenant_id")
}

// userID extracts the authenticated user's UUID from the gin context.
// Set by the kernel's auth middleware.
func userID(c *gin.Context) uuid.UUID {
	return get(c, "internal_user_id")
}

func get(c *gin.Context, key string) uuid.UUID {
	_id, ok := c.Get(key)
	if !ok {
		return uuid.Nil
	}

	id, ok := _id.(uuid.UUID)
	if !ok {
		return uuid.Nil
	}
	return id
}
