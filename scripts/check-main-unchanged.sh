#!/usr/bin/env bash
# Verify that the OSM steps never change the main feed: build it once without
# and once with all OSM steps and compare the two main outputs file by file
# (feed_info.txt is ignored, it contains the build time; transfers.txt is compared
# as a set of lines because the writer does not order its rows).
#
# Usage: scripts/check-main-unchanged.sh <feed-dir> <osm-features> <osm-routes> [work-dir]
set -euo pipefail
[ $# -ge 3 ] || { echo "usage: $0 <feed-dir> <osm-features> <osm-routes> [work-dir]" >&2; exit 2; }
feeds=$(realpath "$1"); features=$(realpath "$2"); routes=$(realpath "$3")
work=${4:-$(mktemp -d)}
mkdir -p "$work/a" "$work/b"
repo=$(cd "$(dirname "$0")/.." && pwd)

go build -o "$work/merger" "$repo/cmd/austria-gtfs-merger"
common=(--force --dir "$feeds" --drop-shapes --drop-erroneous --extended-route-types --stats "")

echo "== run A: rule-based steps only"
(cd "$work/a" && "$work/merger" "${common[@]}" --enrich steig,routes -o main.zip --enrich-report rep.json >/dev/null 2>&1)
echo "== run B: all steps with OSM data"
(cd "$work/b" && "$work/merger" "${common[@]}" --enrich all --osm-features "$features" --osm-routes "$routes" \
   --osm-output osm.zip --osm-sidecar-dir extras -o main.zip --enrich-report rep.json >/dev/null 2>&1)

python3 -I - "$work/a/main.zip" "$work/b/main.zip" <<'PY'
import sys, zipfile
a, b = zipfile.ZipFile(sys.argv[1]), zipfile.ZipFile(sys.argv[2])
names = sorted(set(a.namelist()) | set(b.namelist()))

def same(n):
    if n not in a.namelist() or n not in b.namelist():
        return False
    if n == "transfers.txt":
        # gtfswriter writes transfers in map order: compare as sets of lines
        return sorted(a.read(n).splitlines()) == sorted(b.read(n).splitlines())
    return a.read(n) == b.read(n)

diff = [n for n in names if n != "feed_info.txt" and not same(n)]
print("UNCHANGED" if not diff else "DIFFERENT: " + ", ".join(diff))
sys.exit(1 if diff else 0)
PY
echo "outputs in $work"
