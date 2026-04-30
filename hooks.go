package shifts

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"go.edgescale.dev/kernel/sdk"
)

// RegisterHooks subscribes to kernel lifecycle hooks.
func (m *Module) RegisterHooks(hooks *sdk.HookRegistry) {
	hooks.After("after.kernel.tenant.provisioned", m.onTenantProvisioned)
}

// ── Hook handlers ─────────────────────────────────────────────────────────────

// tenantProvisionedPayload is the expected shape of the kernel's provisioning event.
type tenantProvisionedPayload struct {
	TenantID uuid.UUID `json:"tenant_id"`
	UserID   uuid.UUID `json:"user_id"`
}

// onTenantProvisioned seeds default configuration when a new tenant is provisioned.
func (m *Module) onTenantProvisioned(ctx context.Context, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("shifts: marshal hook payload: %w", err)
	}

	var p tenantProvisionedPayload
	if err := json.Unmarshal(data, &p); err != nil {
		return fmt.Errorf("shifts: unmarshal hook payload: %w", err)
	}

	if p.TenantID == uuid.Nil {
		return fmt.Errorf("shifts: tenant.provisioned hook: missing tenant_id")
	}

	m.ctx.Logger.Info("shifts: seeding defaults for new tenant",
		"tenant_id", p.TenantID,
	)

	// Default configuration is handled by the Manifest Config defaults.
	// No additional seeding needed at this time.

	return nil
}
