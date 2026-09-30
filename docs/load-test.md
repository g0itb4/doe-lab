# Load test: envelope dispatch

How long does an envelope take to reach a device when many devices are
listening, and what does it cost the API to hold their subscriptions open?

Measured on 30 September 2026.

## Result

| Subscriptions | Dispatches timed | p50    | p90    | p99    | Max    | Failed | API memory (RSS) | Go heap in use | Goroutines |
| ------------- | ---------------- | ------ | ------ | ------ | ------ | ------ | ---------------- | -------------- | ---------- |
| 1,000         | 3,000            | 31 ms  | 64 ms  | 78 ms  | 80 ms  | 0      | 112 MB           | 61 MB          | 1,193      |
| 5,000         | 15,000           | 166 ms | 284 ms | 338 ms | 348 ms | 0      | 292 MB           | 115 MB         | 5,225      |
| 10,000        | 40,000           | 330 ms | 644 ms | 739 ms | 768 ms | 0      | 461 MB           | 191 MB         | 10,265     |

With no load subscriptions (56 from the simulated devices) the API held
41 MB and 183 goroutines.

At 10,000 subscriptions every envelope reached its subscriber within 0.77 s
of being written, and none of the subscriptions failed.

## What is measured

The latency is from the write of an envelope to its arrival at the
subscriber: `created_at` of the envelope, which the database sets when the
publish commits, to the moment the client has decoded the message. It
covers the commit, the notification through Postgres `LISTEN/NOTIFY`, the
subscription's read of the envelope in force, and the send over HTTP/2.

Only a dispatch is timed: an envelope that replaces the one a subscriber
already holds for the same interval. An envelope that arrives because the
interval turned was written long before, and is not counted.

The API's own histogram, `doelab_envelope_dispatch_latency_seconds`,
measures the same path up to the send. With only the 56 simulated devices
subscribed its p99 was 50 ms.

## Method

Everything ran on one laptop: the API, Postgres, the engine, the simulated
devices and the load generator.

| Part      | Detail                                                                                              |
| --------- | --------------------------------------------------------------------------------------------------- |
| Machine   | MacBook Air, Apple M2, 8 cores, 8 GB, macOS 15.7                                                    |
| Go        | 1.27.1                                                                                              |
| Database  | PostgreSQL 17.11 with TimescaleDB, in a podman VM with 4 vCPU and 3.6 GB                            |
| API       | development mode with request logging on, 16 database connections, one process                      |
| Feeder    | LV10: 94 sites, 56 of them with envelopes                                                           |
| Clock     | feeder time at 60 times the wall clock, half-hour intervals                                         |
| Engine    | `just engine-loop`: a run every two intervals, which is every 60 s here; 2,688 envelopes per run    |
| Devices   | `just dersim`: 76 devices on 56 sites, reporting once a second                                      |
| Load      | `dersim -load N -rate 500 -hold 150s`: N subscriptions spread over the 56 sites, 500 opened a second |

Each engine run replaces the envelope in force at every site, so every
subscriber receives one dispatch per run. A hold of 150 s sees two to four
runs.

To repeat it:

```sh
just up && just import            # once
# The API's rate limit is per client address, and the load comes from one.
cd apps/api && DOELAB_ENV=development METRICS_ADDR=127.0.0.1:9464 \
  DEMO_CLOCK_SPEED=60 DEMO_CLOCK_ANCHOR=$(date -u +%Y-%m-%dT00:00:00Z) \
  RATE_LIMIT_PER_SECOND=2000 RATE_LIMIT_BURST=20000 go run ./cmd/api
just engine-loop                  # in a second terminal
just dersim                       # in a third
cd apps/api && DOELAB_ENV=development go run ./cmd/dersim -load 10000 -rate 500 -hold 150s
```

The last command prints the report and the API's memory, which it reads
from the API's metrics page.

## Reading the numbers

- **The latency grows in step with the number of subscribers.** Every
  subscription reads its envelope from the database when it is woken, and
  the 16 connections of the pool serve those reads in turn: 10,000 reads
  take about 0.7 s. Nothing is lost while they queue; a subscriber that is
  woken twice reads once.
- **The read path is loaded as a real fleet of this size would load it.**
  The 10,000 subscribers share 56 sites, but the API does not share a read
  between the subscribers of one site: the test makes 10,000 reads per run,
  as 10,000 sites with one device each would. The write path is lighter
  than theirs: a run here writes 2,688 envelopes, not 480,000.
- **Memory is about 42 kB per subscription**: a goroutine, an HTTP/2 stream
  and its buffers.
- **What would move the number.** A larger pool, a read that fetches the
  envelopes of many woken sites in one query, or carrying the envelope in
  the notification. None is needed at this size, and none is built.

## Limits of this test

- One machine: the load generator and the database compete with the API for
  the same eight cores, and there is no network between them.
- The subscribers only listen. The telemetry of 76 devices is in the
  background; the ingest path is not under load here.
- Subscriptions are opened at 500 a second. A fleet that reconnects all at
  once after an outage is a different test; the devices' reconnect backoff
  and jitter exist for that case, and the API's rate limit bounds it.
