# Working in this repo

doe-lab is a public demo of dynamic operating envelopes (DOEs) on a real
Australian low-voltage feeder: an engine computes export and import limits, a
ConnectRPC API stores and dispatches them, simulated inverters obey them, and
a web UI shows operators what is happening.

## Commands

Everything goes through `just` from the repo root. `just` alone lists recipes.

```sh
just hooks        # install the git hooks (once per clone)
just check        # every hook of both stages on every file, as CI does
just fmt          # rewrite formatting in place
just lint         # every linter
just cover-go     # Go unit tier with -race, then the per-package coverage gate
just cover-web    # web unit and component tests in headless Chromium, with thresholds
just e2e          # the built web app in Playwright: smoke, keyboard, axe (WCAG 2.2 AA)
just dev          # the database, the API and the web UI, in watch mode
just data         # download the raw datasets and verify their checksums
```

Do not call `go test`, `golangci-lint`, `buf` or `prettier` with hand-picked
flags in a hook or in CI. Add or change a `just` recipe, so the hooks, CI and
a developer's terminal run the same command.

## Layering

One Go module (`apps/api`, module path `doelab/api`), three binaries (`api`,
`engine`, `dersim`). The dependency arrow points inwards:

```
controller  →  service  →  domain  ←  repo
```

| Layer      | Package                 | Does                                                                                               | Must not import                                                      |
| ---------- | ----------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------- |
| Controller | `internal/controller`   | ConnectRPC handlers, proto ↔ domain mapping (`internal/protomap`), stream lifecycle                 | `internal/repo`, pgx, the AWS SDK, the model SDK. No business rules, no SQL |
| Service    | `internal/service`      | Business rules. Declares the interfaces it needs in `service/ports.go`                             | generated proto (`doelab/api/gen`), Connect, `internal/repo`, sqlc, pgx, the AWS SDK, the model SDK |
| Domain     | `internal/domain`       | Plain types and domain errors                                                                      | everything above, and anything that does I/O                         |
| Repository | `internal/repo/{pg,objstore,bus,llm}` | Implements the service ports. Maps Postgres error codes, and a model provider's failures, to domain errors | No business rules                                         |
| Engine     | `internal/engine`       | Power flow and envelope search. Pure: no I/O, no clock, no globals                                  | any other package of this module, `os`, `net`, `time`, `database/sql` |
| Clients    | `internal/enginerun`, `internal/dersim` | The engine and the simulated devices as clients of the API: they call it over RPC | `internal/service`, `internal/controller`, `internal/repo`, pgx |

These rules are lint, not convention: `depguard` in `.golangci.yml` fails the
pre-commit hook when a forbidden import appears. When you add a layer or a
package, add its rule in the same commit.

`cmd/engine` and `cmd/dersim` talk to the API over RPC only. They never open
the database.

The assistant follows the same split: `service.Assistant` owns the
conversation (the brief, the lookups, the rations), `service.Model` is the
port, and `internal/repo/llm` is the only package that imports the provider's
SDK. A lookup only reads. Do not add one that writes.

## Validation is split in two

- **Shape, in the controller layer**: protovalidate CEL rules in the `.proto`
  files, run by an interceptor. Example: an NMI matches
  `^[A-Z0-9]{10}[0-9]?$`, export ≥ 0, `valid_to > valid_from`. Controllers
  contain no hand-written validation.
- **Meaning, in the service layer**: the NMI checksum, that the site exists,
  no overlap with an active backstop, idempotency.

Errors map in two hops, never in a controller: `internal/repo` turns Postgres
codes into `domain` errors, and the errors interceptor turns `domain` errors
into Connect codes.

## Git hooks (prek)

`prek.toml` defines the hooks; `just hooks` installs them. They cover four
concerns:

| Concern  | Stage      | What runs                                                             |
| -------- | ---------- | --------------------------------------------------------------------- |
| Style    | pre-commit | whitespace and file checks, `gofumpt` + `goimports`                   |
| Lint     | pre-commit | `golangci-lint` (govet, staticcheck, errcheck, revive, gosec, depguard) |
| Secrets  | pre-commit | `gitleaks` on the staged diff, `detect-private-key`                   |
| Secrets  | pre-push   | `gitleaks` on the commits being pushed                                |
| Lint     | pre-commit | `svelte-check` on the web app                                         |
| Coverage | pre-commit | `just cover-go`: Go unit tier, then `covergate` against `apps/api/coverage.json` |
| Coverage | pre-push   | `just cover-web`: web tests, thresholds in `apps/web/vitest.config.ts` |

Rules:

- **Do not lower a coverage floor to make a commit pass.** Add the test. A
  floor in `apps/api/coverage.json` only goes up.
- `internal/engine/...` and `internal/domain` are held at 100%. Design for it:
  no unreachable defensive branches, and seams where a failure must be
  injected.
- A new Go package needs a floor in `coverage.json`, or `covergate` fails with
  "no floor".
- `//nolint` needs the linter's name and a reason (`nolintlint` enforces it).
- A real secret never goes in the repo, a fixture or `.env.example`. If
  gitleaks flags a fake value, add a narrow allowlist entry in `.gitleaks.toml`
  with a description, never a blanket path.
- `git commit --no-verify` is for work-in-progress commits on a branch. CI runs
  the same hooks, so skipping them only delays the failure.
- The pre-commit stage must stay under 15 s warm on a Go-only change.
  `just cover-go` prints its duration. A hook that breaks the budget moves to
  pre-push.

## Tests

`go test` runs in two tiers:

- **Unit tier (default)**: offline, no container. This is what the hook runs.
- **Container tier**: Postgres + TimescaleDB and MinIO through testcontainers,
  gated on `DOELAB_TEST_DB=1`, not on a build tag (`go mod tidy` drops
  dependencies that only appear behind custom tags).

The engine is checked against OpenDSS. `tools/opendss_snapshots.py` solves the
feeder in OpenDSS and writes node voltages to
`apps/api/internal/engine/testdata/lv10_*.json`; the Go solver must match them
within the tolerance fixed in the test.

## Web app

`apps/web` is SvelteKit with `adapter-static`: prerendered at build time, no
server at run time. It reaches the API at `/rpc`.

- Logic lives in `src/lib` as plain modules and small components, and is
  tested there (`just cover-web`). The pages in `src/routes` wire them
  together and are tested end to end against the build (`just e2e`), with
  the API mocked in `e2e/mock.ts`.
- Every colour is a design token in `src/app.css`. A new pair of colours
  that is used together goes into `tokens.svelte.test.ts`, which measures
  its contrast in both themes.
- Every data view has four states: loading (a `Skeleton` of the final
  size), empty, error (plain words and a retry) and stale. Use `Resource`
  for a fetch and `Live` for a stream; they provide the states.
- The state of a view (range, zoom, filter) lives in the address, through
  `withQuery` and `queryParam`. Do not read `page.url.searchParams`
  directly: it throws while a page is prerendered.
- A number is shown with its unit through `format.ts`; a time in the
  feeder's zone.
- The form rules of `/config` mirror the protovalidate rules, and
  `config-rules.test.ts` fails when they differ. Change the `.proto` first.
- `docs/ux-audit.md` maps each UX rule to the test that checks it. A new
  rule needs a check; a changed page needs `just e2e`.

## Data and licences

- Source code: PolyForm Noncommercial 1.0.0 (`LICENSE`): any use but a
  commercial one.
- **Anything derived from the CSIRO feeder is CC BY-NC-SA 4.0**: files in
  `data/derived/`, the fixtures `apps/api/internal/engine/testdata/lv10_*.json`,
  and the database seed. List such files in `data/derived/LICENSE`.
- **Raw data is never committed.** `data/raw/` is ignored, and the
  `check-added-large-files` hook is the second guard. `just data` downloads it
  and verifies `data/SHA256SUMS`.
- A new raw file needs its checksum in `data/SHA256SUMS` and its source,
  licence and attribution text in `data/SOURCES.md`.
- NMIs are synthetic. Never use a real customer identifier.

## Conventions

- British spelling in prose, comments and identifiers we own ("licence",
  "colour"); protocol and library names keep theirs.
- Field names on the wire follow CSIP-AUS / IEEE 2030.5 `DERControl`
  (`opModExpLimW`, `opModImpLimW`). The UI uses operator words ("export
  limit").
- Units are in the name when a type cannot carry them: `ExportW`, `LengthKm`,
  `VoltagePU`.
- Time is always UTC in storage and on the wire. The feeder's zone
  (Australia/Sydney) is applied at the display edge.
