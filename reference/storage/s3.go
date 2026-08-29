package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	awstypes "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type objectClient interface {
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
	DeleteObjects(
		context.Context,
		*awss3.DeleteObjectsInput,
		...func(*awss3.Options),
	) (*awss3.DeleteObjectsOutput, error)
}

type s3Store struct {
	client objectClient
	bucket string
}

func newS3(config Config) (*s3Store, error) {
	client, err := NewS3Client(config)
	if err != nil {
		return nil, err
	}

	return newS3WithClient(client, config.Bucket)
}

// NewS3Client builds the S3-compatible client shared by Reference storage.
func NewS3Client(config Config) (*awss3.Client, error) {
	if strings.TrimSpace(config.Bucket) == "" ||
		(config.AccessKey == "") != (config.SecretKey == "") {
		return nil, ErrConfigInvalid
	}

	region := config.Region
	if region == "" {
		region = "us-east-1"
	}

	options := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(region),
	}
	if config.AccessKey != "" {
		options = append(
			options,
			awsconfig.WithCredentialsProvider(
				credentials.NewStaticCredentialsProvider(
					config.AccessKey,
					config.SecretKey,
					config.SessionToken,
				),
			),
		)
	}

	awsConfig, err := awsconfig.LoadDefaultConfig(
		context.Background(),
		options...)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrConfigInvalid, err)
	}

	return awss3.NewFromConfig(awsConfig, func(options *awss3.Options) {
		options.UsePathStyle = config.UsePathStyle || config.Endpoint != ""
		if config.Endpoint != "" {
			options.BaseEndpoint = aws.String(
				s3Endpoint(config.Endpoint, config.Secure),
			)
		}
	}), nil
}

func newS3WithClient(client objectClient, bucket string) (*s3Store, error) {
	if client == nil || strings.TrimSpace(bucket) == "" {
		return nil, ErrConfigInvalid
	}

	return &s3Store{client: client, bucket: bucket}, nil
}

func (s *s3Store) Replace(
	ctx context.Context,
	workspaceID, resourceKey, version string,
	files Files,
) (Objects, error) {
	if err := validateFiles(files); err != nil {
		return Objects{}, err
	}

	prefix, err := objectPrefix(workspaceID, resourceKey, version)
	if err != nil {
		return Objects{}, err
	}

	prefix = "reference/" + prefix

	result := Objects{Previews: make(map[int]string, len(files.Previews))}

	if result.Original, err = s.put(
		ctx,
		prefix+"/"+files.OriginalName,
		files.Original,
	); err != nil {
		return Objects{}, err
	}

	for _, preview := range files.Previews {
		ref, err := s.put(
			ctx,
			fmt.Sprintf("%s/preview-%d.webp", prefix, preview.Size),
			preview.File,
		)
		if err != nil {
			return Objects{}, err
		}

		result.Previews[preview.Size] = ref
	}

	result.Placeholder, err = s.put(
		ctx,
		prefix+"/placeholder.svg",
		files.Placeholder,
	)
	if err != nil {
		return Objects{}, err
	}

	return result, nil
}

func (s *s3Store) Read(ctx context.Context, reference string) ([]byte, error) {
	output, err := s.client.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(reference),
	})
	if err != nil {
		return nil, fmt.Errorf("read resource object: %w", err)
	}
	defer output.Body.Close()

	data, err := io.ReadAll(output.Body)
	if err != nil {
		return nil, fmt.Errorf("read resource object body: %w", err)
	}

	return data, nil
}

func (s *s3Store) ReadVersion(
	ctx context.Context,
	workspaceID, resourceKey, version, originalName string,
	size int,
) ([]byte, error) {
	reference, err := versionReference(
		workspaceID,
		resourceKey,
		version,
		originalName,
		size,
	)
	if err != nil {
		return nil, err
	}

	return s.Read(ctx, "reference/"+reference)
}

func (s *s3Store) DeleteVersion(
	ctx context.Context,
	workspaceID, resourceKey, version string,
) error {
	prefix, err := objectPrefix(workspaceID, resourceKey, version)
	if err != nil {
		return err
	}

	objects := make(
		[]awstypes.ObjectIdentifier,
		0,
		len(originalNames)+len(requiredPreviewSizes)+1,
	)
	for _, name := range originalNames {
		objects = append(objects, awstypes.ObjectIdentifier{
			Key: aws.String("reference/" + prefix + "/" + name),
		})
	}

	for _, suffix := range []string{
		"placeholder.svg",
		"preview-61.webp",
		"preview-128.webp",
		"preview-256.webp",
		"preview-512.webp",
	} {
		objects = append(objects, awstypes.ObjectIdentifier{
			Key: aws.String("reference/" + prefix + "/" + suffix),
		})
	}

	output, err := s.client.DeleteObjects(ctx, &awss3.DeleteObjectsInput{
		Bucket: aws.String(s.bucket),
		Delete: &awstypes.Delete{Objects: objects, Quiet: aws.Bool(true)},
	})
	if err != nil {
		return fmt.Errorf("delete resource media version: %w", err)
	}

	if len(output.Errors) > 0 {
		return fmt.Errorf(
			"delete resource media version: %d objects failed",
			len(output.Errors),
		)
	}

	return nil
}

func (s *s3Store) put(
	ctx context.Context,
	key string,
	file File,
) (string, error) {
	_, err := s.client.PutObject(ctx, &awss3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          bytes.NewReader(file.Data),
		ContentType:   aws.String(file.ContentType),
		ContentLength: aws.Int64(int64(len(file.Data))),
	})
	if err != nil {
		return "", fmt.Errorf("upload resource object: %w", err)
	}

	return key, nil
}

func s3Endpoint(endpoint string, secure bool) string {
	if strings.HasPrefix(endpoint, "http://") ||
		strings.HasPrefix(endpoint, "https://") {
		return endpoint
	}

	if secure {
		return "https://" + endpoint
	}

	return "http://" + endpoint
}
