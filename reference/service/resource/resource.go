package resource

import (
	"context"
	"crypto/rand"
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
	resourcecache "github.com/elum2b/services/reference/service/resource/cache"
	"github.com/elum2b/services/reference/storage"
)

var (
	keyPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]{0,127}$`)
	versionPattern = regexp.MustCompile(`^[A-Za-z]{8}$`)
)

type Resource struct {
	repository  *repository.Repository
	store       storage.Store
	cache       *resourcecache.Cache
	root        context.Context
	gcTrigger   chan<- struct{}
	gcRetention time.Duration
}
type SaveParams struct {
	WorkspaceID, Key, Type string
	Payload                json.RawMessage
	IsActive               bool
	File                   []byte
	FirstFrame             []byte
}
type GetParams struct{ WorkspaceID, Key string }
type ListParams struct {
	WorkspaceID   string
	Limit, Offset int32
}
type ContentParams struct {
	WorkspaceID, Key, Version, Format string
	Size                              int
}
type Content struct {
	Data        []byte
	ContentType string
}
type CollectGarbageParams struct{ Limit int32 }

func New(
	ctx context.Context,
	db *sqlwrap.Client,
	opts repository.Options,
	store storage.Store,
	cache *resourcecache.Cache,
	gcTrigger chan<- struct{},
	gcRetention time.Duration,
) *Resource {
	repo, e := repository.NewPreparedWithOptions(ctx, db, opts)
	if e != nil {
		repo = repository.NewWithOptions(db, opts)
	}

	return &Resource{
		repository:  repo,
		store:       store,
		cache:       cache,
		root:        ctx,
		gcTrigger:   gcTrigger,
		gcRetention: gcRetention,
	}
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

	if s.gcTrigger != nil {
		select {
		case s.gcTrigger <- struct{}{}:
		default:
		}
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

	if e == nil && s.gcTrigger != nil {
		select {
		case s.gcTrigger <- struct{}{}:
		default:
		}
	}

	return e
}

// CollectGarbage removes media for soft-deleted resources before purging their rows.
func (s *Resource) CollectGarbage(
	ctx context.Context,
	p CollectGarbageParams,
) (int, error) {
	if s.store == nil || p.Limit <= 0 {
		return 0, nil
	}

	before := time.Now().Add(-s.gcRetention)

	resources, err := s.repository.ListGarbageMediaVersions(
		ctx,
		before,
		p.Limit,
	)
	if err != nil {
		return 0, err
	}

	purged := 0

	for _, resource := range resources {
		if err := s.store.DeleteVersion(
			ctx,
			resource.WorkspaceID,
			resource.ResourceKey,
			resource.MediaVersion,
		); err != nil {
			return purged, err
		}

		rows, err := s.repository.DeleteMediaVersion(ctx, resource, before)
		if err != nil {
			return purged, err
		}

		if rows == 0 {
			continue
		}

		purged++

		_, err = s.repository.PurgeDeletedResource(
			ctx,
			resource.WorkspaceID,
			resource.ResourceKey,
		)
		if err != nil {
			return purged, err
		}
	}

	return purged, nil
}

// GetContent returns original media when Size is zero, otherwise a WebP preview.
// Version is part of the public media identity, so old and deleted versions stay readable.
func (s *Resource) GetContent(
	ctx context.Context,
	p ContentParams,
) (Content, error) {
	p.Key = strings.ToLower(strings.TrimSpace(p.Key))
	if err := services.ValidateWorkspaceID(
		p.WorkspaceID,
	); err != nil || !keyPattern.MatchString(p.Key) ||
		!versionPattern.MatchString(p.Version) || s.store == nil ||
		s.cache == nil {
		return Content{}, fmt.Errorf("invalid resource content request")
	}

	if p.Size != 0 && p.Size != 61 && p.Size != 128 && p.Size != 256 &&
		p.Size != 512 {
		return Content{}, fmt.Errorf("invalid resource preview size")
	}

	if p.Size != 0 && p.Format == string(media.FormatSVG) {
		return Content{}, fmt.Errorf("SVG resources have no previews")
	}

	mimeType := "image/webp"
	originalName := ""

	if p.Size == 0 {
		mimeType = contentTypeForFormat(p.Format)

		var ok bool

		originalName, ok = storage.OriginalName(p.Format)

		if mimeType == "" || !ok {
			return Content{}, fmt.Errorf("invalid resource format")
		}
	}

	key := strings.Join(
		[]string{
			"reference",
			"media",
			p.WorkspaceID,
			p.Key,
			p.Version,
			fmt.Sprint(p.Size),
			mimeType,
		},
		":",
	)

	value, err := s.cache.GetOrLoad(key, func() (resourcecache.Value, error) {
		data, err := s.store.ReadVersion(
			ctx,
			p.WorkspaceID,
			p.Key,
			p.Version,
			originalName,
			p.Size,
		)

		return resourcecache.Value{Data: data, ContentType: mimeType}, err
	})
	if err != nil {
		return Content{}, err
	}

	return Content{Data: value.Data, ContentType: value.ContentType}, nil
}

func (s *Resource) List(
	ctx context.Context,
	p ListParams,
) ([]repository.Resource, error) {
	if err := services.ValidateWorkspaceID(p.WorkspaceID); err != nil {
		return nil, err
	}

	if p.Limit <= 0 {
		p.Limit = 100
	}

	if p.Limit > 1000 {
		p.Limit = 1000
	}

	if p.Offset < 0 {
		p.Offset = 0
	}

	return s.repository.ListResources(
		ctx,
		p.WorkspaceID,
		p.Limit,
		p.Offset,
	)
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

func (s *Resource) Detach(
	ctx context.Context,
	workspaceID, itemKey, resourceKey string,
) (int64, error) {
	return s.repository.DetachResource(
		ctx,
		workspaceID,
		strings.ToLower(strings.TrimSpace(itemKey)),
		strings.ToLower(strings.TrimSpace(resourceKey)),
	)
}

func (s *Resource) ListItemResources(
	ctx context.Context,
	workspaceID, itemKey string,
) ([]repository.Resource, error) {
	return s.repository.ListItemResources(
		ctx,
		workspaceID,
		strings.ToLower(strings.TrimSpace(itemKey)),
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

	options := media.Options{FirstFrame: p.FirstFrame}

	a, e := media.Process(ctx, p.File, options)
	if e != nil {
		return repository.Resource{}, e
	}

	originalName, ok := storage.OriginalName(string(a.Format))
	if !ok {
		return repository.Resource{}, fmt.Errorf(
			"unsupported resource format %q",
			a.Format,
		)
	}

	files := storage.Files{
		OriginalName: originalName,
		Original: storage.File{
			Data:        a.Original,
			ContentType: contentType(a.Format),
		},
		Placeholder: storage.File{
			Data:        a.Placeholder,
			ContentType: "image/svg+xml",
		},
		NoPreviews: a.Format == media.FormatSVG,
	}
	for _, x := range a.Previews {
		files.Previews = append(
			files.Previews,
			storage.Preview{
				Size: x.Size,
				File: storage.File{Data: x.WebP, ContentType: "image/webp"},
			},
		)
	}

	version, e := mediaVersion()
	if e != nil {
		return repository.Resource{}, e
	}

	o, e := s.store.Replace(ctx, p.WorkspaceID, p.Key, version, files)
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
		MediaVersion:   version,
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
	case media.FormatTGS:
		return "application/gzip"
	case media.FormatSVG:
		return "image/svg+xml"
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

func contentTypeForFormat(format string) string {
	switch media.Format(format) {
	case media.FormatLottie:
		return "application/json"
	case media.FormatTGS:
		return "application/gzip"
	case media.FormatSVG:
		return "image/svg+xml"
	case media.FormatWebP:
		return "image/webp"
	case media.FormatJPEG:
		return "image/jpeg"
	case media.FormatGIF:
		return "image/gif"
	case media.FormatPNG:
		return "image/png"
	default:
		return ""
	}
}

func mediaVersion() (string, error) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

	bytes := make([]byte, 8)

	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate media version: %w", err)
	}

	for i := range bytes {
		bytes[i] = alphabet[int(bytes[i])%len(alphabet)]
	}

	return string(bytes), nil
}
