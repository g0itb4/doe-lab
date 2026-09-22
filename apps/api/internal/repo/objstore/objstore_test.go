package objstore_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"doelab/api/internal/config"
	"doelab/api/internal/domain"
	"doelab/api/internal/repo/objstore"
	"doelab/api/internal/testutil"
)

func TestMain(m *testing.M) { testutil.TestMain(m) }

func newStore(t *testing.T, bucket string) *objstore.Store {
	t.Helper()
	return objstore.New(config.S3{
		Endpoint: testutil.S3(t), Region: testutil.S3Region, Bucket: bucket,
		AccessKeyID: testutil.S3AccessKey, SecretAccessKey: testutil.S3SecretKey, PathStyle: true,
	})
}

func TestPutGetPresign(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newStore(t, "putget")

	// Creating the bucket is safe to repeat.
	for range 2 {
		if err := store.EnsureBucket(ctx); err != nil {
			t.Fatalf("ensure bucket: %v", err)
		}
	}

	body := []byte("site_id,valid_from,export_limit_w\nA,2026-10-01T00:00:00Z,1500\n")
	if err := store.Put(ctx, "runs/r1/envelopes.csv", "text/csv", body); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := store.Get(ctx, "runs/r1/envelopes.csv")
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("get = %q, %v", got, err)
	}
	// Put replaces.
	if err := store.Put(ctx, "runs/r1/envelopes.csv", "text/csv", []byte("replaced")); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.Get(ctx, "runs/r1/envelopes.csv"); string(got) != "replaced" {
		t.Errorf("after a second put: %q", got)
	}

	if _, err := store.Get(ctx, "runs/none.csv"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("get a missing key: %v, want ErrNotFound", err)
	}

	// A presigned URL downloads the object with no credentials.
	url, err := store.PresignGet(ctx, "runs/r1/envelopes.csv", time.Minute)
	if err != nil {
		t.Fatalf("presign: %v", err)
	}
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	downloaded, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || string(downloaded) != "replaced" || res.Header.Get("Content-Type") != "text/csv" {
		t.Errorf("presigned download = %d %q (%s)", res.StatusCode, downloaded, res.Header.Get("Content-Type"))
	}
	// Without the signature, the same object is refused.
	bare, err := http.Get(url[:indexOf(url, '?')])
	if err != nil {
		t.Fatal(err)
	}
	_ = bare.Body.Close()
	if bare.StatusCode == http.StatusOK {
		t.Error("the object downloads without a signature")
	}
}

func TestMissingBucket(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newStore(t, "never-created")
	if err := store.Put(ctx, "k", "text/plain", []byte("x")); err == nil {
		t.Error("put into a missing bucket succeeded")
	}
	if _, err := store.Get(ctx, "k"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("get from a missing bucket: %v, want ErrNotFound", err)
	}
}

func indexOf(s string, c byte) int {
	for i := range len(s) {
		if s[i] == c {
			return i
		}
	}
	return len(s)
}
