package admin

import (
	"context"

	"github.com/elum2b/services/collections/model"
)

func (s *Admin) ListJournal(ctx context.Context, p model.JournalParams) ([]model.JournalEntry, error) {
	ctx, cancel := s.withContext(ctx)
	defer cancel()
	return s.repository.ListJournal(ctx, p)
}
