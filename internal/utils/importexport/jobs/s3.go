package jobs

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Options supports AWS S3 and MinIO. Omitted credentials use the AWS provider
// chain. Prefix must be dedicated to this service's archive jobs.
type S3Options struct {
	Bucket          string
	Region          string
	Endpoint        string
	Prefix          string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	UsePathStyle    bool
}

type S3Archive struct {
	client         *s3.Client
	bucket, prefix string
}

func NewS3Archive(ctx context.Context, options S3Options) (*S3Archive, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if options.Bucket == "" {
		return nil, fmt.Errorf("archive S3 bucket is required")
	}
	if options.Region == "" {
		options.Region = "us-east-1"
	}
	if options.Prefix == "" {
		options.Prefix = "cpa/importexport/"
	}
	if strings.HasPrefix(options.Prefix, "/") || strings.Contains(options.Prefix, "..") {
		return nil, fmt.Errorf("invalid archive S3 prefix")
	}
	load := []func(*config.LoadOptions) error{config.WithRegion(options.Region)}
	if options.AccessKeyID != "" || options.SecretAccessKey != "" {
		if options.AccessKeyID == "" || options.SecretAccessKey == "" {
			return nil, fmt.Errorf("both archive S3 credentials are required")
		}
		load = append(load, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				options.AccessKeyID,
				options.SecretAccessKey,
				options.SessionToken,
			),
		))
	}
	cfg, err := config.LoadDefaultConfig(ctx, load...)
	if err != nil {
		return nil, fmt.Errorf("load archive S3 configuration: %w", err)
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = options.UsePathStyle
		if options.Endpoint != "" {
			o.BaseEndpoint = aws.String(options.Endpoint)
		}
	})
	if _, err := client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(options.Bucket)}); err != nil {
		return nil, fmt.Errorf("check archive S3 bucket: %w", err)
	}
	return &S3Archive{client: client, bucket: options.Bucket, prefix: strings.TrimSuffix(options.Prefix, "/") + "/"}, nil
}

func (a *S3Archive) objectKey(key string) (string, error) {
	if key == "" || path.Base(key) != key || path.Ext(key) != ".zip" {
		return "", fmt.Errorf("invalid archive S3 key")
	}
	return a.prefix + key, nil
}

func (a *S3Archive) Store(ctx context.Context, _ ArchiveObject, source io.Reader) (string, error) {
	// A seekable file permits SDK signing/retries without buffering the object.
	f, err := NewStagingFile("importexport-s3-*.zip")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := copyUpload(ctx, f, source, DefaultMaxUploadBytes); err != nil {
		return "", err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	key := newToken() + ".zip"
	_, err = a.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(a.bucket),
		Key:         aws.String(a.prefix + key),
		Body:        f,
		ContentType: aws.String("application/zip"),
	})
	return key, err
}

func (a *S3Archive) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	object, err := a.objectKey(key)
	if err != nil {
		return nil, err
	}
	response, err := a.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(a.bucket), Key: aws.String(object)})
	if err != nil {
		return nil, err
	}
	return response.Body, nil
}

func (a *S3Archive) Delete(ctx context.Context, key string) error {
	object, err := a.objectKey(key)
	if err != nil {
		return err
	}
	_, err = a.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(a.bucket), Key: aws.String(object)})
	return err
}

func (a *S3Archive) List(ctx context.Context) ([]ArchiveInfo, error) {
	pages := s3.NewListObjectsV2Paginator(a.client, &s3.ListObjectsV2Input{Bucket: aws.String(a.bucket), Prefix: aws.String(a.prefix)})
	result := []ArchiveInfo{}
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, object := range page.Contents {
			key := strings.TrimPrefix(aws.ToString(object.Key), a.prefix)
			if _, err := a.objectKey(key); err != nil || object.LastModified == nil {
				continue
			}
			result = append(result, ArchiveInfo{Key: key, CreatedAt: *object.LastModified})
		}
	}
	return result, nil
}
