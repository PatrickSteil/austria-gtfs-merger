# Austria GTFS Merger

> Download and merge all Austrian public transport GTFS feeds into a single unified dataset.

This project allows you to download and merge all GTFS feeds from [data.mobilitaetsverbuende.at](https://data.mobilitaetsverbuende.at/):

> "Welcome to the data platform of the Mobilitätsverbünde Österreich. The platform serves as a central site for the publication of mobility data of the Austrian public transport authorities in standardized data formats."

They provide multiple GTFS datasets (and much more), but they do not upload *one* entire Austria GTFS feed; instead multiple smaller ones.

Note that merging GTFS feeds is generally non-trivial, as the same logical entity (e.g., stops, trips, routes) may appear in multiple feeds under different IDs.
In this case, however, the datasets use a coordinated ID scheme across feeds, so entities remain consistent and can be merged directly.
Any remaining duplicate entities are handled by `gtfsparser`.

## Setup
```bash
git clone https://github.com/PatrickSteil/austria-gtfs-merger.git
cd austria-gtfs-merger
make
```

## Usage

The provided `Makefile` allows the following parameters.
| Command | Description |
|---|---|
| `make build` | Compile the binary |
| `make run` | Build and run |
| `make tidy` | Run `go mod tidy` |
| `make clean` | Remove the binary |

The main executable `merger` has the following options:

```bash
>>> ./merger -h
merger (C) Patrick Steil <patrick@steil.dev>
Version dev
Usage:
      --dir string       GTFS directory (default "gtfs_feeds")
  -d, --download         Download GTFS datasets
      --enrich string    Enrichment steps: steig, routes, wheelchair, all (default "steig,routes")
      --enrich-report string  Where to write the enrichment report (default "enrich-report.json")
      --extended-route-types  Write extended (HVT) route_type values
      --osm-features string  OSM features file from osm-extract (enables the wheelchair step)
  -e, --drop-erroneous   Drop erroneous GTFS entities
  -s, --drop-shapes      Drop shapes.txt data
  -o, --output string    Output path (directory or .zip) for merged GTFS (default "merged.zip")
  -t, --threads int      Number of worker threads (download only - 0 means all threads)
  -v, --verbose          Verbose output
  -w, --warning          Show all warning while reading GTFS
```

### Username and Password
Note that in order to automatically download all the GTFS feeds from [data.mobilitaetsverbuende.at](https://data.mobilitaetsverbuende.at/), you need to have an account (i.e., username and password).
These must be set as environment variables `DBP_USERNAME` and `DBP_PASSWORD` (or in a `.env` file).

Hence a complete call to `merger` (which downloads and merges the GTFS feeds), could look like this:

```bash
DBP_USERNAME=user1 DBP_PASSWORD=hiddenpassword ./merger -d -o austria.merged.zip --drop-shapes --drop-erroneous
```

### Sanity checks

Before writing the output, the merger checks that the result looks complete and aborts (exit code 1, no file written) otherwise. This prevents publishing a feed that is silently missing a region or operator.

| Check | Default | Flag |
|---|---|---|
| Every input feed contributes at least one trip | always | |
| Minimum number of agencies | 50 | `--min-agencies` |
| Minimum number of trips | 500000 | `--min-trips` |
| Minimum number of rail routes (type 2 or 100-199) | 200 | `--min-rail-routes` |
| Maximum drop of any count vs. the previous run | 20 % | `--max-drop` |

The previous run's counts are stored in `merge-stats.json` (`--stats`), which is only updated after a feed passed all checks. Set a limit to `0` to disable it, or pass `--skip-checks` to write the output anyway. A feed that fails to parse also aborts the run unless `--allow-parse-errors` is given.

## Enrichment

After merging, optional steps derive data the source feeds do not provide. Each step only fills empty fields (or fixes clearly recognised patterns), never overwrites source values silently, and reports what it did in `enrich-report.json`.

| Step | Flag value | What it does |
|---|---|---|
| Platform names | `steig` | Boarding stops that are only named after their platform (`13A`, `1,2`, `G1`) get the station name; the platform moves to `platform_code`. |
| Route metadata | `routes` | The product category from `trip_short_name` (RJX, ICE, REX, S, ...) fills `route_desc` and, for rail, `route_color`/`route_text_color`. With `--extended-route-types`, rail routes get HVT types (101 high speed, 102 long distance, 103 inter-regional, 105 sleeper, 106 regional, 109 S-Bahn), rail replacement buses 714, cable cars 6. |
| Wheelchair | `wheelchair` | `wheelchair_boarding` from OpenStreetMap `wheelchair=*` tags of nearby platforms and stops. Needs `--osm-features`. Unknown or ambiguous cases stay `0`. |

The OSM features file is built from a Geofabrik extract:

```bash
curl -O https://download.geofabrik.de/europe/austria-latest.osm.pbf
go run ./cmd/osm-extract --pbf austria-latest.osm.pbf --out osm-features.json.gz
./merger --enrich all --extended-route-types --osm-features osm-features.json.gz ...
```

If the OSM step cannot run, the feed is still written (without `wheelchair_boarding`) and a warning is logged.

## Attribution

This feed contains data derived from OpenStreetMap: © OpenStreetMap contributors, available under the [Open Database License (ODbL)](https://www.openstreetmap.org/copyright). The attribution is also part of the feed itself (`feed_info.txt` and `attributions.txt`) whenever the OSM step ran.

## Latest merged GTFS

A daily merged GTFS feed is available here:

[https://github.com/PatrickSteil/austria-gtfs-merger/releases](https://github.com/PatrickSteil/austria-gtfs-merger/releases/download/latest/austria.merged.zip)

## License

This project depends on `gtfsparser` and `gtfswriter`, which are licensed under GPL-2.0.  
As a result, this project is also licensed under GPL-2.0.

If you are interested in a more permissive license, contributions replacing these dependencies are welcome.
