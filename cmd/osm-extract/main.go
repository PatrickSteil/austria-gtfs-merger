// osm-extract reduces an OSM PBF extract to the public transport features the
// merger needs for its wheelchair step (see internal/osmfeat).
//
// Usage: osm-extract --pbf austria-latest.osm.pbf --out osm-features.json.gz
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"runtime"
	"sort"

	"github.com/PatrickSteil/austria-gtfs-merger/internal/osmfeat"
	"github.com/paulmach/osm"
	"github.com/paulmach/osm/osmpbf"
	flag "github.com/spf13/pflag"
)

type wayInfo struct {
	feat  osmfeat.Feature
	nodes []osm.NodeID
}

func main() {
	pbf := flag.String("pbf", "", "input OSM PBF file")
	out := flag.String("out", "osm-features.json.gz", "output features file")
	flag.Parse()
	log.SetFlags(0)
	if *pbf == "" {
		log.Fatal("error   --pbf is required")
	}

	feats, err := extract(*pbf)
	if err != nil {
		log.Fatalf("error   %v", err)
	}
	if len(feats) == 0 {
		log.Fatal("error   no features found; refusing to write an empty file")
	}
	if err := osmfeat.Write(*out, feats); err != nil {
		log.Fatalf("error   writing %s: %v", *out, err)
	}
	counts := map[string]int{}
	for _, f := range feats {
		counts[f.Kind]++
	}
	kinds := make([]string, 0, len(counts))
	for k := range counts {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		log.Printf("kind    %-10s %d", k, counts[k])
	}
	log.Printf("done    %d features written to %s", len(feats), *out)
}

func feature(tags osm.Tags, lat, lon float64, area bool) (osmfeat.Feature, bool) {
	m := tags.Map()
	kind, ok := osmfeat.ClassifyTags(m)
	if !ok {
		return osmfeat.Feature{}, false
	}
	ref := m["local_ref"]
	if ref == "" {
		ref = m["ref"]
	}
	return osmfeat.Feature{
		Lat: lat, Lon: lon, Kind: kind, Area: area,
		Wheelchair: osmfeat.NormalizeWheelchair(m["wheelchair"]),
		Name:       m["name"], Ref: ref,
	}, true
}

func scan(path string, fn func(o osm.Object)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	s := osmpbf.New(context.Background(), f, runtime.NumCPU())
	defer s.Close()
	for s.Scan() {
		fn(s.Object())
	}
	return s.Err()
}

func extract(path string) ([]osmfeat.Feature, error) {
	var feats []osmfeat.Feature
	var ways []wayInfo
	needed := map[osm.NodeID]struct{}{}

	// Pass 1: tagged nodes become features; tagged ways are remembered with
	// their node list so pass 2 can compute their centroid.
	err := scan(path, func(o osm.Object) {
		switch v := o.(type) {
		case *osm.Node:
			if f, ok := feature(v.Tags, v.Lat, v.Lon, false); ok {
				feats = append(feats, f)
			}
		case *osm.Way:
			f, ok := feature(v.Tags, 0, 0, true)
			if !ok || len(v.Nodes) == 0 {
				return
			}
			ids := make([]osm.NodeID, len(v.Nodes))
			for i, n := range v.Nodes {
				ids[i] = n.ID
				needed[n.ID] = struct{}{}
			}
			ways = append(ways, wayInfo{f, ids})
		}
	})
	if err != nil {
		return nil, fmt.Errorf("pass 1: %w", err)
	}
	log.Printf("pass1   %d node features, %d ways", len(feats), len(ways))

	// Pass 2: coordinates of the nodes the ways reference.
	type ll struct{ lat, lon float64 }
	coords := make(map[osm.NodeID]ll, len(needed))
	err = scan(path, func(o osm.Object) {
		if n, ok := o.(*osm.Node); ok {
			if _, want := needed[n.ID]; want {
				coords[n.ID] = ll{n.Lat, n.Lon}
			}
		}
	})
	if err != nil {
		return nil, fmt.Errorf("pass 2: %w", err)
	}

	for _, w := range ways {
		var lat, lon float64
		n := 0
		for _, id := range w.nodes {
			if c, ok := coords[id]; ok {
				lat += c.lat
				lon += c.lon
				n++
			}
		}
		if n == 0 {
			continue
		}
		w.feat.Lat, w.feat.Lon = lat/float64(n), lon/float64(n)
		feats = append(feats, w.feat)
	}
	return feats, nil
}
