package admin

import (
	"context"

	"github.com/elum2b/services/collections/model"
)

func (s *Admin) CorrectOwned(ctx context.Context, p model.CorrectOwnedParams) (model.Receipt, error) {
	ctx, cancel := s.withContext(ctx)
	defer cancel()
	return s.repository.CorrectOwned(ctx, p)
}
