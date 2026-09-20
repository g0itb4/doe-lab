set shell := ["zsh", "-cu"]

# So a recipe can forward its arguments with "$@" and keep the caller's quoting.
set positional-arguments := true

# list recipes
default:
    @just --list

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

# rewrite files in place: gofumpt + goimports
fmt:
    cd apps/api && golangci-lint fmt

# fail on a formatting diff, without rewriting (the pre-commit hook)
fmt-check: fmt-check-go

[private]
fmt-check-go:
    cd apps/api && golangci-lint fmt --diff

# ── lint ─────────────────────────────────────────────────────────────────────

# every linter
lint: lint-go

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
# Go unit tests with the race detector, then the per-package coverage gate
cover-go:
    #!/usr/bin/env zsh
    set -eu
    zmodload zsh/datetime
    cd apps/api
    mkdir -p coverage
    start=$EPOCHREALTIME
    go test -short -race -coverprofile=coverage/unit.coverprofile ./... >coverage/unit.log 2>&1 \
      || { cat coverage/unit.log; exit 1 }
    go run ./cmd/covergate
    printf 'cover-go: %.1f s\n' $(( EPOCHREALTIME - start ))

# every Go test of the offline tier, including the slow ones, with the race detector
test-go:
    cd apps/api && go test -race ./...

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
