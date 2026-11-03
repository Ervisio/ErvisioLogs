//go:build integration

package archive

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Run through scripts/archive-test.sh, which starts the servers and sets these variables.

func TestS3Contract(t *testing.T) {
	endpoint := os.Getenv("ERVISIO_LOGS_TEST_S3_ENDPOINT")
	secretFile := os.Getenv("ERVISIO_LOGS_TEST_S3_SECRET_FILE")
	if endpoint == "" || secretFile == "" {
		t.Skip("ERVISIO_LOGS_TEST_S3_ENDPOINT not set")
	}
	secret, err := os.ReadFile(secretFile)
	if err != nil {
		t.Fatal(err)
	}
	bucket := "archive-test-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	makeBucket(t, endpoint, secret, bucket)
	for _, tc := range []struct {
		name string
		cfg  S3Config
	}{
		{"server checksum", S3Config{Endpoint: endpoint, Region: "us-east-1", Bucket: bucket, Prefix: "with-sum", PathStyle: true}},
		{"read back", S3Config{Endpoint: endpoint, Region: "us-east-1", Bucket: bucket, Prefix: "no-sum", PathStyle: true, NoServerChecksum: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := NewS3(tc.cfg, secret)
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			runContract(t, p, "")
			if !tc.cfg.NoServerChecksum {
				t.Run("server reports the sha256", func(t *testing.T) {
					ctx := context.Background()
					if err := p.Put(ctx, "tmp/sum.parquet", stringsReader("hello"), 5, sha("hello")); err != nil {
						t.Fatal(err)
					}
					info, err := p.Stat(ctx, "tmp/sum.parquet")
					if err != nil {
						t.Fatal(err)
					}
					if string(info.SHA256) != string(sha("hello")) {
						t.Fatalf("server checksum = %x", info.SHA256)
					}
					_ = p.Delete(ctx, "tmp/sum.parquet")
				})
			}
		})
	}

	t.Run("wrong credentials fail health", func(t *testing.T) {
		bad, _ := json.Marshal(S3Secret{AccessKey: "nobody", SecretKey: "wrongwrongwrong"})
		p, err := NewS3(S3Config{Endpoint: endpoint, Region: "us-east-1", Bucket: bucket, PathStyle: true}, bad)
		if err != nil {
			t.Fatal(err)
		}
		if err := p.Health(context.Background()); err == nil {
			t.Fatal("health passed with wrong credentials")
		}
	})
	t.Run("missing bucket fails health", func(t *testing.T) {
		p, err := NewS3(S3Config{Endpoint: endpoint, Region: "us-east-1", Bucket: "does-not-exist-" + bucket, PathStyle: true}, secret)
		if err != nil {
			t.Fatal(err)
		}
		if err := p.Health(context.Background()); err == nil {
			t.Fatal("health passed for a missing bucket")
		}
	})
}

func makeBucket(t *testing.T, endpoint string, secret []byte, bucket string) {
	t.Helper()
	var sec S3Secret
	if err := json.Unmarshal(secret, &sec); err != nil {
		t.Fatal(err)
	}
	c, err := minio.New(endpoint[len("http://"):], &minio.Options{Creds: credentials.NewStaticV4(sec.AccessKey, sec.SecretKey, ""), BucketLookup: minio.BucketLookupPath})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	// SeaweedFS answers before its volume server is ready; retry until the bucket can be created.
	for {
		err = c.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: "us-east-1"})
		if err == nil {
			return
		}
		if ctx.Err() != nil {
			t.Fatalf("create bucket: %v", err)
		}
		time.Sleep(time.Second)
	}
}

func TestSFTPContract(t *testing.T) {
	addr := os.Getenv("ERVISIO_LOGS_TEST_SFTP_ADDR")
	if addr == "" {
		t.Skip("ERVISIO_LOGS_TEST_SFTP_ADDR not set")
	}
	key, err := os.ReadFile(os.Getenv("ERVISIO_LOGS_TEST_SFTP_KEY_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	hostKey, err := os.ReadFile(os.Getenv("ERVISIO_LOGS_TEST_SFTP_HOST_KEY_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)
	cfg := SFTPConfig{Host: host, Port: port, User: "archive", BaseDir: "upload/run-" + strconv.FormatInt(time.Now().UnixNano(), 36), HostKey: string(hostKey)}
	p, err := NewSFTP(cfg, key)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	waitHealthy(t, p)
	runContract(t, p, "")

	t.Run("reconnects after the connection drops", func(t *testing.T) {
		p.mu.Lock()
		p.conn.Close() // simulate a dropped TCP connection
		p.mu.Unlock()
		if err := p.Health(context.Background()); err != nil {
			t.Fatalf("health after a dropped connection: %v", err)
		}
	})
	t.Run("rejects a different host key", func(t *testing.T) {
		other := cfg
		other.HostKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGb3lBqvJmCaH1QZz9M3vXk0i0r8FqKk5ZcM0Lw9yQ0b"
		q, err := NewSFTP(other, key)
		if err != nil {
			t.Fatal(err)
		}
		defer q.Close()
		if err := q.Health(context.Background()); err == nil {
			t.Fatal("connected to a server with an unexpected host key")
		}
	})
}

func waitHealthy(t *testing.T, p Provider) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		err := p.Health(context.Background())
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("provider never became healthy: %v", err)
		}
		time.Sleep(time.Second)
	}
}
