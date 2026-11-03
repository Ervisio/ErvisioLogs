package archive

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Config holds the non-secret settings of an S3-compatible provider.
type S3Config struct {
	// Endpoint is a URL: https://s3.eu-central-1.amazonaws.com, or http://host:port for a test server.
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	Prefix    string `json:"prefix"`
	PathStyle bool   `json:"path_style"`
	// NoServerChecksum turns off x-amz-checksum-sha256 for servers that reject it. Verification
	// then reads every object back.
	NoServerChecksum bool `json:"no_server_checksum"`
}

// S3Secret is the content of the provider's secret file.
type S3Secret struct {
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
}

// partSize keeps segments up to this size in a single part, so the server checksum is a plain
// SHA-256 of the object rather than a checksum of part checksums.
const partSize = 128 << 20

type S3 struct {
	c      *minio.Client
	cfg    S3Config
	prefix string
}

func NewS3(cfg S3Config, secret []byte) (*S3, error) {
	var sec S3Secret
	if err := json.Unmarshal(secret, &sec); err != nil || sec.AccessKey == "" || sec.SecretKey == "" {
		return nil, errors.New("s3 secret file must be JSON with access_key and secret_key")
	}
	u, err := url.Parse(cfg.Endpoint)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.Path != "" && u.Path != "/" {
		return nil, errors.New("s3 endpoint must be an http(s) URL without a path")
	}
	if cfg.Bucket == "" {
		return nil, errors.New("s3 bucket is required")
	}
	prefix := strings.Trim(cfg.Prefix, "/")
	if prefix != "" {
		if err := ValidKey(prefix); err != nil {
			return nil, errors.New("s3 prefix may only use a-z, 0-9, '.', '_', '-' and '/'")
		}
		prefix += "/"
	}
	lookup := minio.BucketLookupAuto
	if cfg.PathStyle {
		lookup = minio.BucketLookupPath
	}
	c, err := minio.New(u.Host, &minio.Options{
		Creds:        credentials.NewStaticV4(sec.AccessKey, sec.SecretKey, ""),
		Secure:       u.Scheme == "https",
		Region:       cfg.Region,
		BucketLookup: lookup,
		// Needed for x-amz-checksum-sha256 on uploads: the client hashes what it sends and the
		// server rejects the object if its own hash differs.
		TrailingHeaders: !cfg.NoServerChecksum,
	})
	if err != nil {
		return nil, err
	}
	return &S3{c: c, cfg: cfg, prefix: prefix}, nil
}

func (s *S3) key(k string) (string, error) {
	if err := ValidKey(k); err != nil {
		return "", err
	}
	return s.prefix + k, nil
}

func notFound(err error) bool {
	code := minio.ToErrorResponse(err).Code
	return code == "NoSuchKey" || code == "NotFound"
}

func (s *S3) Put(ctx context.Context, key string, r io.Reader, size int64, sum []byte) error {
	k, err := s.key(key)
	if err != nil {
		return err
	}
	opts := minio.PutObjectOptions{ContentType: "application/vnd.apache.parquet", PartSize: partSize}
	if !s.cfg.NoServerChecksum && len(sum) == 32 {
		opts.Checksum = minio.ChecksumSHA256
	}
	info, err := s.c.PutObject(ctx, s.cfg.Bucket, k, r, size, opts)
	if err != nil {
		return err
	}
	if info.Size != size {
		_ = s.c.RemoveObject(context.WithoutCancel(ctx), s.cfg.Bucket, k, minio.RemoveObjectOptions{})
		return fmt.Errorf("short upload: stored %d of %d bytes", info.Size, size)
	}
	return nil
}

func (s *S3) Promote(ctx context.Context, tmpKey, finalKey string) error {
	src, err := s.key(tmpKey)
	if err != nil {
		return err
	}
	dst, err := s.key(finalKey)
	if err != nil {
		return err
	}
	info, err := s.Stat(ctx, tmpKey)
	if err != nil {
		return err
	}
	dest := minio.CopyDestOptions{Bucket: s.cfg.Bucket, Object: dst}
	if info.SHA256 != nil {
		dest.ChecksumType = minio.ChecksumSHA256
	}
	if _, err := s.c.CopyObject(ctx, dest, minio.CopySrcOptions{Bucket: s.cfg.Bucket, Object: src}); err != nil {
		if notFound(err) {
			return ErrNotExist
		}
		return err
	}
	moved, err := s.Stat(ctx, finalKey)
	if err != nil {
		return err
	}
	if moved.Size != info.Size || (info.SHA256 != nil && moved.SHA256 != nil && string(moved.SHA256) != string(info.SHA256)) {
		return fmt.Errorf("%w: copy of %s differs from the source", ErrVerify, tmpKey)
	}
	return s.c.RemoveObject(ctx, s.cfg.Bucket, src, minio.RemoveObjectOptions{})
}

func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	k, err := s.key(key)
	if err != nil {
		return nil, err
	}
	obj, err := s.c.GetObject(ctx, s.cfg.Bucket, k, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	// GetObject is lazy: Stat surfaces a missing object before the caller starts reading.
	if _, err := obj.Stat(); err != nil {
		obj.Close()
		if notFound(err) {
			return nil, ErrNotExist
		}
		return nil, err
	}
	return obj, nil
}

func (s *S3) Stat(ctx context.Context, key string) (ObjectInfo, error) {
	k, err := s.key(key)
	if err != nil {
		return ObjectInfo{}, err
	}
	o, err := s.c.StatObject(ctx, s.cfg.Bucket, k, minio.StatObjectOptions{Checksum: true})
	if err != nil {
		if notFound(err) {
			return ObjectInfo{}, ErrNotExist
		}
		return ObjectInfo{}, err
	}
	return ObjectInfo{Key: key, Size: o.Size, SHA256: fullSHA256(o.ChecksumSHA256), ModTime: o.LastModified}, nil
}

// fullSHA256 decodes a full-object SHA-256. A multipart checksum ("...-N") covers the part
// checksums, not the content, so it is reported as unknown.
func fullSHA256(v string) []byte {
	if v == "" || strings.Contains(v, "-") {
		return nil
	}
	b, err := base64.StdEncoding.DecodeString(v)
	if err != nil || len(b) != 32 {
		return nil
	}
	return b
}

func (s *S3) List(ctx context.Context, prefix string, fn func(ObjectInfo) error) error {
	for o := range s.c.ListObjects(ctx, s.cfg.Bucket, minio.ListObjectsOptions{Prefix: s.prefix + prefix, Recursive: true}) {
		if o.Err != nil {
			return o.Err
		}
		if err := fn(ObjectInfo{Key: strings.TrimPrefix(o.Key, s.prefix), Size: o.Size, ModTime: o.LastModified}); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (s *S3) Delete(ctx context.Context, key string) error {
	k, err := s.key(key)
	if err != nil {
		return err
	}
	err = s.c.RemoveObject(ctx, s.cfg.Bucket, k, minio.RemoveObjectOptions{})
	if err != nil && notFound(err) {
		return nil
	}
	return err
}

// Health checks that the bucket exists and that the credentials can write and delete.
func (s *S3) Health(ctx context.Context) error {
	ok, err := s.c.BucketExists(ctx, s.cfg.Bucket)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("bucket %q does not exist", s.cfg.Bucket)
	}
	k := s.prefix + "health/" + newObjectName()
	if _, err := s.c.PutObject(ctx, s.cfg.Bucket, k, strings.NewReader("ok"), 2, minio.PutObjectOptions{}); err != nil {
		return fmt.Errorf("cannot write to the bucket: %w", err)
	}
	if err := s.c.RemoveObject(ctx, s.cfg.Bucket, k, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("cannot delete from the bucket: %w", err)
	}
	return nil
}

func (s *S3) Capabilities() Capabilities {
	return Capabilities{ServerChecksum: !s.cfg.NoServerChecksum, RangeReads: true}
}

func (s *S3) Close() error { return nil }
