package internalapi

import (
	"context"

	"github.com/elum2b/services/collections/model"
)

func (s *Internal) SetOwned(ctx context.Context, p model.SetOwnedParams) (model.Receipt, error) {
	ctx, cancel := s.withContext(ctx)
	defer cancel()
	return s.repository.SetOwned(ctx, p)
}
func (s *Internal) SetOwnedBatch(ctx context.Context, p model.SetOwnedBatchParams) (model.Receipt, error) {
	ctx, cancel := s.withContext(ctx)
	defer cancel()
	return s.repository.SetOwnedBatch(ctx, p)
}
