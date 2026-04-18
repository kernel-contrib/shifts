package internal

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"go.edgescale.dev/kernel-contrib/mymodule/types"
	"go.edgescale.dev/kernel/sdk"
	"gorm.io/gorm"
)

// Repository is the data-access layer for this module.
// All database interactions happen through this struct.
// Services MUST NOT use *gorm.DB directly — always go through the repository.
type Repository struct {
	db *gorm.DB
}

// NewRepository creates a Repository backed by the provided *gorm.DB.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// ── Items ─────────────────────────────────────────────────────────────────────

// FindItemByID looks up an item by its UUID.
func (r *Repository) FindItemByID(ctx context.Context, id uuid.UUID) (*types.Item, error) {
	var item types.Item
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&item).Error; err != nil {
		return nil, fmt.Errorf("mymodule: find item by id: %w", err)
	}
	return &item, nil
}

// ListItems returns a paginated list of items scoped to a tenant.
func (r *Repository) ListItems(ctx context.Context, tenantID uuid.UUID, page sdk.PageRequest) (*sdk.PageResult[types.Item], error) {
	return sdk.Paginate[types.Item](
		r.db.WithContext(ctx).Model(&types.Item{}).Where("tenant_id = ?", tenantID),
		page,
	)
}

// CreateItem inserts a new item.
func (r *Repository) CreateItem(ctx context.Context, item *types.Item) error {
	if err := r.db.WithContext(ctx).Create(item).Error; err != nil {
		return fmt.Errorf("mymodule: create item: %w", err)
	}
	return nil
}

// UpdateItem patches an item by ID with the provided field updates.
func (r *Repository) UpdateItem(ctx context.Context, id uuid.UUID, updates map[string]any) (*types.Item, error) {
	if err := r.db.WithContext(ctx).
		Model(&types.Item{}).
		Where("id = ?", id).
		Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("mymodule: update item: %w", err)
	}
	return r.FindItemByID(ctx, id)
}

// SoftDeleteItem performs a soft delete on an item.
func (r *Repository) SoftDeleteItem(ctx context.Context, id uuid.UUID) error {
	if err := r.db.WithContext(ctx).Where("id = ?", id).Delete(&types.Item{}).Error; err != nil {
		return fmt.Errorf("mymodule: delete item: %w", err)
	}
	return nil
}
