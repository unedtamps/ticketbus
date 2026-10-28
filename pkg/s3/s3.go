// Package s3 is a thin client over any S3-compatible object store (AWS S3,
// Cloudflare R2, MinIO, ...). Objects are uploaded server-side; reads happen
// through a configured public base URL, so the client only needs write
// credentials — the bucket itself is public for downloads.
package s3

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Config wires the client.
//
// Endpoint is host[:port] without scheme — Cloudflare R2:
// "<account_id>.r2.cloudflarestorage.com"; AWS: "s3.<region>.amazonaws.com".
// PublicURL is the bucket's public base URL as served to browsers (R2 dev
// domain: "https://pub-xxx.r2.dev"; custom domain: "https://cdn.example.com";
// MinIO path-style: "http://localhost:9000/bucket").
type Config struct {
	Endpoint  string
	Secure    bool
	Region    string
	AccessKey string
	SecretKey string
	Bucket    string
	PublicURL string
}

// Client uploads objects and resolves their public URLs.
type Client struct {
	mc        *minio.Client
	bucket    string
	publicURL string
}

// New creates a client. It performs no network call: the first Upload is the
// first time the store is contacted.
func New(cfg Config) (*Client, error) {
	mc, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.Secure,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("s3 client: %w", err)
	}
	return &Client{
		mc:        mc,
		bucket:    cfg.Bucket,
		publicURL: strings.TrimSuffix(cfg.PublicURL, "/"),
	}, nil
}

// Upload stores r as an object under key, overwriting whatever is there.
func (c *Client) Upload(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	_, err := c.mc.PutObject(ctx, c.bucket, key, r, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return fmt.Errorf("s3 upload %s: %w", key, err)
	}
	return nil
}

// PublicURL returns the publicly readable URL of an object.
func (c *Client) PublicURL(key string) string {
	return c.publicURL + "/" + key
}
