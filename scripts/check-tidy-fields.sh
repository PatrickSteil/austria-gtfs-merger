#!/usr/bin/env bash
# Compare which enrichment fields survive a gtfstidy run.
# Usage: scripts/check-tidy-fields.sh before.zip after.zip
set -euo pipefail
[ $# -eq 2 ] || { echo "usage: $0 before.zip after.zip" >&2; exit 2; }

python3 -I - "$1" "$2" <<'PY'
import csv, io, sys, zipfile

def stats(path):
    z = zipfile.ZipFile(path)
    names = set(z.namelist())
    out = {"files": sorted(names)}

    def rows(name):
        with z.open(name) as f:
            return list(csv.DictReader(io.TextIOWrapper(f, encoding="utf-8-sig")))

    def fill(name, col):
        if name not in names:
            return None
        r = rows(name)
        if not r or col not in r[0]:
            return None
        return sum(1 for x in r if x[col].strip())

    for f, c in [("stops.txt", "platform_code"), ("stops.txt", "wheelchair_boarding"),
                 ("routes.txt", "route_desc"), ("routes.txt", "route_color"),
                 ("routes.txt", "route_text_color")]:
        out[f"{f}:{c}"] = fill(f, c)
    r = rows("routes.txt")
    out["routes.txt:route_type>=100"] = sum(1 for x in r if x["route_type"].isdigit() and int(x["route_type"]) >= 100)
    out["routes.txt:count"] = len(r)
    out["attributions.txt"] = "attributions.txt" in names
    out["feed_info.txt"] = "feed_info.txt" in names
    return out

b, a = stats(sys.argv[1]), stats(sys.argv[2])
print(f"{'field':36s} {'before':>10s} {'after':>10s}")
for k in b:
    if k == "files":
        continue
    print(f"{k:36s} {str(b[k]):>10s} {str(a[k]):>10s}")
PY
