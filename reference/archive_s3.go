package reference

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/elum2b/services/internal/utils/importexport/jobs"
	resourcestorage "github.com/elum2b/services/reference/storage"
)

const archivePrefix = "importexport/"

type archiveObjectClient interface {
	PutObject(
		context.Context,
		*awss3.PutObjectInput,
		...func(*awss3.Options),
	) (*awss3.PutObjectOutput, error)
	GetObject(
		context.Context,
		*awss3.GetObjectInput,
		...func(*awss3.Options),
	) (*awss3.GetObjectOutput, error)
	DeleteObject(
		context.Context,
		*awss3.DeleteObjectInput,
		...func(*awss3.Options),
	) (*awss3.DeleteObjectOutput, error)
}

type s3Archive struct {
	client archiveObjectClient
	bucket string
}

func newS3Archive(config resourcestorage.Config) (*s3Archive, error) {
	client, err := resourcestorage.NewS3Client(config)
	if err != nil {
		return nil, err
	}

	return newS3ArchiveWithClient(client, config.Bucket)
}

func newS3ArchiveWithClient(
	client archiveObjectClient,
	bucket string,
) (*s3Archive, error) {
	if client == nil || strings.TrimSpace(bucket) == "" {
		return nil, resourcestorage.ErrConfigInvalid
	}

	return &s3Archive{client: client, bucket: bucket}, nil
}

func (a *s3Archive) Store(
	ctx context.Context,
	_ jobs.ArchiveObject,
	source io.Reader,
) (string, error) {
	if a == nil || source == nil {
		return "", fmt.Errorf("reference archive source is required")
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}

	key, err := newArchiveKey()
	if err != nil {
		return "", err
	}

	_, err = a.client.PutObject(ctx, &awss3.PutObjectInput{
		Bucket:      aws.String(a.bucket),
		Key:         aws.String(key),
		Body:        &archiveContextReader{ctx: ctx, reader: source},
		ContentType: aws.String("application/zip"),
	})
	if err != nil {
		return "", fmt.Errorf("store reference archive: %w", err)
	}

	return key, nil
}

func (a *s3Archive) Open(
	ctx context.Context,
	key string,
) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if !validArchiveKey(key) {
		return nil, fmt.Errorf("invalid reference archive key")
	}

	output, err := a.client.GetObject(
		ctx,
		&awss3.GetObjectInput{
			Bucket: aws.String(a.bucket),
			Key:    aws.String(key),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("open reference archive: %w", err)
	}

	return output.Body, nil
}

func (a *s3Archive) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if !validArchiveKey(key) {
		return fmt.Errorf("invalid reference archive key")
	}

	if _, err := a.client.DeleteObject(
		ctx,
		&awss3.DeleteObjectInput{
			Bucket: aws.String(a.bucket),
			Key:    aws.String(key),
		},
	); err != nil {
		return fmt.Errorf("delete reference archive: %w", err)
	}

	return nil
}

func newArchiveKey() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate reference archive key: %w", err)
	}

	return archivePrefix + hex.EncodeToString(value) + ".zip", nil
}

func validArchiveKey(key string) bool {
	name, ok := strings.CutPrefix(key, archivePrefix)
	if !ok || len(name) != 36 || !strings.HasSuffix(name, ".zip") {
		return false
	}

	_, err := hex.DecodeString(strings.TrimSuffix(name, ".zip"))

	return err == nil
}

type archiveContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *archiveContextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}

	return r.reader.Read(data)
}
