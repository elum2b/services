package user

import (
	"context"

	"github.com/elum2b/services/collections/model"
)

func (s *User) HasItem(ctx context.Context, p model.HasItemParams) (model.ItemState, error) {
	ctx, cancel := s.withContext(ctx)
	defer cancel()
	return s.repository.HasItem(ctx, p)
}
func (s *User) HasItems(ctx context.Context, p model.HasItemsParams) ([]model.ItemState, error) {
	ctx, cancel := s.withContext(ctx)
	defer cancel()
	return s.repository.HasItems(ctx, p)
}
func (s *User) ListOwned(ctx context.Context, p model.ListOwnedParams) ([]model.ItemState, error) {
	ctx, cancel := s.withContext(ctx)
	defer cancel()
	return s.repository.ListOwned(ctx, p)
}
func (s *User) ListCollections(ctx context.Context, p model.ReadParams) ([]model.CollectionSummary, error) {
	ctx, cancel := s.withContext(ctx)
	defer cancel()
	return s.repository.ListCollections(ctx, p)
}
func (s *User) GetCollection(ctx context.Context, p model.GetCollectionParams) (model.CollectionView, error) {
	ctx, cancel := s.withContext(ctx)
	defer cancel()
	return s.repository.GetCollection(ctx, p)
}
