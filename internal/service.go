package internal

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"go.edgescale.dev/kernel-contrib/mymodule/types"
	"go.edgescale.dev/kernel/sdk"
)

// Service provides business logic for the module's domain operations.
// Services are the ONLY place where:
//   - Business rules are enforced
//   - Events are published
//   - Cache is invalidated
type Service struct {
	repo  *Repository
	bus   sdk.EventBus
	redis sdk.NamespacedRedis
	log   *slog.Logger
}

// NewService constructs a Service.
func NewService(repo *Repository, bus sdk.EventBus, redis sdk.NamespacedRedis, log *slog.Logger) *Service {
	return &Service{repo: repo, bus: bus, redis: redis, log: log}
}

// ── Query ─────────────────────────────────────────────────────────────────────

// GetByID returns an item by internal UUID.
func (s *Service) GetByID(ctx context.Context, id uuid.UUID) (*types.Item, error) {
	item, err := s.repo.FindItemByID(ctx, id)
	if IsNotFoundErr(err) {
		return nil, sdk.NotFound("item", id)
	}
	return item, err
}

// List returns a paginated list of items for a tenant.
func (s *Service) List(ctx context.Context, tenantID uuid.UUID, page sdk.PageRequest) (*sdk.PageResult[types.Item], error) {
	return s.repo.ListItems(ctx, tenantID, page)
}

// ── Mutations ─────────────────────────────────────────────────────────────────

// CreateItemInput contains the fields for creating a new item.
type CreateItemInput struct {
	TenantID    uuid.UUID
	Name        string
	Description *string
}

// Create inserts a new item and publishes mymodule.item.created.
func (s *Service) Create(ctx context.Context, in CreateItemInput) (*types.Item, error) {
	if in.Name == "" {
		return nil, sdk.BadRequest("name is required")
	}

	item := &types.Item{
		TenantID:    in.TenantID,
		Name:        in.Name,
		Description: in.Description,
	}

	if err := s.repo.CreateItem(ctx, item); err != nil {
		if IsDuplicateError(err) {
			return nil, sdk.Conflict("item with this name already exists")
		}
		return nil, fmt.Errorf("mymodule: create item: %w", err)
	}

	s.publish(ctx, "mymodule.item.created", map[string]any{
		"item_id":   item.ID,
		"tenant_id": item.TenantID,
	})

	return item, nil
}

// UpdateItemInput is a partial update for item fields.
type UpdateItemInput struct {
	Name        *string
	Description *string
}

// Update patches item fields and publishes mymodule.item.updated.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in UpdateItemInput) (*types.Item, error) {
	updates := make(map[string]any)
	if in.Name != nil {
		updates["name"] = *in.Name
	}
	if in.Description != nil {
		updates["description"] = *in.Description
	}
	if len(updates) == 0 {
		return s.repo.FindItemByID(ctx, id)
	}

	item, err := s.repo.UpdateItem(ctx, id, updates)
	if IsNotFoundErr(err) {
		return nil, sdk.NotFound("item", id)
	}
	if err != nil {
		return nil, err
	}

	s.publish(ctx, "mymodule.item.updated", map[string]any{"item_id": id})
	return item, nil
}

// Delete soft-deletes an item and publishes mymodule.item.deleted.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	// Verify existence first.
	if _, err := s.repo.FindItemByID(ctx, id); IsNotFoundErr(err) {
		return sdk.NotFound("item", id)
	}

	if err := s.repo.SoftDeleteItem(ctx, id); err != nil {
		return err
	}

	s.publish(ctx, "mymodule.item.deleted", map[string]any{"item_id": id})
	return nil
}

// ── internal ──────────────────────────────────────────────────────────────────

func (s *Service) publish(ctx context.Context, subject string, payload map[string]any) {
	if s.bus == nil {
		return
	}
	s.bus.Publish(ctx, subject, payload)
}
