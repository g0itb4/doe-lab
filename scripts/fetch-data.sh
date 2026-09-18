#!/usr/bin/env bash
# Download the raw datasets into data/raw/ and check them against
# data/SHA256SUMS. Files already on disk with the right checksum are kept.
#
#   scripts/fetch-data.sh           download what is missing, then verify
#   scripts/fetch-data.sh --verify  verify only, no network
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
raw="$root/data/raw"
sums="$root/data/SHA256SUMS"

verify() {
  (cd "$raw" && shasum -a 256 --quiet -c "$sums")
  echo "data: $(wc -l <"$sums" | tr -d ' ') files match data/SHA256SUMS"
}

if [[ "${1:-}" == "--verify" ]]; then
  verify
  exit 0
fi

mkdir -p "$raw/ausgrid" "$raw/csiro"

# True when the file exists and its checksum is the one in SHA256SUMS.
have() {
  local want
  want="$(awk -v f="$1" '$2 == f { print $1 }' "$sums")"
  [[ -n "$want" && -f "$raw/$1" ]] &&
    [[ "$(shasum -a 256 "$raw/$1" | cut -d' ' -f1)" == "$want" ]]
}

ausgrid="ausgrid/Ausgrid_solar_home_data.zip"
if ! have "$ausgrid"; then
  echo "data: downloading $ausgrid"
  curl -fSL --retry 3 -o "$raw/$ausgrid" \
    "https://pierreh.eu/downloads/Ausgrid_solar_home_data.zip"
fi

# The CSIRO portal lists each file with a short-lived presigned link.
listing="$(mktemp)"
trap 'rm -f "$listing"' EXIT
curl -fsSL --retry 3 -H 'Accept: application/json' -o "$listing" \
  "https://data.csiro.au/dap/ws/v2/collections/65408/data"

python3 - "$listing" <<'PY' | while IFS=$'\t' read -r name url; do
import json, sys
for f in json.load(open(sys.argv[1]))["file"]:
    print(f["filename"], f["presignedLink"]["href"], sep="\t")
PY
  if ! have "csiro/$name"; then
    echo "data: downloading csiro/$name"
    mkdir -p "$(dirname "$raw/csiro/$name")"
    curl -fsSL --retry 3 -o "$raw/csiro/$name" "$url"
  fi
done

verify
