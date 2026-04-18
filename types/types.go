// Package types defines the shared domain types for this module.
// It lives in its own sub-package so that reader consumers and other
// modules can import types without creating a cycle back to the parent package.
package types

import (
	"github.com/google/uuid"
	"go.edgescale.dev/kernel/sdk"
)

// ── Item ──────────────────────────────────────────────────────────────────────

// Item is the primary domain model for this module.
// All models embed sdk.BaseModel which provides:
//   - ID        uuid.UUID (primary key)
//   - CreatedAt time.Time
//   - UpdatedAt time.Time
//   - DeletedAt gorm.DeletedAt (soft deletes)
//
// And a method which sets ID if it's nil
//   - func (m *BaseModel) BeforeCreate(_ *gorm.DB) error
type Item struct {
	sdk.BaseModel
	TenantID    uuid.UUID `json:"tenant_id"    gorm:"type:uuid;not null"`
	Name        string    `json:"name"         gorm:"not null"`
	Description *string   `json:"description,omitempty"`
	Status      string    `json:"status"       gorm:"not null;default:active"`
	Metadata    sdk.JSONB `json:"metadata,omitempty" gorm:"type:jsonb"`
}
