// Command migrate is a thin wrapper around golang-migrate:
//
//	migrate up [n] | down [n] | status | force <v>
//
// Two properties migrations depend on: each file runs as ONE Exec inside an
// implicit transaction (never set x-multi-statement=true — it splits the file
// per statement and breaks both atomicity and the trailing
// `SELECT apply_conventions();`), and versions are a linear high-water mark —
// stamp with `date -u +%Y%m%d%H%M%S`, renumber rather than merge.
// See packages/db/README.md.
package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/lib/pq"

	"doelab/api/internal/config"
)

// migrationsSubdir is where the SQL lives, relative to the repo root. It stays
// in packages/db because sqlc reads the same directory as its schema. There is
// deliberately no second copy for the Go side to drift from.
const migrationsSubdir = "packages/db/migrations"

var filenamePattern = regexp.MustCompile(`^(\d{14})_(.+)\.up\.sql$`)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}

func run() error {
	dir := flag.String("path", "", "migrations directory (default: found by walking up to the repo root)")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if *dir == "" {
		if *dir, err = findMigrations(); err != nil {
			return err
		}
	}

	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	// The process is exiting; a failed close loses nothing.
	defer func() { _ = db.Close() }()
	if err := db.Ping(); err != nil {
		return fmt.Errorf("connect to %s: %w", redact(cfg.DatabaseURL), err)
	}

	// WithInstance rather than a database URL so the *sql.DB is ours to reuse
	// afterwards, because assert_conventions() below needs it.
	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("postgres driver: %w", err)
	}

	m, err := migrate.NewWithDatabaseInstance("file://"+*dir, "postgres", driver)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	m.Log = logger{}

	args := flag.Args()
	command := "up"
	if len(args) > 0 {
		command = args[0]
	}
	var arg string
	if len(args) > 1 {
		arg = args[1]
	}

	switch command {
	case "up":
		return up(m, db, *dir, arg)
	case "down":
		return down(m, *dir, arg)
	case "status":
		return status(m, *dir)
	case "force":
		return force(m, arg)
	default:
		return fmt.Errorf("unknown command %q (want up, down, status or force)", command)
	}
}

func up(m *migrate.Migrate, db *sql.DB, dir, arg string) error {
	var err error
	if arg == "" {
		err = m.Up()
	} else {
		n, parseErr := count(arg)
		if parseErr != nil {
			return parseErr
		}
		err = m.Steps(n)
	}

	switch {
	case errors.Is(err, migrate.ErrNoChange):
		fmt.Println("nothing to apply")
	case err != nil:
		return explain(m, dir, err)
	}

	return assertConventions(db)
}

func down(m *migrate.Migrate, dir, arg string) error {
	n := 1
	if arg != "" {
		var err error
		if n, err = count(arg); err != nil {
			return err
		}
	}
	// Negative steps roll back. m.Down() would revert EVERYTHING, which is never
	// what a bare `migrate down` should mean.
	if err := m.Steps(-n); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			fmt.Println("nothing to revert")
			return nil
		}
		return explain(m, dir, err)
	}
	return nil
}

func status(m *migrate.Migrate, dir string) error {
	version, dirty, err := m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		version, dirty, err = 0, false, nil
	}
	if err != nil {
		return err
	}
	files, err := versions(dir)
	if err != nil {
		return err
	}

	fmt.Printf("version: %d  dirty: %t\n", version, dirty)
	if dirty {
		// Everything here is stdout, in order, because a recovery instruction
		// interleaved unpredictably with the listing it refers to is worse than
		// no instruction.
		fmt.Printf("\nDATABASE IS DIRTY. Migration %d failed partway; because a file runs in\n"+
			"one implicit transaction, nothing it did survived, so the schema is still at\n"+
			"%d. Verify by hand, then: just migrate force %d\n\n",
			version, previous(files, uint64(version)), previous(files, uint64(version)))
	}

	for _, f := range files {
		state := "pending"
		switch {
		// The dirty version is the one that FAILED, not the last that succeeded.
		// Calling it "applied" is how someone forces to the wrong version.
		case dirty && f.version == uint64(version):
			state = "FAILED"
		case f.version <= uint64(version):
			state = "applied"
		}
		fmt.Printf("  %-7s  %d %s\n", state, f.version, f.name)
	}

	// The linear-version blind spot, surfaced. A file at or below the high-water
	// mark is *assumed* applied; if it is in fact new (a branch merge with
	// interleaved timestamps. It will never run and nothing else would say so.
	if !dirty && version > 0 && len(files) > 0 && files[len(files)-1].version < uint64(version) {
		fmt.Fprintf(os.Stderr,
			"\nNOTE: the highest migration file (%d) sorts below the recorded version (%d).\n"+
				"Versions are a high-water mark, so anything under one is treated as applied\n"+
				"whether it ran or not. Renumber the newer file above %d.\n",
			files[len(files)-1].version, version, version)
	}
	return nil
}

// previous is the highest migration strictly below v: the version the schema is
// actually at when v is dirty, since v's transaction rolled back whole.
func previous(files []migrationFile, v uint64) uint64 {
	var out uint64
	for _, f := range files {
		if f.version < v && f.version > out {
			out = f.version
		}
	}
	return out
}

func force(m *migrate.Migrate, arg string) error {
	if arg == "" {
		return errors.New("usage: migrate force <version>")
	}
	v, err := strconv.Atoi(arg)
	if err != nil || v < 0 {
		return fmt.Errorf("expected a non-negative version, got %q", arg)
	}
	if err := m.Force(v); err != nil {
		return err
	}
	fmt.Printf("forced to version %d, dirty cleared\n", v)
	return nil
}

// assertConventions runs the schema's own self-check, if it has one.
//
// apply_conventions() derives the updated_at, audit and soft-delete-cascade
// triggers from information_schema rather than from a hand-maintained list of
// table names; assert_conventions() is what catches a table that slipped
// through. Guarded on existence because the function only arrives with the
// conventions migration, and `migrate up` has to work on an empty database.
func assertConventions(db *sql.DB) error {
	var present bool
	if err := db.QueryRow(
		`SELECT to_regprocedure('public.assert_conventions()') IS NOT NULL`,
	).Scan(&present); err != nil {
		return fmt.Errorf("check for assert_conventions: %w", err)
	}
	if !present {
		return nil
	}
	if _, err := db.Exec(`SELECT assert_conventions()`); err != nil {
		return fmt.Errorf("schema conventions: %w", err)
	}
	return nil
}

// explain adds the recovery instruction to a failed migration, because the
// driver's own error says what broke but not what to do about it.
//
// The version golang-migrate marks dirty is the one that FAILED, not the last
// that succeeded, so forcing to it would assert that a migration which rolled
// back had in fact run. The recovery target is always the version below it.
func explain(m *migrate.Migrate, dir string, err error) error {
	version, dirty, verr := m.Version()
	if verr != nil || !dirty {
		return err
	}
	files, ferr := versions(dir)
	if ferr != nil {
		return err
	}
	at := previous(files, uint64(version))
	return fmt.Errorf("%w\n\nMigration %d failed and rolled back whole, so the schema is still at "+
		"%d. Verify by hand, then run: just migrate force %d", err, version, at, at)
}

type migrationFile struct {
	version uint64
	name    string
}

func versions(dir string) ([]migrationFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []migrationFile
	for _, e := range entries {
		m := filenamePattern.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		v, err := strconv.ParseUint(m[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		out = append(out, migrationFile{version: v, name: m[2]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

// findMigrations walks up from the working directory so the command works from
// anywhere in the repo: apps/api under `just`, the root by hand.
func findMigrations() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, migrationsSubdir)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no %s found above %s (pass -path)", migrationsSubdir, dir)
		}
		dir = parent
	}
}

func count(arg string) (int, error) {
	n, err := strconv.Atoi(arg)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("expected a positive integer, got %q", arg)
	}
	return n, nil
}

// redact keeps the password out of a connection-refused message, which is the
// one error guaranteed to be pasted into a chat window.
func redact(url string) string {
	if at := regexp.MustCompile(`//[^/@]*@`); at.MatchString(url) {
		return at.ReplaceAllString(url, "//***@")
	}
	return url
}

type logger struct{}

func (logger) Printf(format string, v ...any) { fmt.Printf(format, v...) }
func (logger) Verbose() bool                  { return false }
