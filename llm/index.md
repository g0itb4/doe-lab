# doe-lab: index for agents

Dynamic operating envelopes on a fleet of low-voltage feeders, each below a zone substation. `apps/api` is one Go module: the API, the engine and the device simulator. `apps/web` is the operator UI.
Each entry names where a thing starts. Read those files, and the document named with them, before you edit.
This directory is documentation for agents. The adapter for the language model is `apps/api/internal/repo/llm/`.

## Rules a hook or CI enforces

- Run tools through `just`, never with hand-picked flags. `just check` runs every hook, as CI does.
- Never edit generated code: `apps/api/gen/`, `apps/api/internal/repo/pg/gen/`, `packages/gen/src/gen/`, `docs/openapi/`. Change `proto/doelab/v1/` or `packages/db/queries/`, run `just gen`, and commit the diff in `docs/openapi/`.
- The schema is `packages/db/migrations/`. Go structs and proto resources mirror it, and `just test-db` fails when they drift.
- Imports run controller → service → domain ← repo; depguard in `.golangci.yml` checks them. A new Go package needs a rule there and a floor in `apps/api/coverage.json`.
- A coverage floor only goes up. `apps/api/internal/engine/` and `apps/api/internal/domain/` stay at 100%.
- Shape is validated by CEL rules in `proto/doelab/v1/`, meaning in `apps/api/internal/service/`. Controllers validate nothing.
- `apps/web/src/lib/config-rules.ts` mirrors the proto rules, and a test fails when they differ. Change the proto file first.
- Nothing under `data/raw/` is committed. A file derived from the CSIRO feeder is listed in `data/derived/LICENSE`.

The reasons, and the rest of the rules: `CONTRIBUTING.md`.

## Features

For a Go feature the path is its service or package. Its handlers are in `apps/api/internal/controller/`, its messages in `proto/doelab/v1/`. Verify Go with `just cover-go` unless the entry says otherwise.

- **Envelope engine**: 4-wire power flow and envelope search; pure, checked against OpenDSS. `apps/api/internal/engine/`. Also `just bench`. Fixtures change only through `just fixtures`.
- **Engine runner**: the engine as a client of the API, on a schedule. `apps/api/internal/enginerun/`, `apps/api/cmd/engine/main.go`.
- **API server**: ConnectRPC wiring, interceptors, bearer tokens per scope. `apps/api/internal/server/server.go`, `apps/api/internal/interceptor/`, `apps/api/internal/auth/auth.go`.
- **Envelope dispatch**: idempotent publish, and a stream to each device. `apps/api/internal/service/envelopes.go`, `apps/api/internal/repo/pgbus/pgbus.go`.
- **Telemetry and fleet view**: readings in, the `WatchFleet` stream out. `apps/api/internal/service/telemetry.go`.
- **Compliance and alerts**: a site over its limit, a silent device. `apps/api/internal/service/compliance.go`, `apps/api/internal/service/alerts.go`.
- **Backstop**: an operator's override of every envelope. `apps/api/internal/service/backstops.go`, `apps/web/src/lib/components/BackstopControl.svelte`.
- **Envelope configs**: engine settings with version history. `apps/api/internal/service/envelope_configs.go`, `apps/web/src/lib/components/ConfigForm.svelte`.
- **Engine runs, reports, CSV export**: `apps/api/internal/service/envelope_runs.go`, `apps/api/internal/service/report.go`, `apps/api/internal/service/export.go`, `apps/api/internal/repo/objstore/objstore.go`.
- **Retention**: old history removed on a timer, counted in feeder time. `apps/api/internal/service/retention.go`.
- **Importer**: the fleet of `data/fleet/` (substations, feeders, sites with DER) and the Ausgrid profiles into the database. `apps/api/cmd/import/main.go`, `apps/api/internal/service/importer.go`, `apps/api/internal/fleet/fleet.go`, `apps/api/internal/ausgrid/ausgrid.go`.
- **Fleet map**: substations and located sites on a street map, from `GetFleetState`. `apps/web/src/routes/map/+page.svelte`, `apps/web/src/lib/map/fleet.ts`, `apps/web/src/lib/components/FleetMap.svelte`, `apps/api/internal/service/telemetry.go`. Verify: `just cover-web`, `just e2e`.
- **Network schematic**: a feeder drawn by cable distance, with the voltage at each bus and the flow in each line that the engine records. `apps/web/src/routes/network/+page.svelte`, `apps/web/src/lib/map/layout.ts`, `apps/web/src/lib/map/schematic.ts`, `apps/api/internal/engine/report.go`, `packages/db/migrations/20261002000002_feeder_states.up.sql`.
- **Limit against reading**: one comparison of a reading with its limit, in words and as a bar, for the overview, the map and a site's page. `apps/web/src/lib/limit.ts`, `apps/web/src/lib/components/LimitMeter.svelte`. Verify: `just cover-web`, `just e2e`.
- **dersim**: simulated inverters, some rogue or flaky, as clients of the API. `apps/api/internal/dersim/`.
- **Demo clock**: feeder time at a multiple of the wall clock. `apps/api/internal/simclock/simclock.go`, `apps/web/src/lib/clock.svelte.ts`.
- **Assistant**: a language model with four read-only lookups, a rate limit and a daily budget. `apps/api/internal/service/assistant.go`, `apps/api/internal/repo/llm/llm.go`, `apps/web/src/lib/components/AssistantDrawer.svelte`. A lookup never writes.
- **Persistence**: `packages/db/migrations/`, `packages/db/queries/`, `apps/api/internal/repo/pg/`. `apps/api/internal/repo/mem/` is the in-memory store of the unit tier, and one suite runs against both. Verify: `just lint`, `just test-db`.
- **Web UI**: pages in `apps/web/src/routes/` (overview, map, network, sites, one site, operations, config); logic and components in `apps/web/src/lib/`. Verify: `just cover-web` for `apps/web/src/lib/`, `just e2e` for a page. Read "Web app" in `CONTRIBUTING.md` first.
- **Observability**: metrics, traces, a Grafana dashboard. `apps/api/internal/obs/obs.go`, `infra/obs/`.
- **Deploy**: one server, set up with Ansible. `scripts/deploy.sh`, `infra/prod/README.md`.
- **Demo**: `just demo` runs the whole stack on a fresh database. Every recipe is in `justfile`.

## Documents

- `CONTRIBUTING.md`: layering, validation, hook rules, test tiers, web rules, licences, naming.
- `README.md`: quick start, commands, how the parts fit, measured results.
- `docs/model.md`: what the model assumes, and what it is not.
- `docs/ux-audit.md`: each UX rule and the test that checks it.
- `docs/load-test.md`: the dispatch load test, its method and limits.
- `packages/db/README.md`: schema conventions, how to add a field, the entity diagram.
- `infra/prod/README.md`: first deploy, rollback, operating the server.
- `.env.example`: every environment variable, with its default.
