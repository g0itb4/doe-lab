# Production

One server, no containers. The repo produces four things to run (three Go
binaries and a directory of prerendered pages), so production is those behind
Caddy, supervised by systemd, built on a laptop and shipped with rsync:

```
                      ┌───────────────────────── server ─────────────────────────┐
  browser ── https ──►│ Caddy :443   TLS, static pages, /rpc/* ─► h2c ─┐          │
                      │                                                 ▼          │
                      │   doelab-engine ── RPC ──►  doelab-api :3100 (127.0.0.1)   │
                      │   doelab-dersim ── RPC ──►        │         └ :9464 metrics │
                      │                                    ▼                        │
                      │                  Postgres 17 + TimescaleDB (localhost)     │
                      └────────────────────────────────────┬─────────────────────┘
       laptop ── just deploy deploy@host ──► rsync         └──► object store (S3)
```

Two commands own the two halves:

| Command | Owns |
| ------- | ---- |
| `ansible-playbook` in [`ansible/`](ansible/) | **The server**: packages, the deploy user, the firewall, Postgres with TimescaleDB, Caddy, the systemd units, the environment file. Safe to run again |
| `just deploy deploy@host` ([`scripts/deploy.sh`](../../scripts/deploy.sh)) | **Each release**: builds the web app and cross-compiles the binaries here, ships them to `/srv/doelab/releases/<time>-<sha>/`, runs the migrations, switches the `current` symlink, restarts, and checks the health endpoint. Switches back when the check fails |

The server needs no Go, no bun and no git.

**Size.** 2 vCPU and 2 GB (a DigitalOcean droplet in `syd1` fits). Postgres
holds about 1.5 GB at the default retention (a year of profiles and the time series), the API is capped
at 512 MB, and the engine and the devices at 256 MB each. The playbook adds
2 GB of swap.

## First deploy

1. **Provision.** Ubuntu 24.04 with your ssh key. Point the domain's A and
   AAAA records at it now: Caddy cannot obtain a certificate until the name
   resolves.
2. **An object store.** A DigitalOcean Space (or any S3 bucket) and a key
   for it. Exports are written there.
3. **Configure.** In `ansible/`:

   ```sh
   cp inventory.example.ini inventory.ini    # the host
   cp vars.example.yml vars.yml              # the domain, the secrets, the clock
   ansible-galaxy collection install ansible.posix community.general community.postgresql
   ansible-playbook -i inventory.ini playbook.yml
   ```

   Both copies are ignored by git. Set `demo_clock_anchor` to today.
4. **Deploy**, from the laptop: `just deploy deploy@<domain>`. The first run
   applies every migration to the empty database and starts the API. The
   engine and the devices start, find no feeder, and wait.
5. **Import the feeder**, once. The raw data is not in the repo; fetch it
   here, copy the two inputs over, and run the import on the server:

   ```sh
   just data
   rsync -az data/raw/csiro/LV/LV10_223bus data/raw/ausgrid/Ausgrid_solar_home_data.zip \
     deploy@<domain>:/srv/doelab/data/
   ssh deploy@<domain> 'set -a; . /etc/doelab/doelab.env; set +a;
     /srv/doelab/current/import -feeder /srv/doelab/data/LV10_223bus \
       -ausgrid /srv/doelab/data/Ausgrid_solar_home_data.zip'
   ssh deploy@<domain> 'sudo systemctl restart doelab-engine; sudo systemctl restart doelab-dersim'
   ```

   The import is safe to run again: it changes nothing the second time.
6. **Verify.**

   ```sh
   curl -X POST -H 'content-type: application/json' -d '{"service":""}' \
     https://<domain>/grpc.health.v1.Health/Check       # {"status":"SERVING_STATUS_SERVING"}
   curl -sI https://<domain>/ | grep -i cache-control    # no-cache
   ```

   Then open the site: the overview shows live figures within a minute, and
   the charts fill as the engine runs.

## The clock

The demo runs feeder time faster than the wall clock (`demo_clock_speed`, 60
by default), starting from `demo_clock_anchor`. Two things follow.

- **The anchor must not change on a server that has data.** Feeder time is
  computed from it; moving it moves "now" away from every envelope and
  reading already stored.
- **A fast clock cannot run for ever.** At a speed of 60, feeder time gains
  59 days on the wall clock every day. The API refuses to start once feeder
  time is more than 50 years from the wall clock, which is after about ten
  months. Before then, start again: stop the three units, drop and recreate
  the database (`sudo -u postgres dropdb doelab`, then the playbook), set a
  new anchor in `vars.yml`, run the playbook, deploy, and import.

A speed of 1 has neither limit: feeder time is then the wall clock.

## What is kept

The API removes old history every ten minutes, in feeder time
(`retention_*_days` in `vars.yml`). At a clock speed of 60 on LV10 the
defaults hold the database at about 1.5 GB.

There is no backup job, on purpose: the feeder and its profiles are rebuilt
by the import, and the envelopes, readings and alerts by letting the engine
and the devices run. What a rebuild loses is the config versions an
operator has saved in the UI. If those matter, dump one table:
`pg_dump -t envelope_configs doelab`.

## Rollback

`scripts/deploy.sh` rolls back by itself when the health check fails after a
restart. By hand, on the server:

```sh
ln -sfn /srv/doelab/releases/<previous> /srv/doelab/current
sudo systemctl restart doelab-api
sudo systemctl restart doelab-engine
sudo systemctl restart doelab-dersim
```

Binaries and static files only. Migrations only go forward and are never
rolled back.

## Operating it

| To | Do |
| -- | -- |
| See the logs | `journalctl -u doelab-api -f` (JSON lines; a request's lines share a `trace_id`) |
| See the metrics | `curl -s 127.0.0.1:9464/metrics` on the server. They are not exposed through Caddy |
| Change a setting | Edit `vars.yml`, run the playbook, restart the unit |
| Trigger or clear a backstop | The Operations page, with `operator_token` |

## What this deliberately is not

No Docker on the server (four artifacts do not need an image), no deploy from
CI (a laptop and an ssh key are the pipeline), no managed Postgres, and one
API process: the envelope bus already uses Postgres `LISTEN/NOTIFY`, so a
second instance behind the same Caddy would stay in step, but nothing here
needs one.
