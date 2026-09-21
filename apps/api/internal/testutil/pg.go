// Package testutil starts throwaway services for the container tier of the
// tests: a Postgres with TimescaleDB, migrated with the real migrations.
//
// Gated on DOELAB_TEST_DB rather than a build tag, so `go test ./...` stays
// fast and offline while `just test` opts in. A build tag would be tidier to
// read, but `go mod tidy` ignores files behind custom tags and would drop
// testcontainers from go.mod.
//
// Why a container and not the dev database: the dev database is stateful, and
// it would not answer the question these tests ask, which is whether the
// MIGRATIONS produce a schema with the properties they claim.
package testutil

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratepg "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file" // the migration source
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/lib/pq" // the driver golang-migrate uses
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Enabled reports whether the container tier is switched on.
func Enabled() bool {
	return os.Getenv("DOELAB_TEST_DB") != ""
}

var (
	pgOnce      sync.Once
	pgContainer *postgres.PostgresContainer
	pgAdminURL  string // connects to the `postgres` database, for CREATE DATABASE
	pgErr       error
)

func startPostgres(ctx context.Context) {
	pgContainer, pgErr = postgres.Run(ctx,
		// The image of infra/compose.yaml.
		"docker.io/timescale/timescaledb:latest-pg17",
		postgres.WithDatabase("postgres"),
		postgres.WithUsername("doelab"),
		postgres.WithPassword("doelab"),
		testcontainers.WithEnv(map[string]string{"TIMESCALEDB_TELEMETRY": "off"}),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(90*time.Second)),
	)
	if pgErr != nil {
		return
	}
	pgAdminURL, pgErr = pgContainer.ConnectionString(ctx, "sslmode=disable")
}

// TestMain runs a package's tests and then stops the containers it started.
// Call it from a TestMain in any package that uses this one:
//
//	func TestMain(m *testing.M) { testutil.TestMain(m) }
//
// Without this the containers outlive the test binary: Ryuk, the reaper that
// testcontainers normally relies on, wants a privileged container that podman
// does not grant by default, so it is disabled. t.Cleanup cannot do the job,
// because the containers are shared by every test of the package.
func TestMain(m *testing.M) {
	code := m.Run()
	ctx := context.Background()
	if pgContainer != nil {
		if err := pgContainer.Terminate(ctx); err != nil {
			log.Printf("testutil: could not stop the postgres container: %v", err)
		}
	}
	if s3Container != nil {
		if err := s3Container.Terminate(ctx); err != nil {
			log.Printf("testutil: could not stop the S3 container: %v", err)
		}
	}
	os.Exit(code)
}

// Postgres hands this test its own database with every migration applied, and
// drops it afterwards. The test is skipped when the container tier is off.
func Postgres(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := PostgresURL(t)
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// PostgresURL is Postgres for a caller that wants the connection string.
func PostgresURL(t *testing.T) string {
	t.Helper()
	dsn := EmptyDatabase(t)
	if err := Migrate(dsn, "up"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return dsn
}

// EmptyDatabase hands this test its own database with NO migrations applied,
// for the tests of the migrations themselves.
func EmptyDatabase(t *testing.T) string {
	t.Helper()
	if !Enabled() {
		t.Skip("set DOELAB_TEST_DB=1 (or run `just test`): needs a container runtime")
	}
	pgOnce.Do(func() { startPostgres(context.Background()) })
	if pgErr != nil {
		t.Fatalf("start postgres: %v", pgErr)
	}

	// One database per test, named after it, so a failure names the database
	// that could be inspected.
	name := databaseName(t.Name())
	if err := execSQL(pgAdminURL, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	t.Cleanup(func() {
		// Best effort: the container goes away with the test binary anyway.
		_ = execSQL(pgAdminURL, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})

	u, err := url.Parse(pgAdminURL)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	return u.String()
}

// Migrate runs the real migrations against dsn with the same library the
// runner uses: "up" applies all of them, "down" reverts all of them.
func Migrate(dsn, direction string) error {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	driver, err := migratepg.WithInstance(db, &migratepg.Config{})
	if err != nil {
		return fmt.Errorf("driver: %w", err)
	}
	m, err := migrate.NewWithDatabaseInstance("file://"+MigrationsDir(), "postgres", driver)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if direction == "down" {
		err = m.Down()
	} else {
		err = m.Up()
	}
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("%s: %w", direction, err)
	}
	// Releases the driver's own connection, which db.Close() does not.
	sourceErr, dbErr := m.Close()
	return errors.Join(sourceErr, dbErr)
}

// MigrationsDir resolves packages/db/migrations from this file's own
// location, so the tests do not depend on the working directory.
func MigrationsDir() string {
	return filepath.Join(RepoRoot(), "packages", "db", "migrations")
}

// RepoRoot is the root of the repository.
func RepoRoot() string {
	_, self, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(self), "..", "..", "..", "..")
}

func execSQL(dsn, stmt string) error {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	_, err = db.Exec(stmt)
	return err
}

// databaseName makes a valid, unique database name from a test's name: the
// readable start of it, and a hash of all of it.
func databaseName(test string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(test) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			b.WriteRune(c)
		} else {
			b.WriteByte('_')
		}
		if b.Len() == 40 {
			break
		}
	}
	sum := sha256.Sum256([]byte(test))
	return "t_" + b.String() + "_" + hex.EncodeToString(sum[:4])
}
