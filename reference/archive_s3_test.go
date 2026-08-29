package reference

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/elum2b/services/internal/utils/importexport/jobs"
)

func TestS3ArchiveStoreOpenDelete(t *testing.T) {
	client := &fakeArchiveClient{objects: make(map[string][]byte)}

	archive, err := newS3ArchiveWithClient(client, "archives")
	if err != nil {
		t.Fatal(err)
	}

	key, err := archive.Store(
		t.Context(),
		jobs.ArchiveObject{},
		strings.NewReader("archive"),
	)
	if err != nil {
		t.Fatal(err)
	}

	if !validArchiveKey(key) || !strings.HasPrefix(key, archivePrefix) {
		t.Fatalf("archive key = %q", key)
	}

	if client.contentType != "application/zip" {
		t.Fatalf("content type = %q", client.contentType)
	}

	reader, err := archive.Open(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}

	data, err := io.ReadAll(reader)

	_ = reader.Close()

	if err != nil || string(data) != "archive" {
		t.Fatalf("archive data = %q, err = %v", data, err)
	}

	if err := archive.Delete(t.Context(), key); err != nil {
		t.Fatal(err)
	}

	if _, exists := client.objects[key]; exists {
		t.Fatal("archive remains after delete")
	}

	if _, err := archive.Open(
		t.Context(),
		"importexport/../archive.zip",
	); err == nil {
		t.Fatal("invalid archive key accepted")
	}
}

type fakeArchiveClient struct {
	objects     map[string][]byte
	contentType string
}

func (c *fakeArchiveClient) PutObject(
	_ context.Context,
	input *awss3.PutObjectInput,
	_ ...func(*awss3.Options),
) (*awss3.PutObjectOutput, error) {
	data, err := io.ReadAll(input.Body)
	if err != nil {
		return nil, err
	}

	c.objects[aws.ToString(input.Key)] = bytes.Clone(data)
	c.contentType = aws.ToString(input.ContentType)

	return &awss3.PutObjectOutput{}, nil
}

func (c *fakeArchiveClient) GetObject(
	_ context.Context,
	input *awss3.GetObjectInput,
	_ ...func(*awss3.Options),
) (*awss3.GetObjectOutput, error) {
	data, exists := c.objects[aws.ToString(input.Key)]
	if !exists {
		return nil, errors.New("not found")
	}

	return &awss3.GetObjectOutput{
		Body: io.NopCloser(bytes.NewReader(data)),
	}, nil
}

func (c *fakeArchiveClient) DeleteObject(
	_ context.Context,
	input *awss3.DeleteObjectInput,
	_ ...func(*awss3.Options),
) (*awss3.DeleteObjectOutput, error) {
	delete(c.objects, aws.ToString(input.Key))

	return &awss3.DeleteObjectOutput{}, nil
}
