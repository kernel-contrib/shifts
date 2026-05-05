package shifts

import (
	"context"
	"fmt"
	"time"

	"github.com/edgescaleDev/kernel/sdk"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	iamtypes "github.com/kernel-contrib/iam/types"
)

// ── Context helpers ───────────────────────────────────────────────────────────

// tenantID extracts the tenant UUID from the Gin context.
// Set by the kernel's resolveTenant middleware on tenant-scoped routes.
func tenantID(c *gin.Context) uuid.UUID {
	return c.MustGet("tenant_id").(uuid.UUID)
}

// parseUUID extracts a UUID from a URL parameter. Returns an error response
// and a zero UUID if parsing fails.
func parseUUID(c *gin.Context, param string) (uuid.UUID, error) {
	raw := c.Param(param)
	id, err := uuid.Parse(raw)
	if err != nil {
		sdk.Error(c, sdk.BadRequest(fmt.Sprintf("invalid UUID for %s: %s", param, raw)))
		return uuid.Nil, err
	}
	return id, nil
}

// parseDate parses a "YYYY-MM-DD" date string.
func parseDate(s string) (time.Time, error) {
	return time.Parse("2006-01-02", s)
}

// ── IAM Reader interface ──────────────────────────────────────────────────────

// iamMemberReader is the expected interface from the IAM module's reader.
// We define it locally to avoid importing the root IAM module directly.
// Only iam/types is imported (shared structs, no cycle risk).
type iamMemberReader interface {
	GetMembersByIDs(ctx context.Context, tenantID uuid.UUID, memberIDs []uuid.UUID) (map[uuid.UUID]iamtypes.TenantMember, error)
}
