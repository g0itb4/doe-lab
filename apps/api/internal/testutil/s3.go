package testutil

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// S3 credentials of the throwaway gateway.
const (
	S3AccessKey = "doelab"
	S3SecretKey = "doelab-test-only"
	S3Region    = "us-east-1"
)

var (
	s3Once      sync.Once
	s3Container testcontainers.Container
	s3Endpoint  string
	s3Err       error
)

func startS3(ctx context.Context) {
	s3Container, s3Err = testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			// The image of infra/compose.yaml.
			Image:        "docker.io/versity/versitygw:v1.8.0",
			Cmd:          []string{"--access", S3AccessKey, "--secret", S3SecretKey, "posix", "/tmp/data"},
			Entrypoint:   []string{"/bin/sh", "-c", `mkdir -p /tmp/data && exec /usr/local/bin/versitygw "$@"`, "--"},
			ExposedPorts: []string{"7070/tcp"},
			WaitingFor:   wait.ForListeningPort("7070/tcp").WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	if s3Err != nil {
		return
	}
	host, err := s3Container.Host(ctx)
	if err != nil {
		s3Err = err
		return
	}
	port, err := s3Container.MappedPort(ctx, "7070/tcp")
	if err != nil {
		s3Err = err
		return
	}
	s3Endpoint = fmt.Sprintf("http://%s:%s", host, port.Port())
}

// S3 returns the endpoint of a throwaway S3-compatible gateway, shared by the
// tests of a package. A test should use a bucket of its own. The test is
// skipped when the container tier is off.
func S3(t *testing.T) string {
	t.Helper()
	if !Enabled() {
		t.Skip("set DOELAB_TEST_DB=1 (or run `just test`): needs a container runtime")
	}
	s3Once.Do(func() { startS3(context.Background()) })
	if s3Err != nil {
		t.Fatalf("start the S3 gateway: %v", s3Err)
	}
	return s3Endpoint
}
