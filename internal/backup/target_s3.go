package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// s3Target stores archives in any S3-compatible bucket (MinIO, Garage,
// Backblaze B2, AWS, …).
type s3Target struct {
	client *minio.Client
	bucket string
	prefix string
}

func openS3(config map[string]any, secrets map[string]string) (Target, error) {
	endpoint := cfgString(config, "endpoint")
	bucket := cfgString(config, "bucket")
	if endpoint == "" || bucket == "" {
		return nil, errors.New("endpoint and bucket are required")
	}
	if secrets["accessKey"] == "" || secrets["secretKey"] == "" {
		return nil, errors.New("access key and secret key are required")
	}
	useSSL := true
	if strings.HasPrefix(endpoint, "http://") {
		useSSL = false
		endpoint = strings.TrimPrefix(endpoint, "http://")
	}
	endpoint = strings.TrimPrefix(endpoint, "https://")

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(secrets["accessKey"], secrets["secretKey"], ""),
		Secure: useSSL,
		Region: cfgString(config, "region"),
	})
	if err != nil {
		return nil, err
	}
	return &s3Target{
		client: client,
		bucket: bucket,
		prefix: strings.Trim(cfgString(config, "prefix"), "/"),
	}, nil
}

func (t *s3Target) Close() {}

func (t *s3Target) full(p string) string {
	if t.prefix == "" {
		return p
	}
	return path.Join(t.prefix, p)
}

func (t *s3Target) Test(ctx context.Context) error {
	exists, err := t.client.BucketExists(ctx, t.bucket)
	if err != nil {
		return fmt.Errorf("cannot reach the S3 endpoint: %w", err)
	}
	if !exists {
		return fmt.Errorf("bucket %q does not exist or the credentials cannot see it", t.bucket)
	}
	return runProbe(ctx, t)
}

func (t *s3Target) Write(ctx context.Context, p string, r io.Reader, size int64) error {
	_, err := t.client.PutObject(ctx, t.bucket, t.full(p), r, size, minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})
	return err
}

func (t *s3Target) Read(ctx context.Context, p string) (io.ReadCloser, error) {
	obj, err := t.client.GetObject(ctx, t.bucket, t.full(p), minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	// GetObject is lazy; surface missing objects now.
	if _, err := obj.Stat(); err != nil {
		obj.Close()
		return nil, err
	}
	return obj, nil
}

func (t *s3Target) List(ctx context.Context, prefix string) ([]Entry, error) {
	entries := []Entry{}
	listPrefix := t.full(prefix)
	for object := range t.client.ListObjects(ctx, t.bucket, minio.ListObjectsOptions{
		Prefix: listPrefix, Recursive: true,
	}) {
		if object.Err != nil {
			return nil, object.Err
		}
		clean := object.Key
		if t.prefix != "" {
			clean = strings.TrimPrefix(strings.TrimPrefix(clean, t.prefix), "/")
		}
		entries = append(entries, Entry{Path: clean, Size: object.Size, ModTime: object.LastModified})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ModTime.After(entries[j].ModTime) })
	return entries, nil
}

func (t *s3Target) Delete(ctx context.Context, p string) error {
	return t.client.RemoveObject(ctx, t.bucket, t.full(p), minio.RemoveObjectOptions{})
}

func (t *s3Target) Capacity(context.Context) (Capacity, error) {
	return Capacity{Unknown: true}, nil
}
