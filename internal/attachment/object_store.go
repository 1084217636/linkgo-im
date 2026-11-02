package attachment

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// ObjectStore keeps S3 credentials and object keys server-side. Download still
// passes membership checks and streams through the authenticated API.
type ObjectStore interface {
	Put(context.Context, string, io.Reader, int64, string) error
	Open(context.Context, string) (io.ReadCloser, error)
	Delete(context.Context, string) error
}
type S3Store struct {
	client *minio.Client
	bucket string
}

func ObjectStoreFromEnv() (ObjectStore, error) {
	endpoint := os.Getenv("S3_ENDPOINT")
	if endpoint == "" {
		return nil, nil
	}
	bucket := os.Getenv("S3_BUCKET")
	if bucket == "" {
		bucket = "linkgo-attachments"
	}
	if os.Getenv("S3_ACCESS_KEY") == "" || os.Getenv("S3_SECRET_KEY") == "" {
		return nil, errors.New("S3 credentials required")
	}
	c, err := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(os.Getenv("S3_ACCESS_KEY"), os.Getenv("S3_SECRET_KEY"), ""), Secure: strings.EqualFold(os.Getenv("S3_TLS"), "true")})
	if err != nil {
		return nil, err
	}
	return &S3Store{client: c, bucket: bucket}, nil
}
func (s *S3Store) Put(ctx context.Context, key string, r io.Reader, size int64, mime string) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return err
	}
	if !exists {
		if err = s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{}); err != nil {
			if exists, _ = s.client.BucketExists(ctx, s.bucket); !exists {
				return err
			}
		}
	}
	_, err = s.client.PutObject(ctx, s.bucket, key, r, size, minio.PutObjectOptions{ContentType: mime})
	return err
}
func (s *S3Store) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	o, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	if _, err = o.Stat(); err != nil {
		o.Close()
		return nil, err
	}
	return o, nil
}
func (s *S3Store) Delete(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}
