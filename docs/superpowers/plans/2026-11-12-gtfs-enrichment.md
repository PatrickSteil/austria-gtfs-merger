# GTFS-Anreicherung Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Der Merger reichert den Österreich-Feed um saubere Steig-Namen, Routen-Metadaten (erweiterte `route_type`, `route_desc`, Farben) und OSM-basiertes `wheelchair_boarding` an, mit Attribution in README und `feed_info`.

**Architecture:** Neues Paket `internal/enrich` (drei Schritte auf `*gtfsparser.Feed`, je mit `Report`), Paket `internal/osmfeat` (Zwischenformat der OSM-Extraktion) und ein eigenes Kommando `cmd/osm-extract`, das die Geofabrik-PBF in die kleine Zwischendatei verwandelt. `main.go` ruft die Schritte nach `enforceParentStations` auf. CI cached die Zwischendatei.

**Tech Stack:** Go 1.24, `gtfsparser`/`gtfswriter` (vorhanden), `github.com/paulmach/osm/osmpbf` (neu, nur `cmd/osm-extract`), GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-11-12-gtfs-enrichment-design.md`

## Global Constraints

- Nur leere Felder füllen; Quell-Werte nie still überschreiben (Konflikte in den Report).
- Deterministische Ausgabe: Maps nie ungeordnet iterieren, wenn das Ergebnis davon abhängt (Stop-IDs sortieren).
- `wheelchair_boarding`: `yes`→1, `no`→2, alles andere (auch `limited`) und widersprüchliche Kandidaten →0.
- Radien: 30 m allgemein, 40 m für Bahnsteig-Ways (Vorschlag aus der Spec, als Konstanten).
- Erweiterte Typen (HVT): RJ, RJX, ICE→101; IC, EC, D, WB→102; IR→103; NJ, EN→105; R, REX, CJX, SLB, PLB, ATB, SP, ER, RR, RB, OS→106; S→109; Bus-SEV→714; SB, LIF (Basis 7)→6; FUN bleibt 7; Unbekanntes bleibt.
- Attribution-Text exakt: `© OpenStreetMap contributors (ODbL)`.
- Der Release darf nie wegen OSM blockiert werden; Ausfall = Warnung + Schritt übersprungen.
- Tests im vorhandenen Stil (Standard-`testing`, Tabellentests, synthetische Feeds mit `gtfsparser.NewFeed()`).

## Review Focus

- Trip ohne `trip_short_name` (Zeiger `nil`) darf nicht panicken und zählt nicht zur Mehrheit (Task 3).
- Route ohne Trips behält ihren Typ (Task 3).
- Stop mit Lat/Lon = 0 oder ohne Koordinate (`!HasLatLon()`) wird nicht gematcht und nicht falsch auf (0,0) gesnappt (Task 5).
- Stop, den keine Linie bedient, hat keinen Modus → Matching bleibt bei 0 (Task 5).
- OSM-Tagwerte in anderer Schreibweise (`Yes`, `designated`, Leerzeichen) werden normalisiert; unbekannte Werte werden zu 0 (Task 4/5).
- Leere oder fehlende Feature-Datei: Schritt übersprungen, Feed trotzdem geschrieben, Warnung (Task 5/7).
- Synthetischer Parent mit Steig-Namen als Namen wird nicht als Namensquelle genutzt (Task 2).
- `platform_code` bereits gesetzt und abweichend: Konflikt statt Überschreiben (Task 2).

---

### Task 1: Paket `internal/enrich`, Report und CLI-Flags

**Files:**
- Create: `internal/enrich/report.go`, `internal/enrich/report_test.go`
- Modify: `cmd/austria-gtfs-merger/main.go` (Flags nach `allowParseErrFlag`, Aufruf nach `enforceParentStations`)

**Interfaces:**
- Produces:
  - `type Report struct { Step string; Changed int; Examples []string; Conflicts []string; Counts map[string]int; Skipped string }`
  - `func (r *Report) Example(format string, args ...any)` — hängt max. 20 Einträge an.
  - `func (r *Report) Conflict(format string, args ...any)` — wie Example, eigene Liste, max. 50.
  - `func WriteReports(path string, reports []Report) error` — JSON, eingerückt, Reihenfolge wie übergeben; leerer `path` = No-op.
  - `type Steps struct { Platforms, Routes, Wheelchair bool }` und `func ParseSteps(spec string) (Steps, error)`; `"all"`, kommagetrennt `steig|routes|wheelchair`, leer = nichts.
- Flags in `main.go`: `--enrich` (Default `steig,routes`), `--extended-route-types`, `--osm-features <Pfad>`, `--enrich-report <Pfad>` (Default `enrich-report.json`).

- [ ] **Step 1: Failing Tests** in `report_test.go`: `TestParseSteps` (`"all"`→alle true; `"steig,routes"`→Platforms,Routes; `""`→alle false; `"foo"`→Error), `TestReportExampleCaps` (25 Aufrufe → `len(Examples)==20`), `TestWriteReports` (schreibt nach `t.TempDir()`, liest zurück, `Changed` und `Step` stimmen; leerer Pfad → nil, keine Datei).
- [ ] **Step 2:** `go test ./internal/enrich/ -run 'Steps|Report' -v` → FAIL (nicht definiert).
- [ ] **Step 3:** Implementiere die Typen/Funktionen oben in `report.go`.
- [ ] **Step 4:** Gleicher Test → PASS.
- [ ] **Step 5:** In `main.go` die vier Flags anlegen, `enrich.ParseSteps` auswerten (Fehler → `log.Fatalf`) und nach `enforceParentStations` eine Slice `reports []enrich.Report` aufbauen (noch leer), am Ende `enrich.WriteReports` aufrufen; je Report eine Logzeile `enrich  <step> changed=<n>`. `go build ./... && go test ./...` → PASS.
- [ ] **Step 6:** Commit `feat: add enrich package skeleton and flags`.

### Task 2: Schritt Steig-Namen

**Files:**
- Create: `internal/enrich/platforms.go`, `internal/enrich/platforms_test.go`
- Modify: `cmd/austria-gtfs-merger/main.go` (Aufruf, wenn `Steps.Platforms`)

**Interfaces:**
- Consumes: `Report` (Task 1).
- Produces:
  - `func isPlatformName(name string) bool` — Muster aus der Spec: `^[0-9]{1,3}[A-Za-z]?([,/-][0-9]{1,3}[A-Za-z]?)*$` oder `^G[0-9]+$`.
  - `func isModeWord(name string) bool` — Liste (case-insensitive): bus, bahn, zug, tram, sev, ubahn (erweiterbar).
  - `func Platforms(feed *gtfsparser.Feed) Report`

- [ ] **Step 1: Failing Tests** `TestIsPlatformName`: wahr für `"1"`, `"1,2"`, `"10A"`, `"13A-69A"`, `"G1"`, `"G12"`; falsch für `"Wien Hbf"`, `"Bahnhof 1"`, `""`, `"U1"`. `TestIsModeWord`: wahr für `"Bus"`, `"sev"`, `"Zug"`; falsch für `"Busbahnhof"`. `TestPlatforms` mit synthetischem Feed (Parent `Pat:1:1` "Wien Gersthof"; Kinder `at:1:1:0:1` Name "10A" ohne `platform_code`, `at:1:1:0:2` Name "Bus", `at:1:1:0:3` Name "Wien Gersthof", `at:1:1:0:4` Name "12" mit `platform_code="5"`; zweiter Parent `Pat:2:2` mit Name "3" und Kind "3"): erwartet Kind1 → Name "Wien Gersthof", `platform_code="10A"`; Kind2 → Name "Wien Gersthof", `platform_code==""`; Kind3 unverändert; Kind4 → Name "Wien Gersthof", `platform_code` bleibt "5", ein Eintrag in `Conflicts`; zweiter Parent/Kind unverändert (Parent-Name ist selbst steig-artig); `Changed==3`.
- [ ] **Step 2:** `go test ./internal/enrich/ -run 'Platform|ModeWord' -v` → FAIL.
- [ ] **Step 3:** Implementiere. Iteriere über sortierte Stop-IDs; nur `Location_type==0` mit `Parent_station != nil`; Parent mit steig-artigem Namen überspringen.
- [ ] **Step 4:** Tests → PASS.
- [ ] **Step 5:** In `main.go` aufrufen und `Report` an `reports` hängen. `go build ./...`.
- [ ] **Step 6:** Commit `feat: clean platform stop names`.

### Task 3: Kategorie-Tabellen und Routen-Schritt

**Files:**
- Create: `internal/enrich/categories.go`, `internal/enrich/routes.go`, `internal/enrich/routes_test.go`
- Modify: `cmd/austria-gtfs-merger/main.go`

**Interfaces:**
- Consumes: `Report`.
- Produces:
  - `type Category string`
  - `func CategoryOf(tripShortName string) (Category, bool)` — Präfix aus Buchstaben (inkl. Ä/Ö/Ü) vor Leerzeichen/Ziffer; rein numerisch oder leer → `("", false)`.
  - `func ExtendedType(c Category, base int16) int16` — Tabelle aus Global Constraints; Bus (`base==3`) nie hier.
  - `func ColorFor(c Category) (string, bool)` — 6-stelliges Hex ohne `#`; Start: R/REX/CJX, S, IC/EC/IR, RJ/RJX/ICE, NJ/EN je eine Farbe, Rest `false`.
  - `func TextColorFor(bgHex string) string` — `"000000"` oder `"FFFFFF"` nach WCAG-Kontrast.
  - `func DisplayName(c Category) string` — für `route_desc` (z. B. RJX→"Railjet Xpress", S→"S-Bahn", REX→"Regionalexpress", R→"Regionalzug", ICE→"ICE", IC→"InterCity", EC→"EuroCity", NJ→"Nightjet", EN→"EuroNight", WB→"WESTbahn", unbekannt→ die Kategorie selbst).
  - `func isReplacementBus(r *gtfs.Route) bool` — Kurzname oder Langname enthält (case-insens.) `SEV`, `Schienenersatz` als eigenes Wort.
  - `type RouteOptions struct{ ExtendedTypes bool }`
  - `func Routes(feed *gtfsparser.Feed, opts RouteOptions) Report` — Counts: `extended_types`, `mixed_routes`, `colored`, `described`.

- [ ] **Step 1: Failing Tests** (Tabellen): `TestCategoryOf` (`"RJX 662"`→RJX,true; `"S 8914"`→S; `"Ä 5"`→Ä? nicht nötig: nur `"EN 40465"`→EN; `"1234"`, `""`, `"  "`→false); `TestExtendedType` (RJX,2→101; WB,2→102; IR,2→103; EN,2→105; REX,2→106; S,2→109; SB,7→6; FUN,7→7; LIF,7→6; XYZ,2→2; REX,3→3); `TestTextColorFor` (`"FFFFFF"`→`"000000"`, `"000000"`→`"FFFFFF"`); `TestIsReplacementBus` (`"SEV"`, `"BUS SEV"`, "Schienenersatzverkehr"-Langname wahr; `"SV12"`, `"SEVERIN"` falsch). `TestRoutes` mit Feed: Route A (Typ 2) mit Trips RJX,RJX,RJ → Typ 101, `route_desc` "Railjet Xpress", Farbe aus Tabelle (nur wenn leer); Route B (Typ 2) mit 1× S, 1× REX (Gleichstand) → höherwertiger Typ (106 vor 109? Regel: kleinere HVT-Zahl gewinnt → 106), `mixed_routes==1`; Route C (Typ 3, Kurzname `SEV`) → 714; Route D (Typ 2) ohne `trip_short_name` (`nil`-Zeiger) und Route E (Typ 3) ohne Trips → unverändert; Route F mit vorhandener `route_color="FF0000"` → Farbe unverändert; mit `ExtendedTypes:false` bleibt `Type` überall unverändert, `route_desc` wird trotzdem gefüllt.
- [ ] **Step 2:** `go test ./internal/enrich/ -run 'Category|Extended|TextColor|Replacement|Routes' -v` → FAIL.
- [ ] **Step 3:** Implementiere. Mehrheit je Route über `feed.Trips` (nach Route gruppieren, Trip-Reihenfolge egal, Auswertung deterministisch über sortierte Kategorien). Reine Fragezeichen-`Long_name` (`^\?+( - \?+)*$`) auf `""` setzen und zählen.
- [ ] **Step 4:** Tests → PASS.
- [ ] **Step 5:** In `main.go` aufrufen (`RouteOptions{ExtendedTypes: *extFlag}`), `go build ./...`.
- [ ] **Step 6:** Commit `feat: enrich route metadata from trip categories`.

### Task 4: Paket `osmfeat` und `cmd/osm-extract`

**Files:**
- Create: `internal/osmfeat/osmfeat.go`, `internal/osmfeat/osmfeat_test.go`, `cmd/osm-extract/main.go`
- Modify: `go.mod`/`go.sum` (`github.com/paulmach/osm`)

**Interfaces:**
- Produces:
  - `type Feature struct { Lat, Lon float64; Kind string; Wheelchair string; Name, Ref string; Area bool }` — `Kind` ∈ `bus|rail|tram|subway|ferry|aerialway`; `Wheelchair` normalisiert (`yes|no|limited|""`); `Area` = Weg/Polygon (größerer Radius).
  - `func ClassifyTags(tags map[string]string) (kind string, ok bool)` — Regeln aus der Spec (Tag-Liste), z. B. `highway=bus_stop`→bus, `railway=platform` ohne `tram`/`subway`→rail, `railway=tram_stop`→tram, `railway=subway_entrance`→subway, `amenity=ferry_terminal`→ferry, `aerialway=station`→aerialway; `public_transport=platform|stop_position|station` nach `bus=yes`/`train=yes`/`tram=yes`/`subway=yes`/`ferry=yes`.
  - `func NormalizeWheelchair(v string) string` — trim, lower; `designated`→`yes`; Rest außer `yes|no|limited`→`""`.
  - `func Write(path string, fs []Feature) error` / `func Read(path string) ([]Feature, error)` — gzip-JSON; `Write` sortiert deterministisch (Lat, Lon, Name).
- `cmd/osm-extract`: `osm-extract --pbf <in> --out <osm-features.json.gz>`; Wege per zweitem Durchlauf auf Schwerpunkt der Knoten abbilden; Speicher unter ca. 2 GB.

- [ ] **Step 1: Failing Tests**: `TestClassifyTags` (Tabelle mit mind. je 1 Beispiel pro Kind und 2 Negativfällen, z. B. `{"railway":"rail"}`→false), `TestNormalizeWheelchair` (`"Yes "`→yes, `"designated"`→yes, `"limited"`→limited, `"bad"`→`""`), `TestWriteReadRoundTrip` (Reihenfolge nach `Write` deterministisch, `Read` gibt gleiche Features zurück).
- [ ] **Step 2:** `go test ./internal/osmfeat/ -v` → FAIL.
- [ ] **Step 3:** Implementiere `osmfeat.go`.
- [ ] **Step 4:** Tests → PASS.
- [ ] **Step 5:** Implementiere `cmd/osm-extract/main.go` mit `osmpbf` (Pass 1: Knoten mit relevanten Tags + Wege mit relevanten Tags, Node-IDs der Wege merken; Pass 2: Koordinaten dieser Knoten; Schwerpunkt = Mittel der Koordinaten). Manuell prüfen: `curl -O https://download.geofabrik.de/europe/austria-latest.osm.pbf && go run ./cmd/osm-extract --pbf austria-latest.osm.pbf --out /tmp/osm-features.json.gz`; Erwartung: läuft in wenigen Minuten, Datei im MB-Bereich, Ausgabe nennt Anzahl je Kind.
- [ ] **Step 6:** Commit `feat: add OSM accessibility feature extractor`.

### Task 5: Schritt Wheelchair (Matching)

**Files:**
- Create: `internal/enrich/wheelchair.go`, `internal/enrich/wheelchair_test.go`
- Modify: `cmd/austria-gtfs-merger/main.go`

**Interfaces:**
- Consumes: `osmfeat.Feature`, `osmfeat.Read`, `Report`.
- Produces:
  - `type WheelchairOptions struct{ RadiusM, AreaRadiusM float64 }` mit `DefaultWheelchairOptions() WheelchairOptions` (30, 40).
  - `func stopKinds(feed *gtfsparser.Feed) map[string]map[string]bool` — Stop-ID → Menge bedienter Modi (`bus|rail|tram|subway|ferry|aerialway`) aus den Routen-Typen (Basis- und HVT-Bereiche) der Trips, die den Stop anfahren.
  - `func Wheelchair(feed *gtfsparser.Feed, feats []osmfeat.Feature, opts WheelchairOptions) Report`
  - `func WheelchairKnown(feed *gtfsparser.Feed) int` — Stops mit Wert ≠ 0.

- [ ] **Step 1: Failing Tests** `TestWheelchairMatching` (synthetischer Feed, ein Bus-Stop und ein Bahnsteig-Stop, je mit einem Trip der passenden Route): Bus-Stop mit Feature `bus,yes` 10 m entfernt → 1; mit `bus,no` → 2; Feature 100 m entfernt → 0; zwei gleich nahe Features `yes`+`no` → 0 und Eintrag in `Conflicts`; Feature der Art `rail` bei Bus-Stop → 0; Feature `limited` → 0; Bahnsteig-Stop mit zwei Kandidaten, davon einer mit `Ref == platform_code` → nimmt dessen Wert; Stop ohne Trips → 0; Stop mit `Lat==0 && Lon==0` → 0; Parent mit Kindern (1, 2, 0) → 1; Parent mit Kindern (2, 0) → 2; (0, 0) → 0; leere `feats` → Report mit `Skipped` leer, alle Werte 0.
- [ ] **Step 2:** `go test ./internal/enrich/ -run Wheelchair -v` → FAIL.
- [ ] **Step 3:** Implementiere mit einfachem Gitter-Index (z. B. 0,001° Zellen) statt Brute-Force; Distanz per Haversine/Äquirektangular; Rang: Modus-Passung → `Ref==platform_code` → Namens-Ähnlichkeit (normalisierter Vergleich) → Distanz. Nur Stops mit `Location_type==0` und leerem `Wheelchair_boarding`; Parents zuletzt aus den Kindern.
- [ ] **Step 4:** Tests → PASS.
- [ ] **Step 5:** In `main.go`: wenn `Steps.Wheelchair` und `--osm-features` gesetzt und lesbar → `Wheelchair`; sonst `Report{Step:"wheelchair", Skipped:"<Grund>"}` und `log.Printf("warn    wheelchair step skipped: ...")`. Report-Counts enthalten Anteile 1/2/0 je Modus. `go build ./...`.
- [ ] **Step 6:** Commit `feat: derive wheelchair_boarding from OSM features`.

### Task 6: Stats, Warnungen, Attribution

**Files:**
- Modify: `cmd/austria-gtfs-merger/checks.go`, `cmd/austria-gtfs-merger/checks_test.go`, `cmd/austria-gtfs-merger/main.go` (`injectFeedInfo`, Aufrufer), `cmd/austria-gtfs-merger/main_test.go`, `README.md`

**Interfaces:**
- Produces:
  - `Stats` bekommt `WheelchairKnown int \`json:"wheelchair_known"\`` und `ExtendedRouteTypes int \`json:"extended_route_types"\`` (Routen mit Typ ≥ 100); `computeStats` füllt beide.
  - `func warnings(cur Stats, prev *Stats, wheelchairSkipped bool) []string` — Warnung, wenn `wheelchairSkipped` und `prev.WheelchairKnown>0`, oder wenn `WheelchairKnown` gegenüber `prev` um mehr als 20 % fällt. Blockiert nie; `checkStats` bleibt unverändert (alte Stats ohne neue Felder = 0 = keine Warnung).
  - `injectFeedInfo(out string, isZip bool, startDate, endDate string, osmUsed bool) error`: bei `osmUsed` lautet `feed_publisher_name` `austria-gtfs-merger; contains data © OpenStreetMap contributors (ODbL)` (CSV korrekt quoten), und die Datei `attributions.txt` mit Header `attribution_id,organization_name,is_producer,is_operator,is_authority,attribution_url` und einer Zeile `osm,OpenStreetMap contributors,0,0,0,https://www.openstreetmap.org/copyright` wird neben `feed_info.txt` in Zip/Verzeichnis geschrieben; sonst unverändert wie bisher.

- [ ] **Step 1: Failing Tests**: `TestWarnings` (prev 1000 / cur 700 → Warnung; 1000/900 → keine; Skipped bei prev>0 → Warnung; prev nil → keine), `TestComputeStatsExtended` (Routen mit Typ 101 und 2 → `ExtendedRouteTypes==1`), `TestInjectFeedInfoAttribution` (Zip: `feed_info.txt` enthält `© OpenStreetMap contributors (ODbL)`, `attributions.txt` enthält `OpenStreetMap contributors`; mit `osmUsed=false` fehlt `attributions.txt` und der Name ist `austria-gtfs-merger`). Vorhandener `TestInjectFeedInfo` wird auf die neue Signatur angepasst.
- [ ] **Step 2:** `go test ./cmd/... -v` → FAIL.
- [ ] **Step 3:** Implementiere; `injectFeedInfo` erhält intern eine Hilfsfunktion, die mehrere Dateien im Zip ersetzt (`map[string]string`). In `main.go` `osmUsed := wheelchairReport.Skipped == ""` übergeben, Warnungen als Log `warn    ...` ausgeben und, wenn `GITHUB_ACTIONS=true`, zusätzlich als `::warning::...`.
- [ ] **Step 4:** `go test ./... ` → PASS.
- [ ] **Step 5:** README: Abschnitt "Enrichment" mit Flags (`--enrich`, `--extended-route-types`, `--osm-features`, `--enrich-report`), kurze Beschreibung der drei Schritte, und Abschnitt "Attribution": `This feed contains data derived from OpenStreetMap, © OpenStreetMap contributors (ODbL), https://www.openstreetmap.org/copyright.`
- [ ] **Step 6:** Commit `feat: stats warnings and OSM attribution`.

### Task 7: CI und Verifikation gegen gtfstidy

**Files:**
- Modify: `.github/workflows/gtfs-merge.yml`, `.gitignore` (`osm-features.json.gz`, `enrich-report.json`)
- Create: `scripts/check-tidy-fields.sh` (Hilfsskript für Step 1)

- [ ] **Step 1 (Empirie, zuerst):** Lokal `./merger --dir gtfs_feeds -o /tmp/enriched.zip --enrich steig,routes --extended-route-types`, dann den gtfstidy-Befehl aus dem Workflow auf `/tmp/enriched.zip` anwenden und mit `scripts/check-tidy-fields.sh` Header und Füllraten von `platform_code`, `route_desc`, `route_color`, `route_type`≥100 und `wheelchair_boarding` vor und nach dem Tidy vergleichen (Ausgabe als Tabelle). Das Ergebnis im Commit-Text festhalten. Überlebt ein Feld nicht, entscheide mit dem Nutzer, ob ein gtfstidy-Flag angepasst oder das Feld nach dem Tidy neu eingebracht wird, bevor es weitergeht.
- [ ] **Step 2:** Workflow: Schritt "OSM features" vor "Run GTFS merger": `Last-Modified` von `https://download.geofabrik.de/europe/austria-latest.osm.pbf` per `curl -sI` auslesen, `actions/cache@v4` mit Schlüssel `osm-features-<Datum>` und `restore-keys: osm-features-`; bei Cache-Miss PBF laden und `go run ./cmd/osm-extract` ausführen (`continue-on-error: true`, bei Fehler `::warning::`). Cache älter als 60 Tage (Datum im Schlüssel) → `::warning::`.
- [ ] **Step 3:** Merger-Aufruf um `--enrich all --extended-route-types --enrich-report enrich-report.json` und `--osm-features osm-features.json.gz` (nur wenn die Datei existiert) ergänzen.
- [ ] **Step 4:** Nach dem Tidy-Schritt (wie `feed_info.txt`) `attributions.txt` aus dem ungetidyten Zip wieder einfügen, falls vorhanden; ebenso alle Felder, die laut Step 1 nicht überleben.
- [ ] **Step 5:** `files:` im Release um `enrich-report.json` erweitern; im Release-Text einen Absatz zur OSM-Attribution ergänzen.
- [ ] **Step 6:** Prüfen: `actionlint` (falls vorhanden) bzw. YAML-Syntax mit `python3 -c 'import yaml,sys; yaml.safe_load(open(".github/workflows/gtfs-merge.yml"))'`; danach manueller `workflow_dispatch`-Lauf mit `force=true` durch den Nutzer; erwartet: Release enthält beide Dateien, `feed_info.txt` nennt OSM.
- [ ] **Step 7:** Commit `ci: run enrichment steps and publish report`.

---

## Self-Review

- **Spec-Abdeckung:** Steig → Task 2; Routen (Kategorie, erweiterte Typen, `route_desc`, Farben, SEV, Fragezeichen-Namen, Opt-in-Flag) → Task 3; Extraktion/Matching/Parent-Ableitung → Task 4–5; Report → Task 1; Stats-Warnung, Attribution in `feed_info`/`attributions.txt`/README → Task 6; Cache, CI, gtfstidy-Risiko, Report als Asset → Task 7.
- **Typkonsistenz:** `Report`, `Steps`, `RouteOptions`, `WheelchairOptions`, `osmfeat.Feature` werden in den Tasks, die sie nutzen, gleich benannt.
- **Annahme:** Der Check gegen gtfstidy (Task 7, Step 1) kann Task 3/5 beeinflussen; er läuft deshalb vor den CI-Änderungen und darf zu Rückfragen führen.
