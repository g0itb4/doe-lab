#!/usr/bin/env bash
# Build locally, ship over ssh, migrate, switch, check.
#
# Usage: just deploy user@host        (or: bash scripts/deploy.sh user@host)
#
# The server needs no toolchain: no go, no bun, no git. What it needs, once,
# is the playbook in infra/prod/ansible: Postgres with TimescaleDB, Caddy, the
# three systemd units, /etc/doelab/doelab.env, and /srv/doelab owned by the
# deploy user.
#
# A release lands in /srv/doelab/releases/<utc-timestamp>-<sha>/ and the
# `current` symlink points at the live one. A rollback is the symlink pointed
# back and a restart; the script does that itself when the health check
# fails. Migrations only go forward, so a rollback never touches the schema.
set -euo pipefail

HOST=${1:?usage: deploy.sh user@host}
# amd64 for a DigitalOcean droplet; GOARCH=arm64 for an arm box.
ARCH=${GOARCH:-amd64}
SHA=$(git rev-parse --short HEAD)
REL="$(date -u +%Y%m%d%H%M%S)-$SHA"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

cd "$ROOT"

echo "==> building web"
bun run --filter @doelab/web build

echo "==> building api, engine, dersim, migrate and import (linux/$ARCH)"
for cmd in api engine dersim migrate import; do
	(cd apps/api &&
		CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" \
			go build -trimpath -ldflags='-s -w' -o "dist/linux/$cmd" "./cmd/$cmd")
done

echo "==> shipping release $REL"
ssh "$HOST" "mkdir -p /srv/doelab/releases/$REL"
rsync -az apps/api/dist/linux/ "$HOST:/srv/doelab/releases/$REL/"
rsync -az --delete apps/web/dist/ "$HOST:/srv/doelab/releases/$REL/web/"
rsync -az --delete packages/db/migrations/ \
	"$HOST:/srv/doelab/releases/$REL/migrations/"
ssh "$HOST" "echo DOELAB_VERSION=$SHA > /srv/doelab/releases/$REL/version.env"

echo "==> migrating, switching, restarting"
# A quoted heredoc: nothing in it expands here; $REL arrives as $1.
ssh "$HOST" bash -s -- "$REL" <<'REMOTE'
set -euo pipefail
REL=$1
# 640 root:deploy, so that this step can read DATABASE_URL without sudo.
set -a; source /etc/doelab/doelab.env; set +a

"/srv/doelab/releases/$REL/migrate" -path "/srv/doelab/releases/$REL/migrations" up

PREV=$(readlink /srv/doelab/current || true)
ln -sfn "/srv/doelab/releases/$REL" /srv/doelab/current
sudo systemctl restart doelab-api

# The exact status, in its quotes: "SERVING" alone is also a substring of
# SERVING_STATUS_NOT_SERVING, and a check that greps for it passes a server
# that has just said it is not serving.
healthy() {
	curl -sf -X POST -H 'content-type: application/json' -d '{"service":""}' \
		http://127.0.0.1:3100/grpc.health.v1.Health/Check |
		grep -q '"SERVING_STATUS_SERVING"'
}

for _ in $(seq 1 20); do
	sleep 1
	if healthy; then
		# The engine and the devices are clients of the API: restarted once it
		# answers, so that they start against the new release.
		sudo systemctl restart doelab-engine
		sudo systemctl restart doelab-dersim
		echo "==> healthy: $REL"
		exit 0
	fi
done

echo "==> health check failed, rolling back to ${PREV:-nothing}" >&2
if [ -n "$PREV" ]; then
	ln -sfn "$PREV" /srv/doelab/current
	sudo systemctl restart doelab-api
fi
exit 1
REMOTE

echo "==> pruning old releases (keeping 5)"
ssh "$HOST" 'ls -1dt /srv/doelab/releases/* | tail -n +6 | xargs -r rm -rf'
