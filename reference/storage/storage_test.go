package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

func TestDiskReplaceWritesAndOverwritesCompleteMediaSet(t *testing.T) {
	directory := t.TempDir()

	store, err := New(Config{Directory: directory})
	if err != nil {
		t.Fatal(err)
	}

	objects, err := store.Replace(
		context.Background(),
		"workspace-a",
		"sticker.fire",
		"AbCdEfGh",
		validFiles("first"),
	)
	if err != nil {
		t.Fatalf("first Replace() error = %v", err)
	}

	if len(objects.Previews) != 4 || objects.Original == "" ||
		objects.Placeholder == "" {
		t.Fatalf("objects = %+v", objects)
	}

	second, err := store.Replace(
		context.Background(),
		"workspace-a",
		"sticker.fire",
		"HgfEdCbA",
		validFiles("second"),
	)
	if err != nil {
		t.Fatalf("second Replace() error = %v", err)
	}

	data, err := os.ReadFile(
		filepath.Join(directory, filepath.FromSlash(second.Previews[61])),
	)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(data, []byte("second-preview-61")) {
		t.Fatalf("stored preview = %q", data)
	}
}

func TestS3ReplaceWritesCompleteMediaSet(t *testing.T) {
	client := &fakeClient{}

	store, err := newS3WithClient(client, "media")
	if err != nil {
		t.Fatal(err)
	}

	objects, err := store.Replace(
		context.Background(),
		"workspace-a",
		"sticker.fire",
		"AbCdEfGh",
		validFiles("value"),
	)
	if err != nil {
		t.Fatalf("Replace() error = %v", err)
	}

	if len(client.objects) != 6 || len(objects.Previews) != 4 {
		t.Fatalf("objects = %+v uploads = %+v", objects, client.objects)
	}
}

func TestStorageRejectsInvalidConfigAndFiles(t *testing.T) {
	if _, err := New(
		Config{Endpoint: "minio.local"},
	); !errors.Is(
		err,
		ErrConfigInvalid,
	) {
		t.Fatalf("New() error = %v", err)
	}

	store, err := New(Config{Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.Replace(
		context.Background(),
		"workspace-a",
		"key",
		"AbCdEfGh",
		Files{},
	); !errors.Is(
		err,
		ErrFilesInvalid,
	) {
		t.Fatalf("Replace() error = %v", err)
	}
}

func TestDiskReadVersionReadsImmutableMedia(t *testing.T) {
	store, err := New(Config{Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Replace(context.Background(), "workspace-a", "key", "AbCdEfGh", validFiles("first")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Replace(context.Background(), "workspace-a", "key", "HgfEdCbA", validFiles("second")); err != nil {
		t.Fatal(err)
	}
	first, err := store.ReadVersion(context.Background(), "workspace-a", "key", "AbCdEfGh", 61)
	if err != nil || string(first) != "first-preview-61" {
		t.Fatalf("first=%q err=%v", first, err)
	}
	second, err := store.ReadVersion(context.Background(), "workspace-a", "key", "HgfEdCbA", 61)
	if err != nil || string(second) != "second-preview-61" {
		t.Fatalf("second=%q err=%v", second, err)
	}
}

func TestDiskReplaceAcceptsSVGWithoutPreviews(t *testing.T) {
	store, err := New(Config{Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	objects, err := store.Replace(context.Background(), "workspace-a", "logo", "AbCdEfGh", Files{
		Original:    File{Data: []byte("<svg/>"), ContentType: "image/svg+xml"},
		Placeholder: File{Data: []byte("<svg/>"), ContentType: "image/svg+xml"},
		NoPreviews:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if objects.Original == "" || objects.Placeholder == "" || len(objects.Previews) != 0 {
		t.Fatalf("objects=%+v", objects)
	}
}

func validFiles(prefix string) Files {
	return Files{
		Original: File{
			Data:        []byte(prefix + "-original"),
			ContentType: "image/png",
		},
		Previews: []Preview{
			{
				Size: 61,
				File: File{
					Data:        []byte(prefix + "-preview-61"),
					ContentType: "image/png",
				},
			},
			{
				Size: 128,
				File: File{
					Data:        []byte(prefix + "-preview-128"),
					ContentType: "image/png",
				},
			},
			{
				Size: 256,
				File: File{
					Data:        []byte(prefix + "-preview-256"),
					ContentType: "image/png",
				},
			},
			{
				Size: 512,
				File: File{
					Data:        []byte(prefix + "-preview-512"),
					ContentType: "image/png",
				},
			},
		},
		Placeholder: File{Data: []byte("<svg/>"), ContentType: "image/svg+xml"},
	}
}

type storedObject struct {
	data        []byte
	contentType string
}

type fakeClient struct {
	objects map[string]storedObject
	err     error
}

func (c *fakeClient) PutObject(
	_ context.Context,
	input *awss3.PutObjectInput,
	_ ...func(*awss3.Options),
) (*awss3.PutObjectOutput, error) {
	if c.err != nil {
		return nil, c.err
	}

	data, err := io.ReadAll(input.Body)
	if err != nil {
		return nil, err
	}

	if c.objects == nil {
		c.objects = make(map[string]storedObject)
	}

	c.objects[aws.ToString(input.Key)] = storedObject{
		data:        bytes.Clone(data),
		contentType: aws.ToString(input.ContentType),
	}

	return &awss3.PutObjectOutput{}, nil
}

func (c *fakeClient) GetObject(
	_ context.Context,
	input *awss3.GetObjectInput,
	_ ...func(*awss3.Options),
) (*awss3.GetObjectOutput, error) {
	object, ok := c.objects[aws.ToString(input.Key)]
	if !ok {
		return nil, errors.New("not found")
	}
	return &awss3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(object.data))}, nil
}
