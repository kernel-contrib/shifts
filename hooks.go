package mymodule

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"go.edgescale.dev/kernel/sdk"
)

// RegisterHooks subscribes to kernel lifecycle hooks.
//
// Hook naming convention:
//   - "before.<module>.<resource>.<action>" — fired before an action, can abort
//   - "after.<module>.<resource>.<action>"  — fired after an action, informational
//
// Common kernel hooks to subscribe to:
//   - "after.kernel.tenant.provisioned" — seed data when a new tenant is provisioned
//   - "before.kernel.tenant.deleted"    — guard or cleanup before tenant deletion
//
// Your module can also EMIT hooks so other modules can intercept your operations:
//   - "before.mymodule.item.deleted"  — allow other modules to block deletion
//   - "after.mymodule.item.created"   — notify other modules after creation
func (m *Module) RegisterHooks(hooks *sdk.HookRegistry) {
	// Subscribe to kernel tenant provisioning to seed initial data.
	hooks.After("after.kernel.tenant.provisioned", m.onTenantProvisioned)
}

// ── Hook handlers ─────────────────────────────────────────────────────────────

// tenantProvisionedPayload is the expected shape of the kernel's provisioning event.
type tenantProvisionedPayload struct {
	TenantID uuid.UUID `json:"tenant_id"`
	UserID   uuid.UUID `json:"user_id"`
}

// onTenantProvisioned seeds initial data when the kernel provisions a new tenant.
func (m *Module) onTenantProvisioned(ctx context.Context, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("mymodule: marshal hook payload: %w", err)
	}

	var p tenantProvisionedPayload
	if err := json.Unmarshal(data, &p); err != nil {
		return fmt.Errorf("mymodule: unmarshal hook payload: %w", err)
	}

	if p.TenantID == uuid.Nil {
		return fmt.Errorf("mymodule: tenant.provisioned hook: missing tenant_id")
	}

	m.ctx.Logger.Info("seeding initial data for new tenant",
		"tenant_id", p.TenantID,
	)

	// TODO: Add your tenant provisioning logic here.
	// Example: create default items, seed configuration, etc.

	return nil
}
