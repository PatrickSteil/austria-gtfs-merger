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
  -e, --drop-erroneous   Drop erroneous GTFS entities
  -s, --drop-shapes      Drop shapes.txt data
  -o, --output string    Output path (directory or .zip) for merged GTFS (default "merged.zip")
  -t, --threads int      Number of worker threads (download only - 0 means all threads)
  -v, --verbose          Verbose output
  -w, --warning          Show all warning while reading GTFS
```

### Username and Password
Note that in order to automatically download all the GTFS feeds from [data.mobilitaetsverbuende.at](https://data.mobilitaetsverbuende.at/), you need to have an account (i.e., username and password).
These must be set as environment variables USERNAME and PASSWORD.

Hence a complete call to `merger` (which downloads and merges the GTFS feeds), could look like this:

```bash
USERNAME=user1 PASSWORD=hiddenpassword ./merger -d -o austria.merged.zip --drop-shapes --drop-erroneous
```

## Latest merged GTFS

A daily merged GTFS feed is available here:

[https://github.com/PatrickSteil/austria-gtfs-merger/releases](https://github.com/PatrickSteil/austria-gtfs-merger/releases/download/latest/austria.merged.zip)

## License

This project depends on `gtfsparser` and `gtfswriter`, which are licensed under GPL-2.0.  
As a result, this project is also licensed under GPL-2.0.

If you are interested in a more permissive license, contributions replacing these dependencies are welcome.
