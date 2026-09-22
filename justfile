set shell := ["zsh", "-cu"]

# So a recipe can forward its arguments with "$@" and keep the caller's quoting.
set positional-arguments := true

# podman compose delegates to the docker-compose binary, but sets DOCKER_HOST to
# the podman machine socket first; plain `docker-compose` would look for a
# Docker daemon that is not there.
compose := "podman compose -f infra/compose.yaml"

# list recipes
default:
    @just --list

# install dependencies, generate code and install the git hooks, on a fresh clone
setup:
    bun install
    cd apps/api && go mod download
    @just gen
    @just hooks

# regenerate: buf (proto to Go, TypeScript and OpenAPI) and sqlc (SQL to Go)
gen:
    bun run generate
    cd packages/db && sqlc generate
    cd apps/api && go mod tidy

# --wait blocks on the healthchecks. Without it the migration races the
# database.
#
# start Postgres + TimescaleDB and the S3 gateway, and bring the schema up to date
up:
    {{compose}} up -d --wait postgres
    {{compose}} up -d s3
    @just migrate up

# golang-migrate, driven by apps/api/cmd/migrate. The SQL lives in
# packages/db/migrations, because sqlc reads the same directory as its schema.
#
# migrations: up | down [n] | status | force <v>
migrate *args:
    cd apps/api && DOELAB_ENV=development go run ./cmd/migrate "$@"

# psql into the dev database
psql *args:
    {{compose}} exec postgres psql -U doelab -d doelab "$@"

# destroy the dev database, the object store and everything running against them
nuke: _kill
    {{compose}} --profile obs down -v --remove-orphans
    @echo "fresh start: just up"

# By PORT, not by process name: `go run` execs a binary out of the build cache,
# which no sensible pkill pattern matches.
[private]
_kill:
    -@lsof -ti:3100,5273,4273 -sTCP:LISTEN 2>/dev/null | xargs -r kill 2>/dev/null || true

# install the git hooks (pre-commit and pre-push)
hooks:
    prek install --overwrite

# run every hook of both stages on every file, as CI does
check:
    prek run --all-files
    prek run --all-files --stage pre-push

# download the raw datasets into data/raw/ and verify their checksums
data:
    scripts/fetch-data.sh

# verify the raw datasets on disk against data/SHA256SUMS, no network
data-verify:
    scripts/fetch-data.sh --verify

# ── style ────────────────────────────────────────────────────────────────────

# rewrite files in place: gofumpt + goimports, buf format, prettier
fmt:
    cd apps/api && golangci-lint fmt
    bunx buf format -w
    bunx prettier --write --log-level warn .

# fail on a formatting diff, without rewriting (the pre-commit hooks)
fmt-check: fmt-check-go fmt-check-proto fmt-check-web

[private]
fmt-check-go:
    cd apps/api && golangci-lint fmt --diff

[private]
fmt-check-proto:
    bunx buf format --diff --exit-code

[private]
fmt-check-web:
    bunx prettier --check --log-level warn .

# ── lint ─────────────────────────────────────────────────────────────────────

# every linter
lint: lint-go lint-proto lint-sql

# buf lint: naming, packages, and the STANDARD rule set
lint-proto:
    bunx buf lint

# sqlc type-checks every query against the migrations
lint-sql:
    cd packages/db && sqlc compile

# govet, staticcheck, errcheck, revive, gosec, and depguard for the layering
lint-go:
    cd apps/api && golangci-lint run

# ── secrets ──────────────────────────────────────────────────────────────────

[private]
secrets-staged:
    gitleaks git --pre-commit --staged --no-banner --redact

[private]
secrets-push:
    scripts/gitleaks-push.sh

# scan the whole history for secrets
secrets:
    gitleaks git --no-banner --redact

# ── tests and coverage ───────────────────────────────────────────────────────

# The offline Go tier: no container, no network. The profile goes through
# covergate, which holds each package to its floor in apps/api/coverage.json.
# The recipe prints its duration; the pre-commit budget is 15 s warm.
#
# -short skips the tests that are slow under the race detector (the dense
# solver on the full feeder). Coverage must reach its floors without them;
# `just test-go` runs everything.
#
# -coverpkg=./... counts a statement as covered whichever package's tests ran
# it: the end-to-end tests in internal/server drive the controllers, the
# services and protomap through the real interceptor chain, and that is where
# most of their coverage comes from.
#
# Go unit tests with the race detector, then the per-package coverage gate
cover-go:
    #!/usr/bin/env zsh
    set -eu
    zmodload zsh/datetime
    cd apps/api
    mkdir -p coverage
    start=$EPOCHREALTIME
    go test -short -race -coverpkg=./... -coverprofile=coverage/unit.coverprofile ./... >coverage/unit.log 2>&1 \
      || { cat coverage/unit.log; exit 1 }
    go run ./cmd/covergate
    printf 'cover-go: %.1f s\n' $(( EPOCHREALTIME - start ))

# every Go test of the offline tier, including the slow ones, with the race detector
test-go:
    cd apps/api && go test -race ./...

# Needs a container runtime: the tier runs against a throwaway TimescaleDB and
# S3 gateway through testcontainers. Ryuk, the reaper, is off because podman
# does not grant it the privileged container it wants; testutil.TestMain stops
# the containers instead.
#
# the container tier: migrations, schema constraints, parity, repositories
test-db:
    cd apps/api && DOELAB_TEST_DB=1 TESTCONTAINERS_RYUK_DISABLED=true go test -count=1 ./internal/repo/... ./internal/testutil/...

# everything: hooks on every file, both Go tiers, and the engine's time budget
test: check test-go test-db bench

# A plain build: timings mean nothing under the race detector or coverage.
#
# engine benchmarks, and the time budget for one day of envelopes on LV10
bench:
    cd apps/api && go test -run '^TestDayBudget$' -bench . -benchmem -v ./internal/engine/ | grep -vE '^(=== RUN|PASS|ok)'

# ── fixtures ─────────────────────────────────────────────────────────────────

# Needs the raw data (`just data`) and the tools venv with OpenDSSDirect.py.
#
# regenerate the engine fixtures: the LV10 network and the OpenDSS reference voltages
fixtures:
    cd apps/api && go test ./internal/engine/dss -run TestLV10Network -update
    .venv-tools/bin/python tools/opendss_snapshots.py

# ── demo data ────────────────────────────────────────────────────────────────

# Needs `just up` and `just data`. Safe to run again: it changes nothing.
#
# load the LV10 feeder and a year of Ausgrid profiles into the dev database
import *args:
    cd apps/api && DOELAB_ENV=development go run ./cmd/import \
      -feeder ../../data/raw/csiro/LV/LV10_223bus \
      -ausgrid ../../data/raw/ausgrid/Ausgrid_solar_home_data.zip "$@"
