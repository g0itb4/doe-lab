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

# Prometheus on :9290 and Grafana on :3300, with the doe-lab dashboard
# provisioned. Needs the API running (`just api` or `just dev`).
#
# start the local observability stack
obs:
    {{compose}} --profile obs up -d prometheus grafana
    @echo "Grafana:    http://localhost:3300/d/doelab"
    @echo "Prometheus: http://localhost:9290"

# Builds here, ships with rsync, migrates, switches the release and checks its
# health; rolls back by itself when the check fails. The server is set up
# once with the playbook in infra/prod/ansible. See infra/prod/README.md.
#
# deploy a release: just deploy deploy@example.com
deploy host:
    bash scripts/deploy.sh {{host}}

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

# llm/index.md is loaded into every agent session through CLAUDE.md, so it
# stays inside a size budget, and every path and recipe it names must exist.
#
# check the agent index against the repo
llm-check:
    bun scripts/llm-index-check.ts

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

# svelte-check: TypeScript in the .svelte and .ts files of the web app
typecheck-web:
    bun run --filter @doelab/web typecheck

# ── tests and coverage ───────────────────────────────────────────────────────

# The offline Go tier: no container, no network. The profile goes through
# covergate, which holds each package to its floor in apps/api/coverage.json.
# The recipe prints its duration; the pre-commit budget is 15 s warm.
#
# -short skips the tests that are slow under the race detector (the dense
# solver on the full feeder). Coverage must reach its floors without them;
# `just test-go` runs everything.
#
# -coverpkg counts a statement as covered whichever package's tests ran it:
# the end-to-end tests in internal/server drive the controllers, the services
# and protomap through the real interceptor chain, and that is where most of
# their coverage comes from.
#
# The engine is measured on its own, by its own tests. Counting it from other
# packages' tests as well would instrument its inner loops in every test
# binary that solves a power flow, and under the race detector that makes a
# two-second test take a minute.
#
# Go unit tests with the race detector, then the per-package coverage gate
cover-go:
    #!/usr/bin/env zsh
    set -eu
    zmodload zsh/datetime
    cd apps/api
    mkdir -p coverage
    start=$EPOCHREALTIME
    rest=(${(f)"$(go list ./... | grep -vE '/internal/engine(/|$)')"})
    go test -short -race -coverprofile=coverage/engine.coverprofile ./internal/engine/... >coverage/unit.log 2>&1 \
      || { cat coverage/unit.log; exit 1 }
    go test -short -race -coverpkg=${(j:,:)rest} -coverprofile=coverage/rest.coverprofile $rest >coverage/unit.log 2>&1 \
      || { cat coverage/unit.log; exit 1 }
    cat coverage/engine.coverprofile coverage/rest.coverprofile >coverage/unit.coverprofile
    go run ./cmd/covergate
    printf 'cover-go: %.1f s\n' $(( EPOCHREALTIME - start ))

# In a real browser (headless Chromium, through Playwright). The thresholds
# are in apps/web/vitest.config.ts and, like the Go floors, only go up.
#
# web unit and component tests, with the coverage thresholds
cover-web:
    bun run --filter @doelab/web test

# The first-load JavaScript and CSS of every prerendered page, gzipped.
#
# production build of the web app, held to its bundle budget
build-web:
    bun run --filter @doelab/web build
    bun scripts/bundle-budget.ts apps/web/dist 120

# The production build in headless Chromium, at a phone's width and a
# desktop's, with the API answered in the browser (apps/web/e2e/mock.ts):
# smoke, keyboard-only use, WCAG 2.2 AA with axe, and no sideways scroll.
#
# end-to-end and accessibility tests of the web app
e2e: build-web
    cd apps/web && bunx playwright test

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
    cd apps/api && DOELAB_TEST_DB=1 TESTCONTAINERS_RYUK_DISABLED=true go test -count=1 -race ./internal/repo/... ./internal/testutil/...

# everything: hooks on every file, both Go tiers, the engine's time budget, and the web app end to end
test: check test-go test-db bench e2e

# A plain build: timings mean nothing under the race detector or coverage.
#
# engine benchmarks, and the time budget for one day of envelopes on LV10
bench:
    cd apps/api && go test -run '^TestDayBudget$' -bench . -benchmem -v ./internal/engine/ | grep -vE '^(=== RUN|PASS|ok)'

# ── fixtures ─────────────────────────────────────────────────────────────────

# Needs the raw data (`just data`) and the tools venv with OpenDSSDirect.py.
#
# regenerate the engine fixtures: the networks and the OpenDSS reference voltages
fixtures:
    cd apps/api && go test ./internal/engine/dss -run 'TestLV10Network|TestTemplateNetworks' -update
    .venv-tools/bin/python tools/opendss_snapshots.py

# ── demo data ────────────────────────────────────────────────────────────────

# Needs `just up` and `just data`. Safe to run again: it changes nothing.
#
# load the fleet of data/fleet and a year of Ausgrid profiles into the dev database
import *args:
    cd apps/api && DOELAB_ENV=development go run ./cmd/import \
      -fleet ../../data/fleet -feeders ../../data/raw/csiro/LV \
      -ausgrid ../../data/raw/ausgrid/Ausgrid_solar_home_data.zip "$@"

# ── run ──────────────────────────────────────────────────────────────────────

# Feeder time in development: sixty times the wall clock, anchored at the start
# of today (UTC), so a day on the feeder passes in 24 minutes.
#
# METRICS_ADDR is on all interfaces in development, so that the Prometheus
# container of `just obs` can scrape the API on the host. Production keeps
# metrics on loopback, and config.Load refuses anything else there.
dev_env := "DOELAB_ENV=development METRICS_ADDR=0.0.0.0:9464 DEMO_CLOCK_SPEED=60 DEMO_CLOCK_ANCHOR=" + `date -u +%Y-%m-%dT00:00:00Z`

# the API on :3100, restarted when a Go file changes (needs `just up`)
api: _kill
    cd apps/api && {{dev_env}} go tool wgo run ./cmd/api

# the web UI on :5273, with /rpc proxied to the API on :3100
web:
    bun run --filter @doelab/web dev

# Both in watch mode; Ctrl-C stops both.
#
# the database, the API and the web UI
dev: _kill up
    #!/usr/bin/env zsh
    # The trap is taken off first: `kill 0` signals this shell too, and would
    # run the trap again, for ever.
    trap 'trap - INT TERM EXIT; kill 0' INT TERM EXIT
    (cd apps/api && {{dev_env}} go tool wgo run ./cmd/api) &
    bun run --filter @doelab/web dev &
    wait

# A fresh database every time, on purpose: feeder time is anchored at the
# start of today (UTC), so what an earlier day wrote would lie in this day's
# future. Ctrl-C stops everything; the data stays until the next `just demo`
# or `just nuke`.
#
# the whole demo on a fresh database: API, web UI, engine and simulated devices
demo: nuke up data import
    #!/usr/bin/env zsh
    # The trap is taken off first: `kill 0` signals this shell too, and would
    # run the trap again, for ever.
    trap 'trap - INT TERM EXIT; kill 0' INT TERM EXIT
    (cd apps/api && {{dev_env}} go run ./cmd/api) &
    bun run --filter @doelab/web dev &
    tries=0
    until curl -sf -X POST -H 'content-type: application/json' -d '{"service":""}' \
        http://localhost:3100/grpc.health.v1.Health/Check | grep -q '"SERVING_STATUS_SERVING"'; do
      (( ++tries > 240 )) && { echo "the API did not come up on :3100" >&2; exit 1; }
      sleep 0.5
    done
    # A day of envelopes first, so that no device starts without one.
    (cd apps/api && {{dev_env}} go run ./cmd/engine -once)
    (cd apps/api && {{dev_env}} go run ./cmd/engine) &
    (cd apps/api && {{dev_env}} go run ./cmd/dersim -rogue 0.04 -flaky 0.04) &
    echo "doe-lab is running: http://localhost:5273 (operator token: dev-operator-token). Ctrl-C stops it."
    wait

# the production build of the web UI on :4273 (needs the API up)
preview: build-web
    bun run --filter @doelab/web preview

# Needs the API up, and `just import` done.
#
# one engine run: a day of envelopes for every enrolled site
engine *args:
    cd apps/api && {{dev_env}} go run ./cmd/engine -once "$@"

# the engine on its schedule: a run now, and another every two intervals
engine-loop *args:
    cd apps/api && {{dev_env}} go run ./cmd/engine "$@"

# Needs the API up, `just import` done, and envelopes from `just engine`.
#
# the simulated devices: one virtual inverter per site, some of them misbehaving
dersim *args:
    cd apps/api && {{dev_env}} go run ./cmd/dersim -rogue 0.04 -flaky 0.04 "$@"
