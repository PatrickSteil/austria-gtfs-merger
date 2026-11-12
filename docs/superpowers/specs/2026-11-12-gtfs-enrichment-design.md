# GTFS-Anreicherung: Steig-Namen, Routen-Metadaten, wheelchair_boarding

Status: Entwurf, 2026-11-12. Text deutsch, Code und Feldnamen englisch.

## Intent

Der gemergte Österreich-Feed (ca. 105k Stops, 3,5k Routen, 880k Trips) wird
vor allem von Routing-Engines gelesen. Drei Lücken stören dort:

1. Manche Boarding-Stops heißen nur wie ihr Steig ("13A", "1,2", "G1") statt
   wie die Haltestelle. `platform_code` ist kaum gefüllt.
2. Routen haben nur Basis-`route_type`-Werte. Die Produktkategorie
   (RJX, ICE, REX, S, ...) steckt nur im Präfix von `trip_short_name`.
   `route_color` ist zu 0,1 % gefüllt.
3. `wheelchair_boarding` gibt es in keinem Quell-Feed und damit auch nicht im
   Ergebnis. (`trips.wheelchair_accessible` ist zu 96 % gefüllt und bleibt
   unverändert.)

Die Anreicherung soll den Feed für Engines nützlicher machen, ohne ihn
unvorhersehbar zu verändern.

## Ziele

- Drei unabhängige Anreicherungsschritte in einem neuen Paket
  `internal/enrich/`, aufgerufen aus `main.go` nach `enforceParentStations`
  und vor den Sanity-Checks.
- Jeder Schritt füllt nur leere Felder oder korrigiert klar erkannte Muster.
  Quell-Werte werden nie stillschweigend überschrieben.
- Jeder Schritt erzeugt einen Report (Anzahl geänderter Stops/Routen, Beispiele,
  Konflikte) im Log und als `enrich-report.json`.
- Deterministische Ausgabe: gleicher Input und gleiche OSM-Datei ergeben
  byte-gleichen Output.
- Regeln (Kategorie-Tabelle, Steig-Muster, Farben) liegen als Daten bzw.
  Tabellen im Code, mit Tabellentests.

## Nicht-Ziele

- Shapes, Transfers, Pathways, Fares, `translations.txt`, Koordinaten-Korrektur,
  Frequencies. (Spätere Projekte.)
- Eigene Spalten im Output (der gtfswriter schreibt nur Standardfelder).
  Rohwerte, die ersetzt werden, stehen im Report statt im Feed.
- Änderung von `route_id`/`trip_id`/`stop_id`.

## Anti-Ziele (Fehlschlag, auch wenn es technisch läuft)

- Eine Engine verwirft Routen oder Trips wegen unbekannter `route_type`-Werte.
- Der tägliche Release wird durch einen Ausfall von Geofabrik/OSM blockiert.
- Falsche `wheelchair_boarding=1`-Werte: lieber `0` (unbekannt) als geraten.
- Ein Release springt zwischen "mit" und "ohne" Anreicherung, ohne dass es
  jemand merkt.

## Schritt 1: Steig-Namen

Befund (lokaler Feed): 67 458 Boarding-Stops (`location_type` leer), alle haben
einen Parent. Bei 2 168 weicht `stop_name` vom Parent-Namen ab, typischerweise
"1", "1,2", "10A", "13A-69A", "G1", "G12", "Bus", "Bahn", "Zug", "SEV", "Tram".
`platform_code` ist nur bei 3 413 Stops gefüllt.

Regel:
- Betroffen sind Stops mit Parent, deren Name vom Parent-Namen abweicht UND
  "steig-artig" ist (Muster: kurze Tokens aus Ziffern, Buchstaben und `,`/`-`/`/`,
  z. B. `^[0-9]{1,3}[A-Za-z]?([,-/][0-9]{1,3}[A-Za-z]?)*$`, `^G[0-9]+$`, plus eine
  kleine Liste Modus-Wörter: Bus, Bahn, Zug, Tram, SEV, ...).
- `stop_name` wird auf den Namen des Parents gesetzt.
- `platform_code` wird mit dem alten Namen gefüllt, falls leer und der alte Name
  ein Steig ist (nicht bei Modus-Wörtern; dort bleibt `platform_code` leer).
- Nicht steig-artige abweichende Namen bleiben unberührt und stehen im Report.
- Parents, die `enforceParentStations` synthetisch aus dem ersten Kind erzeugt
  hat, zählen nicht als Namensquelle (ihr Name ist selbst der Kind-Name).
- Keine Ableitung von `platform_code` aus der `stop_id`-Endung (nicht belegt).

## Schritt 2: Routen-Metadaten

Befund: Das Präfix von `trip_short_name` ist die Produktkategorie
(Anzahl Trips, Route-Typ 2): S 23k, R 13k, REX 11k, NJ 5,6k, RJX 2,7k, ICE 2k,
EN 1,9k, IC 1,3k, RJ 1,3k, CJX 1,3k, EC 1,2k, WB 0,8k, SLB 0,8k, IR, D, CAT, ...
Bei Typ 7: SB 86k, LIF 4k, FUN 0,8k. Eine Route (z. B. `A1`) mischt oft mehrere
Produkte, `route_short_name` ist daher kein verlässliches Signal. Die
`route_long_name` der ÖBB-Routen ist teils unpassend (S2 mit "Wien - ... Lindau")
oder Fragezeichen-Müll ("?????") und wird nicht angefasst, außer reine
Fragezeichen-Namen zu leeren.

Regeln:
- **Kategorie je Trip** aus dem Präfix von `trip_short_name`. **Typ je Route**
  = Typ der häufigsten Kategorie der Trips (Gleichstand: höherwertiger Typ).
  Gemischte Routen stehen im Report. Routen werden nicht aufgeteilt.
- **Erweiterte `route_type`** (HVT), Tabelle (mein Vorschlag, im Test zu prüfen):
  RJ, RJX, ICE → 101; IC, EC, D, WB → 102; NJ, EN → 105; IR → 103;
  R, REX, CJX, Privatbahn-Linien (SLB, PLB, ATB, SP, ER, RR, RB, OS) → 106;
  S → 109; unbekannt → bleibt 2.
  Busse: Routen mit Namen/Kategorie SEV, "BUS SEV", "Schienenersatz" → 714,
  sonst bleibt 3.
  Typ 7 mit Kategorie SB/LIF → 6 (Aerial lift), FUN bleibt 7. (Mein Vorschlag;
  die Quell-Feeds setzen 7 für alles.)
- **Opt-in**: erweiterte Typen nur mit `--extended-route-types`, weil nicht jede
  Engine sie versteht. Ohne das Flag bleibt `route_type` unverändert und die
  Kategorie wird nur in `route_desc` geschrieben (z. B. "Railjet Xpress"), sofern
  leer.
- **Farben**: `route_color`/`route_text_color` nur wenn beide leer. Palette je
  Produktkategorie (Bahn) in einer Tabelle, `route_text_color` aus Kontrast
  (WCAG) berechnet. Start klein; Erweiterung um Agency-spezifische Farben später.
- Fehlende `route_long_name` (10,5 %) werden in diesem Projekt nicht aufgefüllt.

## Schritt 3: wheelchair_boarding aus OSM

Befund: kein Quell-Feed liefert `wheelchair_boarding`.

Datenquelle: Geofabrik `austria-latest.osm.pbf` (ca. 800 MB). Zwei Stufen:
1. **Extraktion** (schwer, höchstens wöchentlich bzw. bei neuer PBF-Version):
   relevante OSM-Objekte (`public_transport=platform|stop_position|station`,
   `highway=bus_stop`, `railway=platform|tram_stop|halt|station|subway_entrance`,
   `amenity=bus_station|ferry_terminal`, `aerialway=station`) mit ihren Tags
   `wheelchair`, `name`, `ref`, `local_ref`, Koordinate (Weg: Schwerpunkt) in
   eine kleine Zwischendatei (`osm-accessibility.json.gz`, in der Größenordnung
   MB). Die Datei liegt im GitHub-Actions-Cache, nicht im Repo.
2. **Matching** (schnell, bei jedem Merge): Zwischendatei gegen die Stops.

Matching je Boarding-Stop:
- Kandidaten im Radius (Vorschlag 30 m; Bahnsteig-Ways bis 40 m), passend zum
  Modus der Linien, die den Stop bedienen (Bus ↔ Bushalt, Bahn ↔ Bahnsteig).
- Rang: Modus-Passung, `ref`/`local_ref` = `platform_code`, Namens-Ähnlichkeit,
  Distanz.
- Wert: `wheelchair=yes` → 1, `no` → 2, `limited` oder fehlend → 0.
  Widersprechen sich gleich gute Kandidaten (yes vs. no) → 0.
- Eigene Werte der Quell-Feeds gewinnen (derzeit gibt es keine).
- Parent-Stations: 1, wenn alle Boarding-Kinder 1 haben; 2, wenn alle 2 haben;
  sonst 0. (Geändert nach dem Review: Kinder mit Wert 0 erben den Wert der
  Station, ein Parent mit 1 würde unbekannte Kinder als barrierefrei
  veröffentlichen.)
- Report: Abdeckung (Anteil 1/2/0 je Modus), Ambiguitäten, Median-Distanz.

Ausfall-Verhalten: Gibt es weder frischen Download noch Cache, wird der Schritt
mit Warnung übersprungen und der Feed ohne `wheelchair_boarding` geschrieben. Der
Release wird nie dadurch blockiert. Die Abdeckung steht im Report, damit ein
Einbruch auffällt (kein harter Check).

## Ablauf und Schnittstelle

```
parse → enforceParentStations → enrich.Steig → enrich.Routes → enrich.Wheelchair
      → cleanHeadsigns → Sanity-Checks → write
```

- Flags: `--enrich` (Liste: `steig,routes,wheelchair` oder `all`; Default
  `steig,routes`), `--extended-route-types`, `--osm-pbf <Pfad>` bzw.
  `--osm-cache <Dir>`, `--enrich-report <Datei>`.
- CI: `steig,routes` immer; `wheelchair` zusätzlich, sobald OSM-Cache
  funktioniert.
- Der Report wird als Release-Asset mitveröffentlicht (Entscheidung offen).

## Rechtliches (zu klären)

OSM-Daten stehen unter ODbL. Ein Feed, der OSM-abgeleitete Attribute enthält,
braucht Namensnennung ("© OpenStreetMap contributors") und wahrscheinlich eine
Share-Alike-Prüfung. Die DBP-Quell-Feeds haben eigene Lizenzen. Vor dem ersten
Release mit `wheelchair_boarding` klären, wo die Attribution steht
(`feed_info`, README, Release-Text).

## Tests

- Tabellentests: Steig-Muster (positive und negative Beispiele aus dem echten
  Feed), Kategorie→Typ, Mehrheits-/Gleichstandsregel, Farbkontrast.
- Matching: synthetische Mini-OSM-Features, inkl. Widerspruch, Moduswechsel,
  Ref-Match.
- Regression gegen den echten gemergten Feed: Zahl geänderter Stops/Routen und
  Anteil `wheelchair_boarding`≠0 im Report, nicht als harte Grenze.
- Der MobilityData-Validator läuft vor und nach der Anreicherung; es dürfen
  keine neuen Fehler entstehen.

## Entscheidungen des Autors (nach Review, zum Gegenlesen)

- Modell und Writer (gtfsparser/gtfswriter) tragen `wheelchair_boarding`,
  `platform_code`, `route_desc`, `route_color`, `route_text_color` bereits
  (geprüft). Kein Zusatzaufwand.
- **Kein stilles Umspringen**: `merge-stats.json` bekommt `wheelchair_known`
  (Stops mit Wert ≠ 0) und `extended_route_types`. Fällt `wheelchair_known` um
  mehr als 20 % gegenüber dem letzten Lauf oder wird der Schritt übersprungen,
  gibt der Merge eine laute Warnung aus und der CI-Job annotiert sie
  (`::warning::`). Der Release wird nicht blockiert.
- **Steig-Randfälle**: Ist `platform_code` schon gesetzt und weicht vom alten
  Namen ab, bleibt `platform_code` und der Fall steht als Konflikt im Report.
  Modus-Wörter (Bus, Bahn, ...) sind eine erweiterbare Liste im Code. Der alte
  Name von Modus-Wörtern steht nur im Report.
- **Kategorie-Regeln**: Trips ohne oder mit rein numerischem `trip_short_name`
  haben keine Kategorie und zählen nicht in die Mehrheit; hat keine Trip eine
  Kategorie, bleibt der Typ. Unbekannte Präfixe zählen als eigene Kategorie
  (Typ unverändert). SEV wird über Routenname oder Trip-Präfix erkannt.
  `route_desc` wird für alle Routen mit erkannter Kategorie geschrieben, wenn
  sie leer ist.
- **Matching konservativ**: Bei Zweifel 0. Zielabdeckung ist keine Vorgabe;
  der Report zeigt sie je Modus, und die Radien werden anhand einer
  Stichprobe von ca. 50 bekannten Bahnhöfen/Haltestellen von Hand geprüft.
- **CI/Cache**: Extraktion im täglichen Workflow, aber nur bei neuer PBF
  (Prüfung per HTTP `Last-Modified`), Cache-Schlüssel = PBF-Datum. Ein Cache
  bis 60 Tage alt wird still genutzt, älter mit Warnung; schlägt der Download
  fehl und ein Cache existiert, wird der Cache mit Warnung genutzt.
- **Report** wird als Release-Asset veröffentlicht (klein, nützlich).

## Entscheidungen des Nutzers (2026-11-12)

1. CI schaltet `--extended-route-types` ein. Zielgruppe sind vermutlich
   Routing-Engines; ein konkreter Zweck steht nicht fest.
2. Schritt 3 (`wheelchair`) läuft in CI. Die OSM-Attribution
   "© OpenStreetMap contributors (ODbL)" steht im README und in `feed_info.txt`.
   Die Share-Alike-Frage ist damit nicht juristisch geklärt, sondern vom Nutzer
   entschieden.

Folgen für den Entwurf:
- `feed_info.txt` kennt kein Attributionsfeld. Meine Umsetzung (Annahme):
  `injectFeedInfo` hängt die Attribution an `feed_publisher_name` an
  ("austria-gtfs-merger; contains data © OpenStreetMap contributors (ODbL)"),
  und es kommt zusätzlich eine standardkonforme `attributions.txt` dazu
  (Organisation "OpenStreetMap contributors", `is_producer`=0, URL zur
  Copyright-Seite). Die Attribution gilt nur, solange der OSM-Schritt aktiv
  war; fällt er aus, entfällt sie.
- **Risiko gtfstidy**: `.github/workflows/gtfs-merge.yml` lässt nach dem Merge
  `gtfstidy --drop-non-essential --fix ...` laufen. Das kann `route_desc`,
  `platform_code`, `wheelchair_boarding`, Farben oder `attributions.txt` wieder
  entfernen und mit `--fix` Werte ändern. `feed_info.txt` wird deshalb heute
  separat wieder eingefügt. Der Plan muss zuerst empirisch prüfen, was von den
  neuen Feldern den Tidy-Lauf überlebt, und `attributions.txt` analog zu
  `feed_info.txt` nachinjizieren.
- Wheelchair-Abdeckung in `merge-stats.json`: Die Warnung (nicht blockierend)
  aus den Autorenentscheidungen bleibt.

## Dem Builder überlassen

PBF-Bibliothek bzw. Streaming-Methode (Speicher unter ca. 2 GB, Laufzeit wenige
Minuten), räumlicher Index, genaue Regex, Datenformat der Zwischendatei,
Struktur der Tabellen, Aufteilung in Commits.

## Nachtrag: Trennung nach Lizenz (2026-11-12)

Nach der Lizenzprüfung (OSM-Auszug mit rund 8 000 übernommenen Werten gilt als
"substanziell", `stops.txt` damit als abgeleitete Datenbank) wird
`wheelchair_boarding` nicht mehr in den Hauptfeed geschrieben:

- `austria.merged.zip` (Permalink unverändert) enthält keine OSM-Daten.
- `austria.merged.osm.zip` ist derselbe Feed plus `wheelchair_boarding`;
  `stops.txt` wird unter ODbL angeboten. Erzeugt über `--wheelchair-output`.
- Der Schritt `wheelchair` läuft nur zusammen mit `--wheelchair-output`; der
  Merger bricht sonst ab. Die Variante wird nach dem Hauptfeed aus derselben
  Feed-Struktur geschrieben.
- Fehlt OSM, wird kein OSM-Asset erzeugt und ein altes Asset aus dem Release
  entfernt.
- Offen und nicht geprüft: ob die Lizenz der Mobilitätsverbünde (eigene
  Lizenz, an CC BY angelehnt) die Weitergabe in einer Datei mit ODbL-Anteil
  erlaubt, und der genaue Wortlaut der geforderten Namensnennung.
