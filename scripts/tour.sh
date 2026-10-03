#!/usr/bin/env bash
# Records the tour of the UI that the README shows, as two short clips:
# docs/img/tour-fleet.gif (the fleet, and one of its feeders through time)
# and docs/img/tour-network.gif (that feeder's network). One take, cut in two:
# a clip is short enough to watch, and light enough to keep.
#
# It brings up a whole stack of its own, beside whatever is running: a
# database of its own in the dev Postgres, the API, the engine and the
# simulated devices on ports of their own, and the production build of the
# web app. It never touches the dev database, and it takes everything down
# again when it ends, however it ends.
#
# Feeder time is set so that the take is made at midday on the feeders, when
# the sun is up and there is something to limit. The pointer on the recording
# is moved as a hand moves one: apps/web/tour/human-path.ts is the model, and
# the take is checked against it before the GIF is kept.
#
# Needs the dev Postgres up (`just up`), the raw data (`just data`), ffmpeg,
# and a network connection for the map's street tiles.
#
#   scripts/tour.sh               record, encode, and replace the two clips
#   TOUR_SEED=7 scripts/tour.sh   another take: the same story, other moves
#   TOUR_FPS=12.5 TOUR_COLOURS=64 scripts/tour.sh   a smaller file, less smooth
set -euo pipefail
cd "$(dirname "$0")/.."

DB=doelab_tour
API_PORT=3199
WEB_PORT=5373
METRICS_PORT=9465
POSTGRES=doelab-postgres-1
OUT=apps/web/test-results/tour
LOG=apps/web/test-results/tour-stack.log
DOCS=docs/img
CLIPS=(tour-fleet tour-network)
# The most one clip may weigh. A hand that moves smoothly needs the frames,
# and the readouts that follow it on three charts change a part of every one:
# at these settings the clip of the fleet and its feeder is about 1.3 MB for
# 17 s, and the clip of the network about 0.7 MB for 15 s. Every new take is
# two more files in the repo's history, so record one when the UI has
# changed, not for the sake of it.
MAX_KB="${TOUR_MAX_KB:-1536}"
# Frames a second, and colours: the two that the size answers to most. At
# 12.5 and 64 the two clips are about 0.9 and 0.5 MB.
FPS="${TOUR_FPS:-50/3}"
COLOURS="${TOUR_COLOURS:-128}"
WIDTH=832
# How long the last frame of a clip stands before it begins again, in
# hundredths of a second: the eye has it before the loop.
HOLD=160

die() {
  echo "tour: $*" >&2
  exit 1
}

command -v ffmpeg >/dev/null || die "ffmpeg is not installed"
podman ps --format '{{.Names}}' | grep -qx "$POSTGRES" || die "the dev database is not up: run \`just up\` first"
[ -f data/raw/ausgrid/Ausgrid_solar_home_data.zip ] || die "the raw data is missing: run \`just data\` first"
for port in "$API_PORT" "$WEB_PORT" "$METRICS_PORT"; do
  if lsof -ti:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
    die "port $port is in use; the tour needs it"
  fi
done

psql() {
  podman exec "$POSTGRES" psql -U doelab -d postgres -v ON_ERROR_STOP=1 -q -c "$1"
}

# The binaries are built first and run as themselves: `go run` would start
# each as a child that a kill of `go run` leaves behind, and the engine and
# the devices listen on no port to be found by.
BIN="$(mktemp -d)"
pids=()
cleanup() {
  trap - EXIT INT TERM
  for pid in "${pids[@]:-}"; do
    [ -n "$pid" ] && kill "$pid" 2>/dev/null || true
  done
  lsof -ti:"$API_PORT","$WEB_PORT","$METRICS_PORT" -sTCP:LISTEN 2>/dev/null | xargs -r kill 2>/dev/null || true
  sleep 1
  rm -rf "$BIN"
  psql "DROP DATABASE IF EXISTS $DB WITH (FORCE)" || echo "tour: could not drop the database $DB" >&2
}
trap cleanup EXIT INT TERM

(cd apps/api && go build -o "$BIN/" ./cmd/migrate ./cmd/import ./cmd/api ./cmd/engine ./cmd/dersim)

psql "DROP DATABASE IF EXISTS $DB WITH (FORCE)"
psql "CREATE DATABASE $DB"

export DOELAB_ENV=development
export DATABASE_URL="postgres://doelab:doelab@localhost:5436/$DB?sslmode=disable"
export PORT="$API_PORT"
export METRICS_ADDR="127.0.0.1:$METRICS_PORT"
export API_URL="http://localhost:$API_PORT"

echo "tour: a database of its own, and the fleet in it"
# What the stack says goes to a file: it says a line for every call.
mkdir -p "$(dirname "$LOG")"
: >"$LOG"
(cd apps/api && "$BIN/migrate" up) >>"$LOG" 2>&1
(cd apps/api && "$BIN/import" -skip-raw \
  -fleet ../../data/fleet -feeders ../../data/raw/csiro/LV \
  -ausgrid ../../data/raw/ausgrid/Ausgrid_solar_home_data.zip) >>"$LOG" 2>&1

# Feeder time runs at sixty times the wall clock from an anchor, where the two
# agree. The anchor that makes it half past seven this morning in Sydney now:
# midday is then a little over four minutes away, which the engine and the
# devices need to make a morning of data.
export DEMO_CLOCK_SPEED=60
DEMO_CLOCK_ANCHOR="$(python3 - <<'PY'
import datetime, zoneinfo
now = datetime.datetime.now(datetime.timezone.utc)
sydney = zoneinfo.ZoneInfo("Australia/Sydney")
start = now.astimezone(sydney).replace(hour=7, minute=30, second=0, microsecond=0)
anchor = (60 * now.timestamp() - start.timestamp()) / 59
print(datetime.datetime.fromtimestamp(anchor, datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"))
PY
)"
export DEMO_CLOCK_ANCHOR

echo "tour: the API on :$API_PORT, feeder time from 07:30 in Sydney"
(cd apps/api && exec "$BIN/api") >>"$LOG" 2>&1 &
pids+=($!)
tries=0
until curl -sf -X POST -H 'content-type: application/json' -d '{"service":""}' \
  "http://localhost:$API_PORT/grpc.health.v1.Health/Check" | grep -q '"SERVING_STATUS_SERVING"'; do
  tries=$((tries + 1))
  [ "$tries" -gt 240 ] && die "the API did not come up on :$API_PORT"
  sleep 0.5
done

# A day of envelopes first, so that no device starts without one.
(cd apps/api && "$BIN/engine" -once) >>"$LOG" 2>&1
(cd apps/api && exec "$BIN/engine") >>"$LOG" 2>&1 &
pids+=($!)
(cd apps/api && exec "$BIN/dersim" -rogue 0.04 -flaky 0.04) >>"$LOG" 2>&1 &
pids+=($!)

echo "tour: the web app, built, on :$WEB_PORT"
bun run --filter @doelab/web build >/dev/null
(cd apps/web && exec bunx vite preview --port "$WEB_PORT" --strictPort) >/dev/null 2>&1 &
pids+=($!)
tries=0
until curl -sf "http://localhost:$WEB_PORT/" >/dev/null; do
  tries=$((tries + 1))
  [ "$tries" -gt 120 ] && die "the web app did not come up on :$WEB_PORT"
  sleep 0.5
done

echo "tour: waiting for midday on the feeders, then the take"
(cd apps/web && TOUR_URL="http://localhost:$WEB_PORT" bunx playwright test -c playwright.tour.config.ts)

echo "tour: encoding"
# The frames at the times they were drawn, at a steady rate, the frames that
# repeat the last dropped; one palette for a whole clip, no dither, and only
# the part of each frame that changed.
filter="fps=$FPS,scale=$WIDTH:-1:flags=lanczos,mpdecimate,split[a][b];[a]palettegen=max_colors=$COLOURS:stats_mode=full[p];[b][p]paletteuse=dither=none:diff_mode=rectangle"
over=0
for clip in "${CLIPS[@]}"; do
  ffmpeg -hide_banner -loglevel error -y -f concat -safe 0 -i "$OUT/$clip.ffconcat" \
    -filter_complex "$filter" -fps_mode vfr -loop 0 -final_delay "$HOLD" "$OUT/$clip.gif"
  kb=$(( ($(wc -c <"$OUT/$clip.gif") + 1023) / 1024 ))
  echo "tour: $OUT/$clip.gif is $kb KiB (limit $MAX_KB)"
  [ "$kb" -gt "$MAX_KB" ] && over=1
done
# The whole take as a video, for a look at it frame by frame. Not committed.
ffmpeg -hide_banner -loglevel error -y -f concat -safe 0 -i "$OUT/frames.ffconcat" \
  -vf "fps=30,format=yuv420p" "$OUT/tour.mp4"

if [ "$over" -eq 1 ]; then
  die "a clip is over its limit. Both are kept in $OUT with tour.mp4, debug.png and notes.json. Try TOUR_COLOURS=64, TOUR_FPS=12.5, or a shorter story"
fi
for clip in "${CLIPS[@]}"; do
  cp "$OUT/$clip.gif" "$DOCS/$clip.gif"
done
echo "tour: $DOCS/tour-fleet.gif and $DOCS/tour-network.gif replaced. The take's notes and a picture of its moves are in $OUT"
