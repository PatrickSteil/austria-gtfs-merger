# OSM-Anreicherung II Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Platform Codes, Linien-Relationen (Farbe/Name), Eingänge mit Pathways, ein Sidecar mit Haltestellen-Attributen und ein QA-Report, alles aus OSM und nur in `austria.merged.osm.zip` bzw. daneben.

**Architecture:** Das Rollstuhl-Matching wird zu einem gemeinsamen Kern (`Match`) umgebaut, den alle Stufen nutzen. `osmfeat` bekommt ein versioniertes Format mit mehr Feature-Arten und einer zweiten Datei für Linien-Relationen. `cmd/osm-extract` schreibt beide. Die Stufen laufen nach dem Schreiben des Hauptfeeds auf derselben Feed-Struktur und schreiben die OSM-Variante.

**Tech Stack:** Go 1.24, `gtfsparser`/`gtfswriter`, `paulmach/osm` (nur `cmd/osm-extract`), GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-11-12-osm-enrichment-2-design.md`

## Global Constraints

- Der Hauptfeed enthält nie OSM-abgeleitete Daten; alle OSM-Stufen laufen erst nach dem Schreiben des Hauptfeeds.
- Nur leere Felder füllen; Quellwerte nie still überschreiben (Konflikte in den Report). Ausnahme: von `enrich.Routes` selbst vergebene Palette-Farben dürfen ersetzt werden.
- Im Zweifel nichts setzen; deterministische Ausgabe (nach IDs sortieren).
- Radien: 30 m Punkte, 40 m Flächen (Matching), 15 m für Platform Codes, 80 m für Eingänge, 40 m und Überlappung ≥ 0,6 bei ≥ 2 gemeinsamen Haltestellen für Relationen, 100 m für Sidecar-Anzahlen, 50 m für "OSM-Stop ohne GTFS-Stop". Alle als benannte Konstanten.
- `osmfeat`-Format: JSON-Objekt `{"version":2,...}` (gzip); `Read` lehnt andere Versionen mit `ErrFormat` ab.
- Platform-Code-Werte: 1–4 Zeichen aus `[0-9A-Za-z]`, kein `;`, kein Leerzeichen. Bus: nur `local_ref`; Bahn/Tram/U-Bahn: `ref` oder `local_ref`.
- Farben: nur `RRGGBB` oder `#RRGGBB` (3-stellig `#RGB` wird erweitert); Namen wie "red" werden ignoriert. Hex wird großgeschrieben.
- Eingang: `location_type=2`, ID `osm:entrance:<osm-id>`; Pathway `pathway_mode=1`, bidirektional, ID `osm:pw:<osm-id>:<stop_id>`, `length` = Luftlinie in m, `traversal_time` = 30 + gerundete Luftlinie (Sekunden bei 1 m/s), höchstens 4 nächste Boarding-Stops.
- Flags: `--osm-output` (ersetzt `--wheelchair-output`), `--osm-routes`, `--osm-sidecar-dir`; Schritte `wheelchair`, `platformcodes`, `osmroutes`, `entrances`, `all`; jeder OSM-Schritt verlangt `--osm-output`.
- Tests im vorhandenen Stil (Standard-`testing`, Tabellentests, synthetische Feeds).

## Review Focus

- Eine Relation ohne `ref`, `colour`, `from` oder `to` (leere Strings) darf nichts auslösen oder abstürzen (Task 5).
- Eine GTFS-Route mit 0 oder 1 Haltestellen: keine Division durch null, keine Zuordnung (Task 5).
- Ungültige Farben (`red`, `#FFF`-Sonderfall, `#GGGGGG`) werden ignoriert bzw. korrekt erweitert (Task 1/5).
- Ein Cache im alten Format (reines JSON-Array) wird mit `ErrFormat` abgelehnt, der Merge läuft weiter (Task 1/8).
- Station ohne Boarding-Stops oder nur mit Bus: kein Eingang (Task 6).
- Eine `stop_position` näher als die Plattform darf keinen Rollstuhl-Wert verdecken (Task 3).
- Gleicher Lauf zweimal ergibt byte-gleiche Zip/Sidecar/Report (bis auf `feed_version`) (Task 8).
- Der Hauptfeed eines Laufs mit OSM-Schritten gleicht dem ohne (Task 8).

---

### Task 1: `osmfeat` Format v2, Hilfsfunktionen, Relationen-Typen

**Files:**
- Modify: `internal/osmfeat/osmfeat.go`, `internal/osmfeat/osmfeat_test.go`
- Create: `internal/osmfeat/routes.go`, `internal/osmfeat/routes_test.go`

**Interfaces:**
- Produces:
  - `Feature` bekommt `ID int64`, `Platform string`, `Tactile, Bench, Shelter string`, `StopPos bool`. Neue `Kind`-Werte: `entrance`, `elevator`, `bicycle_parking`.
  - `const FormatVersion = 2`, `var ErrFormat = errors.New("osmfeat: unsupported file format")`; `Write`/`Read` schreiben/lesen `{"version":2,"features":[...]}`; `Read` gibt bei anderer Version oder altem Array `ErrFormat` zurück.
  - `func PlatformRef(kind string, tags map[string]string) string`
  - `func NormalizeTri(v string) string` — `yes`/`no`/`""` (trim, lower; `designated` → `yes`).
  - `func ClassifyExtra(tags map[string]string) (kind string, ok bool)` — `amenity=bicycle_parking`→`bicycle_parking`; `highway=elevator`→`elevator`; `railway=subway_entrance`→`entrance`; `entrance=*` zusammen mit `railway` oder `public_transport`→`entrance`; sonst `false`.
  - `type Point struct{ Lat, Lon float64 }`
  - `type RouteRel struct { ID int64; Mode, Ref, Name, From, To, Network, Operator, Colour string; Stops []Point }`
  - `func ModeOfRoute(routeTag string) (string, bool)` — bus/trolleybus→`bus`; train/light_rail→`rail`; tram→`tram`; subway→`subway`; ferry→`ferry`; funicular→`aerialway`.
  - `func NormalizeColour(v string) string` — siehe Global Constraints; ungültig → `""`.
  - `func RouteRelFromTags(id int64, tags map[string]string) (RouteRel, bool)` — nur wenn `type=route`, `ModeOfRoute` bekannt und `ref` oder `name` nicht leer; `Colour` normalisiert.
  - `func WriteRoutes(path string, rels []RouteRel) error` / `func ReadRoutes(path string) ([]RouteRel, error)` — gleiches versioniertes Format (`{"version":2,"routes":[...]}`), sortiert nach `ID`.

- [ ] **Step 1: Failing Tests**: `TestPlatformRef` (rail `{ref:"3"}`→`"3"`; rail `{local_ref:"7a"}`→`"7a"`; bus `{ref:"1234"}`→`""`; bus `{local_ref:"B"}`→`"B"`; rail `{ref:"1;2"}`→`""`; rail `{ref:"12345"}`→`""`; rail `{ref:"Gleis 3"}`→`""`). `TestNormalizeTri`. `TestClassifyExtra` (je ein Beispiel für die drei Arten; `{entrance:"yes"}` allein → false; `{entrance:"yes",railway:"station"}` → entrance). `TestModeOfRoute`, `TestNormalizeColour` (`"#ff0000"`→`FF0000`, `"00ff00"`→`00FF00`, `"#f00"`→`FF0000`, `"red"`→`""`, `"#GGGGGG"`→`""`). `TestRouteRelFromTags` (gültig; `route=road`→false; ohne ref und name→false). `TestFeaturesVersioned` (Round Trip der neuen Felder; Read einer Datei im alten Format, die du im Test als gzip-Array schreibst → `errors.Is(err, ErrFormat)`). `TestRoutesRoundTrip`.
- [ ] **Step 2:** `go test ./internal/osmfeat/ -v` → FAIL.
- [ ] **Step 3:** Implementiere; das bestehende `ClassifyTags` bleibt, setzt aber nichts am `StopPos`-Flag (das setzt der Extraktor).
- [ ] **Step 4:** Tests → PASS; `go build ./... && go test ./...` (Aufrufer von `Read`/`Write` im Code bleiben kompatibel).
- [ ] **Step 5:** Commit `feat(osmfeat): versioned format, new feature kinds, route relation types`.

### Task 2: `osm-extract` liefert Features v2 und Relationen

**Files:**
- Modify: `cmd/osm-extract/main.go`

**Interfaces:**
- Consumes: Task 1.
- Produces: `osm-extract --pbf <in> --out osm-features.json.gz --routes-out osm-routes.json.gz`. Features: wie bisher, plus `ID`, `Platform` (`PlatformRef`), `Tactile`/`Bench`/`Shelter` (`NormalizeTri` von `tactile_paving`/`bench`/`shelter`), `StopPos` (`public_transport=stop_position`) sowie die Arten aus `ClassifyExtra`. Relationen: `RouteRelFromTags`, Haltestellen-Member = Knoten und Wege mit Rolle, die mit `stop` oder `platform` beginnt (Wege über den Schwerpunkt), Unter-Relationen ignoriert.

- [ ] **Step 1:** Extraktor in drei Durchläufen umbauen: (A) nur Relationen: gültige `RouteRel` sammeln, Member-Knoten- und Wege-IDs merken; (B) Knoten (Features + benötigte Koordinaten) und Wege (Features; Wege, die Relation-Member sind, mit Knotenliste merken); (C) Koordinaten der Wegknoten. Die Funktionen `feature(...)` und `scan(...)` wiederverwenden; `ID` = OSM-ID, bei Wegen mit Vorzeichen-Offset nicht nötig (getrennte Kinds).
- [ ] **Step 2:** `go build ./... && go vet ./...`.
- [ ] **Step 3 (manuell):** `go run ./cmd/osm-extract --pbf <austria-latest.osm.pbf aus dem Scratchpad> --out <dir>/f.json.gz --routes-out <dir>/r.json.gz`. Expected: läuft in wenigen Minuten bei unter 2 GB; Log nennt Anzahl je Kind inkl. `entrance`, `elevator`, `bicycle_parking` (in der Größenordnung 700 / 2 400 / 16 000) und etwa 12 500 Relationen; beide Dateien lesbar mit `osmfeat.Read`/`ReadRoutes`.
- [ ] **Step 4:** Commit `feat(osm-extract): extract extra features and route relations`.

### Task 3: Gemeinsamer Matching-Kern

**Files:**
- Modify: `internal/enrich/wheelchair.go`, `internal/enrich/wheelchair_test.go`
- Create: `internal/enrich/matching.go`

**Interfaces:**
- Consumes: bestehende `candidate`, `better`, `stopKinds`, `Wheelchair` aus `wheelchair.go`.
- Produces:
  - `type StopMatch struct { Stop *gtfs.Stop; Modes map[string]bool; Best candidate; Tied []candidate; HasBest bool; Conflict bool }` (`Tied` = gleich gute Kandidaten innerhalb `tieMeters`, ohne `Best`).
  - `func Match(feed *gtfsparser.Feed, feats []osmfeat.Feature, opts WheelchairOptions) []StopMatch` — sortiert nach Stop-ID; nur Boarding-Stops mit Koordinaten ≠ (0,0) und mit bedienendem Modus; **Features mit `StopPos` oder mit Kind ∉ {bus, rail, tram, subway, ferry, aerialway} sind keine Kandidaten**.
  - `func ApplyWheelchair(feed *gtfsparser.Feed, matches []StopMatch) Report` — bisherige Logik inkl. `deriveParents`.
  - `Wheelchair(feed, feats, opts)` bleibt als Aufruf von `Match` + `ApplyWheelchair`.

- [ ] **Step 1: Failing Test** `TestStopPositionDoesNotHideWheelchair` (Bus-Stop, `stop_position`-Feature `StopPos:true` 3 m ohne Tag, Plattform-Feature mit `yes` 12 m → `Wheelchair_boarding==1`) und `TestMatchExposesBest` (`Match` liefert für den Stop `HasBest`, `Best.f.Wheelchair=="yes"`, `Conflict==false`; bei `yes`+`no` gleich nah `Conflict==true`).
- [ ] **Step 2:** `go test ./internal/enrich/ -run 'StopPosition|MatchExposes' -v` → FAIL.
- [ ] **Step 3:** Matching-Schleife aus `Wheelchair` nach `Match` verschieben, `ApplyWheelchair` implementieren, `StopPos` ausschließen.
- [ ] **Step 4:** `go test ./...` → alle bisherigen Rollstuhl-Tests plus die neuen PASS.
- [ ] **Step 5:** Commit `refactor: shared stop matching core`.

### Task 4: Platform Codes

**Files:**
- Create: `internal/enrich/platformcodes.go`, `internal/enrich/platformcodes_test.go`

**Interfaces:**
- Consumes: `StopMatch` (Task 3), `Feature.Platform`.
- Produces: `func PlatformCodes(matches []StopMatch) Report` (Step `platformcodes`, Counts `set`, `conflicts`).

- [ ] **Step 1: Failing Tests** `TestPlatformCodes` über `Match` mit synthetischem Feed: leerer `platform_code` + bester Kandidat mit `Platform:"3"` bei 10 m → `"3"`; bester Kandidat bei 20 m → bleibt leer; gesetzter `platform_code:"5"` und Kandidat `"3"` → bleibt `"5"`, ein Eintrag in `Conflicts`; zwei gleich gute Kandidaten mit `"3"` und `"4"` → leer und `Conflicts`; Kandidat ohne `Platform` → leer; `Changed` zählt gesetzte Stops.
- [ ] **Step 2:** Test ausführen → FAIL.
- [ ] **Step 3:** Implementiere `PlatformCodes` (15-m-Konstante `platformCodeRadiusM`).
- [ ] **Step 4:** Tests → PASS; **Step 5:** Commit `feat: platform codes from OSM refs`.

### Task 5: Linien-Relationen

**Files:**
- Modify: `internal/enrich/routes.go`, `internal/enrich/report.go`, `internal/enrich/routes_test.go`
- Create: `internal/enrich/osmroutes.go`, `internal/enrich/osmroutes_test.go`

**Interfaces:**
- Produces:
  - `Report.PaletteRoutes map[string]bool` mit Tag `json:"-"`; `Routes(...)` trägt die IDs der von der Palette gefärbten Routen ein.
  - `type RouteMatchResult struct { Matched, Ambiguous, UnmatchedRoutes, UnmatchedGroups []string }`
  - `func OSMRoutes(feed *gtfsparser.Feed, rels []osmfeat.RouteRel, palette map[string]bool) (Report, RouteMatchResult)` (Step `osmroutes`; Counts `matched`, `ambiguous`, `colored`, `named`). Route→Modus wie in `routeKind`; `route_short_name` und `Ref` werden normalisiert (Kleinbuchstaben, ohne Leerzeichen und `-`). Gruppierung, Überlappung, Farbe und `route_long_name` nach der Spec ("Entscheidungen des Autors"). Eine Route ohne oder mit nur einer Haltestelle wird nie zugeordnet.

- [ ] **Step 1: Failing Tests** `TestRoutesRecordsPaletteRoutes` (Route S mit Kategorie `S` → `rep.PaletteRoutes["…"]`; Route mit Quellfarbe → nicht enthalten). `TestOSMRoutes` (Helfer baut Route mit Haltestellen bei Koordinaten): zwei Relationen `S1` (Richtungen, `from`/`to` vertauscht, `colour:"#FF0000"`, gleiche Haltestellen) → Route `S1` bekommt `FF0000`, Text über `TextColorFor`, `route_long_name` aus dem Paar der kleineren ID; eine von der Palette gefärbte Route wird ersetzt, eine mit Quellfarbe (nicht in `palette`) bleibt; zwei Gruppen gleicher Ref mit gleicher Überlappung → `Ambiguous`, nichts gesetzt; falscher Modus → unverändert; Überlappung 0,5 → unverändert; Gruppe mit zwei verschiedenen Farben → keine Farbe; drei verschiedene `from`/`to`-Paare → kein Name; Relation ohne `Ref`/`Colour`/`From`/`To` → keine Wirkung; Route mit einer Haltestelle → unzugeordnet.
- [ ] **Step 2:** Tests → FAIL. **Step 3:** Implementiere; Raster-Index für Relation-Haltestellen wiederverwenden (`cellOf`, `distM`). **Step 4:** Tests → PASS. **Step 5:** Commit `feat: route colours and names from OSM relations`.

### Task 6: Eingänge

**Files:**
- Create: `internal/enrich/entrances.go`, `internal/enrich/entrances_test.go`

**Interfaces:**
- Consumes: `stopKinds`, `Feature` (Kind `entrance`, `ID`, `Wheelchair`, `Ref`), `feed.Stops`, `feed.Pathways` (`map[string]*gtfs.Pathway`).
- Produces: `func Entrances(feed *gtfsparser.Feed, feats []osmfeat.Feature) Report` (Step `entrances`; Counts `entrances`, `pathways`).

- [ ] **Step 1: Failing Tests** `TestEntrances`: Station mit zwei Bahn-Boarding-Stops und OSM-Eingang `yes` bei 40 m → neuer Stop `osm:entrance:<id>` mit `Location_type==2`, `Parent_station`, `Wheelchair_boarding==1`, Name enthält den Stationsnamen, und zwei Pathways (`Mode==1`, `Is_bidirectional`, `Length`≈Distanz, `Traversal_time==30+round(Length)`); Station mit sechs Boarding-Stops → genau 4 Pathways zu den nächsten; Station mit bestehendem Kind `Location_type==2` → nichts; bestehender Feed-Eingang innerhalb 80 m des OSM-Eingangs (auch bei anderer Station) → nichts; nur-Bus-Station → nichts; Station ohne Boarding-Stops → nichts; Eingang bei 120 m → nichts; zwei Stationen in Reichweite → nur die nächste; Lauf zweimal → gleiche IDs/Anzahl (idempotent, ein zweiter Aufruf fügt nichts hinzu).
- [ ] **Step 2:** Tests → FAIL. **Step 3:** Implementiere. **Step 4:** Tests → PASS. **Step 5:** Commit `feat: OSM entrances with walkway pathways`.

### Task 7: Sidecar und QA-Report

**Files:**
- Create: `internal/enrich/sidecar.go`, `internal/enrich/sidecar_test.go`, `internal/enrich/qa.go`, `internal/enrich/qa_test.go`

**Interfaces:**
- Produces:
  - `type SidecarRow struct { StopID, Tactile, Bench, Shelter string; BicycleParking100m, Elevators100m int }`
  - `func SidecarRows(matches []StopMatch, feats []osmfeat.Feature) []SidecarRow` — nur Stops mit `HasBest`, sortiert; Tags vom besten Kandidaten; Anzahlen = Features der Arten `bicycle_parking`/`elevator` innerhalb von 100 m.
  - `func WriteSidecar(path string, rows []SidecarRow) error` — gzip-CSV mit Header `stop_id,tactile_paving,bench,shelter,bicycle_parking_100m,elevators_100m`.
  - `type QAReport struct` mit den fünf Abschnitten der Spec (JSON-Tags in `snake_case`, jeweils Zähler und höchstens 50 Beispiele) und `func QA(feed *gtfsparser.Feed, feats []osmfeat.Feature, matches []StopMatch, rels []osmfeat.RouteRel, rm RouteMatchResult) QAReport`; `func WriteQA(path string, q QAReport) error`.

- [ ] **Step 1: Failing Tests** `TestSidecarRows` (gematchter Stop mit `Tactile:"yes"`, `Bench:"no"`, zwei Fahrradstellplätzen in 100 m, einem Aufzug in 150 m → Zeile `yes,no,"",2,0`; nicht gematchter Stop fehlt; Reihenfolge sortiert), `TestWriteSidecarHeader` (gunzip, erste Zeile = Header). `TestQA` (synthetisch: ein Stop ohne Gegenstück, ein Stop mit `stop_position` 20 m entfernt → Zähler `over_15m==1`, Median 20, ein OSM-Bus-Feature ohne GTFS-Stop in 50 m → `osm_without_gtfs` 1, ein Aufzug in 100 m einer Station gezählt, `rm.UnmatchedRoutes` wird übernommen; Beispiele auf 50 begrenzt).
- [ ] **Step 2:** Tests → FAIL. **Step 3:** Implementiere. **Step 4:** Tests → PASS. **Step 5:** Commit `feat: OSM stop attribute sidecar and QA report`.

### Task 8: Schritte, Flags, Orchestrierung, Verifikation

**Files:**
- Modify: `internal/enrich/report.go`, `internal/enrich/report_test.go`, `cmd/austria-gtfs-merger/main.go`, `cmd/austria-gtfs-merger/enrich.go`, `cmd/austria-gtfs-merger/enrich_test.go`
- Create: `scripts/check-main-unchanged.sh`

**Interfaces:**
- Produces:
  - `Steps` bekommt `PlatformCodes, OSMRoutes, Entrances bool`; `func (s Steps) NeedsOSM() bool`; `ParseSteps` kennt `platformcodes`, `osmroutes`, `entrances` (`all` setzt alle).
  - `validateEnrichFlags(steps enrich.Steps, osmOutput string) error` nutzt `NeedsOSM`.
  - `func runOSM(feed *gtfsparser.Feed, steps enrich.Steps, in osmInputs, palette map[string]bool) (reports []enrich.Report, used bool)` mit `type osmInputs struct{ FeaturesPath, RoutesPath, SidecarDir string }`: liest Features (und Relationen, wenn `osmroutes` gewählt), ruft `Match` einmal und danach `ApplyWheelchair`, `PlatformCodes`, `OSMRoutes`, `Entrances`, schreibt Sidecar und QA in `SidecarDir`. `ErrFormat`, fehlende oder leere Dateien → `used==false` mit Warnung; eine fehlende Relationen-Datei überspringt nur `osmroutes`.
  - Flags `--osm-output` (ersetzt `--wheelchair-output`), `--osm-routes`, `--osm-sidecar-dir`; `Routes`' `PaletteRoutes` wird an `runOSM` gereicht.

- [ ] **Step 1: Failing Tests**: `TestParseSteps` um die neuen Namen erweitern (`"all"` → alle sechs wahr), `TestValidateEnrichFlags` für `platformcodes`/`osmroutes`/`entrances` ohne Output (Fehler) und mit Output (ok), `TestNeedsOSM`; `TestRunOSMOldFormat` (Datei im alten Format → `used==false`, kein Panic, Feed unverändert).
- [ ] **Step 2:** Tests → FAIL. **Step 3:** Implementiere und `main.go` auf `runOSM` umstellen; `cur.WheelchairKnown` und die Warnungen aus dem bisherigen OSM-Block behalten; Flags umbenennen. **Step 4:** `go test ./...` → PASS.
- [ ] **Step 5:** `scripts/check-main-unchanged.sh <feed-dir> <osm-features> <osm-routes>`: ruft den Merger zweimal auf (ohne und mit allen OSM-Schritten, `--stats ""`, `--force`), vergleicht `main.zip` beider Läufe Datei für Datei außer `feed_info.txt` und gibt `UNCHANGED` oder die abweichende Datei aus.
- [ ] **Step 6 (Verifikation auf echten Daten):** Features/Relationen aus Task 2 verwenden. Run: `scripts/check-main-unchanged.sh /home/patricksteil/Projects/austria-gtfs-merger/gtfs_feeds <features> <routes>`. Expected: `UNCHANGED`. Danach `osm.zip` prüfen: `stops.txt` hat `wheelchair_boarding`, mehr gefüllte `platform_code` als im Hauptfeed, neue `location_type=2`-Zeilen mit `osm:entrance:`-IDs (falls Stationen ohne Feed-Eingang), Routen mit OSM-Farbe; Sidecar und QA-Report liegen im Verzeichnis. Zahlen in das Ledger schreiben.
- [ ] **Step 7:** Commit `feat: run all OSM stages and write sidecar and QA report`.

### Task 9: CI und README

**Files:**
- Modify: `.github/workflows/gtfs-merge.yml`, `README.md`, `.gitignore`, `scripts/check-tidy-fields.sh`

- [ ] **Step 1 (Messung, vor den CI-Änderungen):** Erweitere `check-tidy-fields.sh` um Zeilenzahlen von `pathways.txt` und Stops mit `location_type=2`. Lauf mit `osm.zip` aus Task 8 gegen die getidyte Fassung (Tidy-Befehl wie in der CI, lokal gebaut aus `…/scratchpad/gtfstidy`). Expected laut Vormessung: `pathways.txt` fällt auf 0 (dokumentiertes Verhalten von `--drop-non-essential`), Entrance-Stops bleiben. Ergebnis ins Ledger und an den Nutzer melden; die Entscheidung am Tidy-Schritt (Flag entfernen oder Pathways nachinjizieren) trifft der Nutzer, nicht dieser Plan.
- [ ] **Step 2:** Workflow: Cache-Pfad beide Dateien, Schlüssel `osm-features-v2-<Woche>` und `restore-keys: osm-features-v2-`; `osm-extract` mit `--routes-out osm-routes.json.gz`; Merger-Aufruf mit `--osm-output`, `--osm-routes osm-routes.json.gz`, `--osm-sidecar-dir osm-extras`; Release-Assets `osm-extras/osm-stop-attributes.csv.gz` und `osm-extras/osm-qa-report.json`; der Schritt "Remove stale OSM asset" entfernt alle drei Assets, wenn der Lauf keine OSM-Daten hatte; YAML-Syntax prüfen.
- [ ] **Step 3:** README: Abschnitt Enrichment um die vier neuen Schritte und die drei Flags ergänzen; Tabelle der Release-Dateien um Sidecar und QA-Report (beide OSM-abgeleitet, ODbL) erweitern; im Abschnitt "Data sources" die neuen OSM-Verwendungen nennen.
- [ ] **Step 4:** `go test ./...`; Commit `ci,docs: publish sidecar and QA report, cache v2 extraction`.

---

## Self-Review

- **Spec-Abdeckung:** Platform Codes → Task 4; Relationen (Gruppen, Way-Member, Provenance) → Task 1, 2, 5; Eingänge → Task 6; Sidecar/QA → Task 7; Flags, Format-Version, Hauptfeed-Prüfung → Task 1, 8; gtfstidy-Befund und CI → Task 9; Matching-Kern und `stop_position`-Ausschluss → Task 3.
- **Typkonsistenz:** `StopMatch`, `candidate`, `RouteMatchResult`, `osmfeat.Feature`/`RouteRel` werden in den abhängigen Tasks mit denselben Namen genutzt; `Wheelchair` bleibt für bestehende Tests erhalten.
- **Annahme:** Die Wirkung der Eingangs-Pathways im veröffentlichten Feed hängt von der Entscheidung zu `--drop-non-essential` ab (Task 9, Step 1).
