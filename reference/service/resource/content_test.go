package resource

import (
	"context"
	"sync/atomic"
	"testing"

	resourcecache "github.com/elum2b/services/reference/service/resource/cache"
	"github.com/elum2b/services/reference/storage"
)

type contentStore struct{ reads atomic.Int32 }

func (s *contentStore) Replace(
	context.Context,
	string,
	string,
	string,
	storage.Files,
) (storage.Objects, error) {
	return storage.Objects{}, nil
}

func (s *contentStore) Read(
	context.Context,
	string,
) ([]byte, error) {
	return nil, nil
}

func (s *contentStore) DeleteVersion(
	context.Context,
	string,
	string,
	string,
) error {
	return nil
}

func (s *contentStore) ReadVersion(
	_ context.Context,
	_, _, version, _ string,
	size int,
) ([]byte, error) {
	s.reads.Add(1)

	return []byte(version + ":" + string(rune(size))), nil
}

func TestGetContentCachesByImmutableVersion(t *testing.T) {
	store := &contentStore{}
	service := &Resource{
		store: store,
		cache: resourcecache.New(resourcecache.Config{}),
	}
	params := ContentParams{
		WorkspaceID: "c2b604c6-6960-41a7-b330-5083ca633434",
		Key:         "logo",
		Version:     "AbCdEfGh",
		Format:      "png",
		Size:        61,
	}

	first, err := service.GetContent(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}

	second, err := service.GetContent(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}

	if store.reads.Load() != 1 || string(first.Data) != string(second.Data) {
		t.Fatalf(
			"reads=%d first=%q second=%q",
			store.reads.Load(),
			first.Data,
			second.Data,
		)
	}

	if first.ContentType != "image/png" {
		t.Fatalf(
			"preview content type = %q, want image/png",
			first.ContentType,
		)
	}

	params.Version = "HgfEdCbA"
	if _, err := service.GetContent(context.Background(), params); err != nil {
		t.Fatal(err)
	}

	if store.reads.Load() != 2 {
		t.Fatalf("new version reused stale cache: reads=%d", store.reads.Load())
	}
}

func TestGetContentRejectsInvalidVariant(t *testing.T) {
	service := &Resource{
		store: &contentStore{},
		cache: resourcecache.New(resourcecache.Config{}),
	}

	_, err := service.GetContent(
		context.Background(),
		ContentParams{
			WorkspaceID: "c2b604c6-6960-41a7-b330-5083ca633434",
			Key:         "logo",
			Version:     "AbCdEfGh",
			Size:        64,
		},
	)
	if err == nil {
		t.Fatal("invalid preview size accepted")
	}
}
