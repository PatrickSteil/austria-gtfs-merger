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
      --osm-features string  OSM features file from osm-extract (input of the OpenStreetMap steps)
      --osm-routes string    OSM route relations file from osm-extract (input of the osmroutes step)
      --osm-sidecar-dir string  Directory for the OSM stop attribute sidecar and the QA report (ODbL)
      --osm-output string    Second output with the OSM-derived data (ODbL); the main output never contains OSM data
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

| Step | Flag value | Input | What it does |
|---|---|---|---|
| Platform names | `steig` | the GTFS feeds only | Boarding stops that are only named after their platform (`13A`, `1,2`, `G1`) get the station name; the platform moves to `platform_code`. |
| Route metadata | `routes` | the GTFS feeds only | The product category from `trip_short_name` (RJX, ICE, REX, S, ...) fills `route_desc` and, for rail, `route_color`/`route_text_color`. With `--extended-route-types`, rail routes get HVT types (101 high speed, 102 long distance, 103 inter-regional, 105 sleeper, 106 regional, 109 S-Bahn), rail replacement buses 714, cable cars 6. |
| Wheelchair | `wheelchair` | GTFS stops + OpenStreetMap | `wheelchair_boarding` from the OpenStreetMap `wheelchair=*` tag of the nearest matching platform or stop (30 m, 40 m for platform areas, same transport mode; platform number and name break ties; stop positions are ignored). Only a clear `yes`/`no` is written, everything else stays `0` (unknown). A station gets a value only if all its boarding stops agree. |
| Platform codes | `platformcodes` | GTFS stops + OpenStreetMap | Empty `platform_code` filled from the platform `ref` (rail, tram, subway) or `local_ref` (bus) of the best match within 15 m. An existing code is never replaced; a differing OSM value is reported. |
| Route relations | `osmroutes` | GTFS routes + OpenStreetMap | `route_color` and `route_long_name` from OSM route relations with the same mode and ref whose stops overlap the GTFS route's stops (at least 60 %). Source colours are kept; colours from our own category palette are replaced. Relations that disagree give no colour. Needs `--osm-routes`. |
| Entrances | `entrances` | GTFS stations + OpenStreetMap | OSM station entrances where the feed has none at that station: `location_type=2` stops below the nearest station, linked by walkway pathways to its nearest boarding stops (estimated length and time, because GTFS pathways cannot end at a station). |

Two more outputs describe the OSM match, they do not change the feed. With `--osm-sidecar-dir` the merger writes `osm-stop-attributes.csv.gz` (tactile paving, bench, shelter, bicycle parking and elevators within 100 m, per stop) and `osm-qa-report.json` (stops without OSM counterpart, distance to the OSM stop position, OSM stops without GTFS stop, route relation matching, elevators).

`steig` and `routes` only use the GTFS feeds. All other steps use OpenStreetMap, so their result is written to a **separate output** (`--osm-output`), never into the main output, and the main feed stays free of OSM data (see [Data sources](#data-sources-and-how-they-are-combined)). The merger refuses to run an OpenStreetMap step without that option.

The OSM files are built from a Geofabrik extract:

```bash
curl -O https://download.geofabrik.de/europe/austria-latest.osm.pbf
go run ./cmd/osm-extract --pbf austria-latest.osm.pbf \
    --out osm-features.json.gz --routes-out osm-routes.json.gz
./merger --enrich all --extended-route-types \
         --osm-features osm-features.json.gz --osm-routes osm-routes.json.gz \
         --output austria.merged.zip --osm-output austria.merged.osm.zip \
         --osm-sidecar-dir osm-extras ...
```

If the OSM data is missing or from an outdated extraction format, the main feed is written as usual, no OSM variant is produced and a warning is logged. A missing `--osm-routes` file only skips the `osmroutes` step.

## Latest merged GTFS

A merged GTFS feed is built daily. These files are assets of the rolling `latest` release:

| File | Contains | Licence of the data |
|---|---|---|
| [`austria.merged.zip`](https://github.com/PatrickSteil/austria-gtfs-merger/releases/download/latest/austria.merged.zip) (permalink, unchanged) | Merged Mobilitätsverbünde feeds plus our rule-based enrichment. **No OpenStreetMap data.** | Terms of the Mobilitätsverbünde Österreich data licence |
| [`austria.merged.osm.zip`](https://github.com/PatrickSteil/austria-gtfs-merger/releases/download/latest/austria.merged.osm.zip) | The same feed enriched with OpenStreetMap data: `wheelchair_boarding`, platform codes and extra entrances in `stops.txt`, entrance pathways in `pathways.txt`, route colours and names in `routes.txt`. | `stops.txt`, `pathways.txt`, `routes.txt`: ODbL, © OpenStreetMap contributors. All other files: as above. |
| `osm-stop-attributes.csv.gz` | Per stop: tactile paving, bench, shelter, bicycle parking and elevators nearby (not a GTFS file). | ODbL, © OpenStreetMap contributors |
| `osm-qa-report.json` | Differences between the feed and OpenStreetMap (stops, stop positions, route relations). | ODbL, © OpenStreetMap contributors |

Both zip files are built from the same merged feed; the OSM variant only adds the OSM-derived data above and the attribution files described below. `enrich-report.json` lists what the enrichment steps changed. If a run has no OpenStreetMap data, the three OSM assets are removed from the release instead of being left outdated.

[All releases](https://github.com/PatrickSteil/austria-gtfs-merger/releases)

## Data sources and how they are combined

| Data | Source | Used for |
|---|---|---|
| GTFS datasets of the Austrian transport associations | [data.mobilitaetsverbuende.at](https://data.mobilitaetsverbuende.at/) (Mobilitätsverbünde Österreich), downloaded with an account of this repository's owner and used under the platform's data licence | Everything in both files: agencies, stops, routes, trips, times, calendars, transfers, pathways |
| OpenStreetMap | [Geofabrik](https://download.geofabrik.de/europe/austria.html) extract of Austria, © OpenStreetMap contributors, [ODbL](https://opendatacommons.org/licenses/odbl/1-0/) | Tags `wheelchair`, `ref`/`local_ref`, `tactile_paving`, `bench`, `shelter`, route relations (`colour`, `from`, `to`), entrances, elevators, bicycle parking, stop positions. Only in `austria.merged.osm.zip`, the sidecar and the QA report. |
| Our own rules | This repository (GPL-2.0) | Product categories, route types, colors, platform names; no external data |

How they meet:

1. **Merge.** The feeds of all associations are merged into one dataset. IDs are consistent across the feeds, so entities are joined directly; duplicates are handled by `gtfsparser`.
2. **Rule-based enrichment.** Platform names and route metadata are derived from the merged feed itself. No OpenStreetMap data is involved. The result is `austria.merged.zip`.
3. **OpenStreetMap match.** A reduced copy of the OSM data (public transport stops and platforms, entrances, elevators, bicycle parking and route relations) is matched to each boarding stop and route by distance, transport mode and, for routes, ref and shared stops. The matched values are added to `stops.txt`, `routes.txt` and `pathways.txt`, which gives `austria.merged.osm.zip`; the sidecar and the QA report are written next to it. This happens after `austria.merged.zip` has been written, so the main feed never sees it.

What this means for the licences:

- **`austria.merged.zip`** contains no OSM data. It is covered by the terms of the Mobilitätsverbünde data licence only.
- **`austria.merged.osm.zip`**: its `stops.txt`, `routes.txt` and `pathways.txt` combine the association data with values taken from OpenStreetMap. We treat them as a *Derivative Database* in the sense of the ODbL and offer them under the [ODbL 1.0](https://opendatacommons.org/licenses/odbl/1-0/). The other files in that zip (agencies, trips, stop times, calendars, transfers, ...) contain no OSM data and keep the licence of the source data. They are the same files as in `austria.merged.zip`.
- **Sidecar and QA report** are derived from OpenStreetMap and offered under the ODbL as well.
- **Attribution** for OpenStreetMap is part of the file itself: `feed_publisher_name` in `feed_info.txt` and `attributions.txt`. Data from the Mobilitätsverbünde Österreich remains attributed to its publisher (see the platform linked above for the required wording).
- This is our reading of the licences and the OSM Foundation's [Community Guidelines](https://osmfoundation.org/wiki/Licence/Community_Guidelines), not legal advice. If you build on these files, check the terms yourself.

## License

This project depends on `gtfsparser` and `gtfswriter`, which are licensed under GPL-2.0.  
As a result, this project is also licensed under GPL-2.0.

If you are interested in a more permissive license, contributions replacing these dependencies are welcome.
