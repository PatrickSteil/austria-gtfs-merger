# OSM-Anreicherung II: Platform Codes, Linien-Relationen, Eingänge, Sidecar, QA

Status: Entwurf, 2026-11-12. Baut auf `2026-11-12-gtfs-enrichment-design.md` auf
(Trennung nach Lizenz: Hauptfeed ohne OSM-Daten, `austria.merged.osm.zip` mit
OSM-Anteil unter ODbL). Text deutsch, Code und Feldnamen englisch.

## Intent

Die OSM-Daten (Österreich-Auszug von Geofabrik) enthalten mehr als das
`wheelchair`-Tag. Eine Messung auf dem Auszug (Nov. 2026) zeigt, was brauchbar
ist: 49 470 `stop_position`-Knoten, 12 581 Linien-Relationen (2 693 mit
`colour`, 12 085 mit `from`+`to`), 746 Eingänge, 2 389 Aufzüge, 21 400
Haltestellen mit `tactile_paving`, 32 674 mit Bank oder Unterstand, 16 067
Fahrradstellplätze, 6 178 Haltestellen-Knoten mit `ref`/`local_ref`. Dieses
Projekt nutzt das für Routing-Engines und für Qualitätskontrolle des Feeds.

## Entscheidungen des Nutzers

- Alle Ideen außer Shapes umsetzen, mit diesen Einschränkungen:
- **Übersetzungen** (`translations.txt` aus `name:*`): entfällt, weil OSM nur
  etwa 100 Haltestellennamen übersetzt (`name:de` 109, `name:en` 44).
- **Stop-Snapping**: nur Report, keine Koordinate wird verschoben.
- **Fußwege/Transfers** aus dem OSM-Fußwegegraph: entfällt (eigenes Projekt).
- **Eingänge**: Eingänge plus Walkway; Aufzüge/Treppen nur im Report.

## Ziele

Alle OSM-abgeleiteten Werte landen ausschließlich in `austria.merged.osm.zip`,
im Sidecar oder im QA-Report. Der Hauptfeed bleibt unverändert und ohne
OSM-Daten.

1. **Platform Codes** (`stops.txt:platform_code`) aus OSM-Plattform-Refs.
2. **Linien-Relationen**: `route_color` und `route_long_name` aus OSM-Relationen
   (`type=route`), zugeordnet über Modus, Ref und Haltestellen-Überlappung.
3. **Eingänge**: OSM-Eingänge als `location_type=2` mit Pathways zu den
   Bahnsteigen, nur wo das Feed an der Station keinen Eingang hat.
4. **Sidecar** `osm-stop-attributes.csv.gz`: `tactile_paving`, `bench`,
   `shelter`, Anzahl Fahrradstellplätze und Aufzüge im Umkreis je Haltestelle.
5. **QA-Report** `osm-qa-report.json`: Haltestellen ohne OSM-Gegenstück,
   Abstand zur `stop_position`, OSM-Haltestellen ohne GTFS-Stop, Zuordnung der
   Linien-Relationen. Er verändert den Feed nicht.

## Nicht-Ziele

Shapes, Übersetzungen, Verschieben von Koordinaten, Fußwege-Transfers,
vollständige Pathway-Topologie (Aufzüge, Treppen), Änderungen am Hauptfeed.

## Anti-Ziele

- Ein OSM-Wert überschreibt einen Quellwert still.
- Falsche Zuordnung statt keiner: bei Zweifel bleibt das Feld leer.
- Eine Linien-Relation färbt oder benennt eine falsche GTFS-Route.
- Erfundene Verbindungen: ein Pathway ohne belegten Eingang.
- Der Release wird durch einen OSM-Ausfall blockiert.

## Gemeinsamer Matching-Kern

Das Matching aus der Rollstuhl-Stufe (Raster-Index, Radius 30 m / 40 m für
Flächen, gleicher Modus, Rang nach Ref, Name, Abstand, Gleichstand =
Konflikt) wird einmal berechnet und von allen Stufen genutzt, statt jede
Stufe neu zu matchen. Ergebnis je Boarding-Stop: bester Kandidat, Menge der
gleich guten Kandidaten, Konfliktflag. Die vorhandenen Rollstuhl-Tests bleiben
die Absicherung des Refactorings.

## Stufe 1: Platform Codes

- Quelle: neues Feld `Platform` je OSM-Feature. Für Bahn, Tram, U-Bahn aus
  `ref` oder `local_ref`; für Bus nur aus `local_ref` (das `ref` von Bushaltestellen
  ist meist eine Betreiber-Nummer, kein Steig).
- Nur gültige Werte: 1–4 Zeichen aus Ziffern/Buchstaben, ohne `;` oder
  Leerzeichen.
- Gesetzt wird `platform_code`, wenn es leer ist, der Kandidat eindeutig ist
  (kein anderer gleich guter Kandidat mit anderem Wert) und der Abstand
  höchstens 15 m beträgt. Weicht ein bestehender Wert ab, bleibt er und der Fall
  steht als Konflikt im Report.

## Stufe 2: Linien-Relationen

- Extraktion: eigene Datei `osm-routes.json.gz` mit je Relation: ID, Modus
  (`bus|train|tram|subway|light_rail|trolleybus|ferry|funicular`), `ref`,
  `name`, `from`, `to`, `network`, `operator`, `colour`, und die Koordinaten
  der Haltestellen-Member (Knoten mit Rolle `stop*` oder `platform*`).
- Zuordnung GTFS-Route → Relation: gleicher Modus (Route-Typ → Modus wie im
  Rollstuhl-Schritt), gleiche normalisierte Ref (Kleinbuchstaben ohne
  Leerzeichen und Bindestriche, verglichen mit `route_short_name`) und
  Überlappung der Haltestellen: Anteil der Haltestellen der kleineren Menge,
  die innerhalb von 40 m eine Entsprechung haben, mindestens 0,6 bei
  mindestens 2 gemeinsamen Haltestellen. Mehrere Treffer: höchste Überlappung
  gewinnt, Gleichstand = nicht zugeordnet (Report: ambiguous).
- Wirkung: `route_color` aus `colour` (gültiges Hex, mit oder ohne `#`), wenn
  die Route keine Quellfarbe hat. Eine von unserer Kategorie-Palette vergebene
  Farbe zählt als nicht gesetzt und wird ersetzt. `route_text_color` aus dem
  Kontrast. `route_long_name` als "from – to", wenn leer und beide Tags da sind.

## Stufe 3: Eingänge

- OSM-Eingänge (`railway=subway_entrance` oder `entrance=*` mit
  `railway`/`public_transport`-Tag) innerhalb von 80 m der nächsten Station
  (`location_type=1`), deren Boarding-Stops Bahn, U-Bahn oder Tram bedienen und
  an der das Feed noch keinen Eingang (`location_type=2`) hat.
- Neuer Stop: ID `osm:entrance:<osm-id>`, Name "<Station> Eingang" (plus Ref,
  falls vorhanden), `location_type=2`, `parent_station` = Station,
  `wheelchair_boarding` aus dem Tag.
- Pathways: Ein Pathway darf in GTFS nicht an einer Station (`location_type=1`)
  enden. Deshalb Walkway (`pathway_mode=1`, bidirektional) vom Eingang zu den
  Boarding-Stops der Station (nach dem Review: alle, damit kein Bahnsteig
  unerreichbar bleibt), `length` = Luftlinie,
  `traversal_time` = 30 s + Luftlinie bei 1 m/s. Das ist eine Schätzung und
  steht so im Report. (Abweichung von der Zusage "ein Walkway zur Station".)
- Erwartete Wirkung ist klein: das Feed enthält schon 1 747 Eingänge an
  1 095 Stationen.

## Stufe 4: Sidecar

`osm-stop-attributes.csv.gz`, eine Zeile je gematchtem Boarding-Stop:
`stop_id, tactile_paving, bench, shelter, bicycle_parking_100m, elevators_100m`.
Die Tags kommen vom besten Kandidaten (wie bei Rollstuhl), die Anzahlen aus dem
Umkreis von 100 m. Das ist keine GTFS-Datei und bleibt außerhalb der Zip.

## Stufe 5: QA-Report

`osm-qa-report.json`, jeweils Zähler und höchstens 50 Beispiele:
- GTFS-Stops ohne OSM-Gegenstück (nach Modus);
- Abstand der Stops zur nächsten `stop_position` gleichen Modus: Median,
  Anzahl über 15 m und über 30 m, die 50 größten Abweichungen;
- OSM-Haltestellen ohne GTFS-Stop im Umkreis von 50 m (nach Modus);
- Linien-Relationen: zugeordnet, mehrdeutig, ohne GTFS-Route; GTFS-Routen ohne
  Relation (nach Modus);
- Aufzüge und Treppen in Stationsnähe, nur gezählt.

## Schnittstellen und CI

- Flags: `--osm-output` (ersetzt `--wheelchair-output`, gleiche Bedeutung),
  `--osm-routes <Datei>`, `--osm-sidecar-dir <Verzeichnis>`. Schritte:
  `wheelchair`, `platformcodes`, `osmroutes`, `entrances`, `all`. Jeder dieser
  Schritte verlangt `--osm-output`; sonst bricht der Merger ab.
- `osm-extract` schreibt zusätzlich `osm-routes.json.gz` (`--routes-out`).
- CI: der Cache enthält beide Extraktionsdateien; Release-Assets
  `osm-stop-attributes.csv.gz` und `osm-qa-report.json` kommen zu
  `austria.merged.osm.zip`; fehlen OSM-Daten, werden alle drei alten Assets
  entfernt.
- Der Umfang des Zip-Inhalts ändert sich nur durch Eingänge (neue Zeilen in
  `stops.txt` und `pathways.txt`) sowie `platform_code`, Farben und Namen.

## Lizenz

Alles hier Abgeleitete ist ODbL-Material (Derivative Database bzw. Auszug
daraus). Zip, Sidecar und Report werden im README als OSM-abgeleitet und unter
ODbL ausgewiesen; die Attribution steht in `feed_info.txt`, `attributions.txt`
und der README. Der Hauptfeed bleibt ohne OSM.

## Tests

Tabellentests für Ref-Validierung, Farb-Parsing, Ref-Normalisierung; Matching
mit synthetischen Features (Platform Code gesetzt/Konflikt/mehrdeutig,
Relation zugeordnet/mehrdeutig/falscher Modus, Eingang nur ohne bestehenden
Eingang, Pathway-Zahl und -Länge); Sidecar-Spalten; QA-Zähler. Ein Lauf auf dem
echten Feed prüft die Zahlen im Report und dass der Hauptfeed unverändert bleibt
(kein `wheelchair_boarding`, keine neuen Zeilen).

## Dem Builder überlassen

Schwellen als benannte Konstanten (Radien, Überlappung), Datenformat der
Routen-Datei, Index-Struktur, genaue Report-Struktur, Aufteilung in Commits.

## Entscheidungen des Autors (nach Review)

- **Gemeinsame Richtungen einer Linie**: OSM hat meist eine Relation je
  Richtung. Relationen gleichen Modus und gleicher normalisierter Ref werden zu
  einer Gruppe zusammengefasst (Vereinigung der Haltestellen). Die Gruppe ist die
  Einheit der Zuordnung; mehrere Gruppen mit gleicher Ref (z. B. Buslinie "1" in
  verschiedenen Orten) werden über die Überlappung unterschieden, Gleichstand
  zwischen Gruppen = nicht zugeordnet. Farbe: nur wenn alle Relationen der Gruppe,
  die eine `colour` haben, dieselbe nennen. `route_long_name`: nur wenn die Gruppe
  genau ein Paar `from`/`to` hat oder zwei Relationen mit vertauschtem Paar
  (dann das Paar der Relation mit der kleineren OSM-ID); sonst leer lassen.
  `route_master` wird nicht gebraucht.
- **Haltestellen-Member**: Knoten und Wege mit Rolle `stop*` oder `platform*`
  zählen (Wege über den Schwerpunkt), Unter-Relationen nicht. Dafür braucht die
  Extraktion einen zusätzlichen Durchlauf (erst Relationen, dann Knoten/Wege).
- **Herkunft von Farben**: `enrich.Routes` merkt sich, welche Routen es selbst
  gefärbt hat (nicht serialisiert, `Report.PaletteRoutes`). Nur diese Farben darf
  die OSM-Stufe ersetzen; Quellfarben bleiben. Gleiches für `route_text_color`.
- **Hauptfeed unverändert**: Der Hauptfeed wird vor allen OSM-Stufen geschrieben;
  die OSM-Stufen verändern danach dieselbe Feed-Struktur im Speicher, die nicht
  mehr in den Hauptfeed zurückgeschrieben wird. Prüfung: Ein Lauf mit und ein Lauf
  ohne OSM-Schritte liefern einen Hauptfeed, der sich in jeder Datei außer
  `feed_info.txt` (enthält die Zeit) byteweise gleicht.
- **Eingänge**: Eine Station ist geeignet, wenn sie kein Kind mit
  `location_type=2` hat, ihre Boarding-Stops (über die bedienenden Routen) Bahn,
  U-Bahn oder Tram bedienen und im Umkreis von 80 m des OSM-Eingangs kein
  bestehender Feed-Eingang liegt. Ein Eingang geht nur an die nächste geeignete
  Station.
- **Sidecar**: Werte normalisiert (`yes`, `no`, leer); Anzahlen zählen Features,
  nicht `capacity`; nicht gematchte Stops fehlen. Das Extraktionsformat bekommt
  eine Versionsnummer (JSON-Objekt `{"version":2,...}`); `Read` lehnt andere
  Versionen ab, und der Cache-Schlüssel enthält die Version, damit ein alter
  Cache nicht mit neuem Code benutzt wird. Treppen werden nicht extrahiert; der
  QA-Report zählt nur Aufzüge.
- **Dateien**: `--osm-sidecar-dir` nimmt `osm-stop-attributes.csv.gz` und
  `osm-qa-report.json` auf. `enrich-report.json` bleibt als Kurzfassung.
- **Platform-Code-Abstand**: 15 m gilt für den besten Kandidaten (gleiche
  Rangfolge wie beim Matching); ist der beste weiter weg oder hat keinen
  gültigen Wert, wird nichts gesetzt.
- **gtfstidy und Pathways (neuer Befund)**: `--drop-non-essential` verwirft
  Levels, Pathways, Attributionen und `feed_info` bewusst. Gemessen: von 34 003
  Pathways bleiben 0; Eingänge als Stops (1 747) bleiben. Neue Pathways aus
  Stufe 3 würden im veröffentlichten Feed also ebenfalls verschwinden. Das ist
  eine Entscheidung am Tidy-Schritt der CI (Flag entfernen oder Pathways nach
  dem Tidy wieder einfügen) und gehört dem Nutzer. Stufe 3 wird trotzdem
  gebaut, schreibt Stops und Pathways und ist bis zu dieser Entscheidung in der
  CI wirkungsgleich mit "nur Eingangs-Stops".
