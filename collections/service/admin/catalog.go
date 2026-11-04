package admin

import (
	"context"

	"github.com/elum2b/services/collections/model"
)

func (s *Admin) CreateCollection(ctx context.Context, p model.SaveCollectionParams) error {
	ctx, cancel := s.withContext(ctx)
	defer cancel()
	return s.repository.CreateCollection(ctx, p)
}

func (s *Admin) UpdateCollection(ctx context.Context, p model.SaveCollectionParams) error {
	ctx, cancel := s.withContext(ctx)
	defer cancel()
	return s.repository.UpdateCollection(ctx, p)
}

func (s *Admin) ArchiveCollection(ctx context.Context, p model.ArchiveCollectionParams) error {
	ctx, cancel := s.withContext(ctx)
	defer cancel()
	return s.repository.ArchiveCollection(ctx, p)
}

func (s *Admin) AddEntry(ctx context.Context, p model.EntryParams) error {
	ctx, cancel := s.withContext(ctx)
	defer cancel()
	return s.repository.AddEntry(ctx, p)
}

func (s *Admin) RemoveEntry(ctx context.Context, p model.EntryParams) error {
	ctx, cancel := s.withContext(ctx)
	defer cancel()
	return s.repository.RemoveEntry(ctx, p)
}

func (s *Admin) ReorderEntries(ctx context.Context, p model.ReorderEntriesParams) error {
	ctx, cancel := s.withContext(ctx)
	defer cancel()
	return s.repository.ReorderEntries(ctx, p)
}

func (s *Admin) ListCatalog(ctx context.Context, p model.CatalogParams) ([]model.Collection, error) {
	ctx, cancel := s.withContext(ctx)
	defer cancel()
	return s.repository.ListCatalog(ctx, p)
}

func (s *Admin) GetCollection(ctx context.Context, p model.CatalogParams) (model.Collection, error) {
	ctx, cancel := s.withContext(ctx)
	defer cancel()
	return s.repository.GetCatalogCollection(ctx, p)
}
