package resource

import (
	"context"
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
	"time"

	json "github.com/goccy/go-json"

	services "github.com/elum2b/services"
	"github.com/elum2b/services/internal/utils/media"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/elum2b/services/reference/repository"
	"github.com/elum2b/services/reference/storage"
)

var keyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]{0,127}$`)

type Resource struct {
	repository *repository.Repository
	store      storage.Store
	root       context.Context
}
type SaveParams struct {
	WorkspaceID, Key, Type string
	Payload                json.RawMessage
	IsActive               bool
	File                   []byte
}
type GetParams struct{ WorkspaceID, Key string }

func New(
	ctx context.Context,
	db *sqlwrap.Client,
	opts repository.Options,
	store storage.Store,
) *Resource {
	repo, e := repository.NewPreparedWithOptions(ctx, db, opts)
	if e != nil {
		repo = repository.NewWithOptions(db, opts)
	}

	return &Resource{repository: repo, store: store, root: ctx}
}
func (s *Resource) Close() error { return s.repository.Close() }

func (s *Resource) Create(
	ctx context.Context,
	p SaveParams,
) (repository.Resource, error) {
	v, e := s.prepare(ctx, p)
	if e != nil {
		return repository.Resource{}, e
	}

	if e = s.repository.CreateResource(ctx, v); e != nil {
		return repository.Resource{}, e
	}

	return s.repository.GetResource(ctx, v.WorkspaceID, v.Key)
}

func (s *Resource) Update(
	ctx context.Context,
	p SaveParams,
) (repository.Resource, error) {
	v, e := s.prepare(ctx, p)
	if e != nil {
		return repository.Resource{}, e
	}

	_, e = s.repository.UpdateResource(ctx, v)
	if e != nil {
		return repository.Resource{}, e
	}

	return s.repository.GetResource(ctx, v.WorkspaceID, v.Key)
}

func (s *Resource) Get(
	ctx context.Context,
	p GetParams,
) (repository.Resource, error) {
	return s.repository.GetResource(
		ctx,
		p.WorkspaceID,
		strings.ToLower(strings.TrimSpace(p.Key)),
	)
}
func (s *Resource) Delete(ctx context.Context, p GetParams) error {
	_, e := s.repository.SoftDeleteResource(
		ctx,
		p.WorkspaceID,
		strings.ToLower(strings.TrimSpace(p.Key)),
	)

	return e
}

func (s *Resource) Attach(
	ctx context.Context,
	workspaceID, itemKey, resourceKey string,
	position int32,
) error {
	return s.repository.AttachResource(
		ctx,
		workspaceID,
		strings.ToLower(strings.TrimSpace(itemKey)),
		strings.ToLower(strings.TrimSpace(resourceKey)),
		position,
	)
}

func (s *Resource) prepare(
	ctx context.Context,
	p SaveParams,
) (repository.Resource, error) {
	p.Key = strings.ToLower(strings.TrimSpace(p.Key))
	if e := services.ValidateWorkspaceID(p.WorkspaceID); e != nil {
		return repository.Resource{}, e
	}

	if !keyPattern.MatchString(p.Key) || p.Type == "" ||
		!json.Valid(p.Payload) ||
		s.store == nil {
		return repository.Resource{}, fmt.Errorf("invalid resource")
	}

	a, e := media.Process(ctx, p.File, media.Options{})
	if e != nil {
		return repository.Resource{}, e
	}

	files := storage.Files{
		Original: storage.File{
			Data:        a.Original,
			ContentType: contentType(a.Format),
		},
		Placeholder: storage.File{
			Data:        a.Placeholder,
			ContentType: "image/svg+xml",
		},
	}
	for _, x := range a.Previews {
		files.Previews = append(
			files.Previews,
			storage.Preview{
				Size: x.Size,
				File: storage.File{Data: x.PNG, ContentType: "image/png"},
			},
		)
	}

	o, e := s.store.Replace(ctx, p.WorkspaceID, p.Key, files)
	if e != nil {
		return repository.Resource{}, e
	}

	h := sha256.Sum256(a.Original)

	return repository.Resource{
		WorkspaceID:    p.WorkspaceID,
		Key:            p.Key,
		Type:           p.Type,
		Payload:        p.Payload,
		IsActive:       p.IsActive,
		Format:         string(a.Format),
		ContentType:    contentType(a.Format),
		Size:           int64(len(a.Original)),
		SHA256:         fmt.Sprintf("%x", h),
		Width:          a.Width,
		Height:         a.Height,
		OriginalRef:    o.Original,
		Preview61Ref:   o.Previews[61],
		Preview128Ref:  o.Previews[128],
		Preview256Ref:  o.Previews[256],
		Preview512Ref:  o.Previews[512],
		PlaceholderRef: o.Placeholder,
		CreatedAt:      time.Now(),
	}, nil
}
func contentType(f media.Format) string {
	switch f {
	case media.FormatLottie:
		return "application/json"
	case media.FormatRive:
		return "application/octet-stream"
	case media.FormatWebP:
		return "image/webp"
	case media.FormatJPEG:
		return "image/jpeg"
	case media.FormatGIF:
		return "image/gif"
	default:
		return "image/png"
	}
}
