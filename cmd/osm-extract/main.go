// osm-extract reduces an OSM PBF extract to what the merger needs from
// OpenStreetMap: public transport features (stops, platforms, entrances,
// elevators, bicycle parking) and public transport route relations (see
// internal/osmfeat).
//
// Usage:
//
//	osm-extract --pbf austria-latest.osm.pbf \
//	    --out osm-features.json.gz --routes-out osm-routes.json.gz
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"runtime"
	"sort"
	"strings"

	"github.com/PatrickSteil/austria-gtfs-merger/internal/osmfeat"
	"github.com/paulmach/osm"
	"github.com/paulmach/osm/osmpbf"
	flag "github.com/spf13/pflag"
)

type point struct{ lat, lon float64 }

// wayInfo is a tagged way (or a way that is a relation member) with its nodes.
type wayInfo struct {
	feat  *osmfeat.Feature // nil if the way only matters as a relation member
	nodes []osm.NodeID
}

// relInfo is a route relation with the IDs of its stop and platform members.
type relInfo struct {
	rel   osmfeat.RouteRel
	nodes []osm.NodeID
	ways  []osm.WayID
}

func main() {
	pbf := flag.String("pbf", "", "input OSM PBF file")
	out := flag.String("out", "osm-features.json.gz", "output features file")
	routesOut := flag.String("routes-out", "", "output route relations file (empty: do not write)")
	flag.Parse()
	log.SetFlags(0)
	if *pbf == "" {
		log.Fatal("error   --pbf is required")
	}

	feats, rels, err := extract(*pbf)
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
		log.Printf("kind    %-16s %d", k, counts[k])
	}
	log.Printf("done    %d features written to %s", len(feats), *out)

	if *routesOut != "" {
		if len(rels) == 0 {
			log.Fatal("error   no route relations found; refusing to write an empty file")
		}
		if err := osmfeat.WriteRoutes(*routesOut, rels); err != nil {
			log.Fatalf("error   writing %s: %v", *routesOut, err)
		}
		log.Printf("done    %d route relations written to %s", len(rels), *routesOut)
	}
}

// feature builds a Feature from an object's tags, or reports false if the
// object is not relevant.
func feature(id int64, tags osm.Tags, lat, lon float64, area bool) (osmfeat.Feature, bool) {
	return osmfeat.FeatureFromTags(id, tags.Map(), lat, lon, area)
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

func isStopRole(role string) bool {
	return strings.HasPrefix(role, "stop") || strings.HasPrefix(role, "platform")
}

func extract(path string) ([]osmfeat.Feature, []osmfeat.RouteRel, error) {
	// Pass A: route relations and the IDs of their stop and platform members.
	var rels []*relInfo
	memberNodes := map[osm.NodeID]struct{}{}
	memberWays := map[osm.WayID]struct{}{}
	err := scan(path, func(o osm.Object) {
		r, ok := o.(*osm.Relation)
		if !ok {
			return
		}
		rel, ok := osmfeat.RouteRelFromTags(int64(r.ID), r.Tags.Map())
		if !ok {
			return
		}
		ri := &relInfo{rel: rel}
		for _, m := range r.Members {
			if !isStopRole(m.Role) {
				continue
			}
			switch m.Type {
			case osm.TypeNode:
				ri.nodes = append(ri.nodes, osm.NodeID(m.Ref))
				memberNodes[osm.NodeID(m.Ref)] = struct{}{}
			case osm.TypeWay:
				ri.ways = append(ri.ways, osm.WayID(m.Ref))
				memberWays[osm.WayID(m.Ref)] = struct{}{}
			}
		}
		rels = append(rels, ri)
	})
	if err != nil {
		return nil, nil, fmt.Errorf("pass A: %w", err)
	}
	log.Printf("passA   %d route relations", len(rels))

	// Pass B: tagged nodes become features, tagged ways are kept with their
	// node lists, and coordinates of relation member nodes are recorded.
	var feats []osmfeat.Feature
	var ways []wayInfo
	coords := map[osm.NodeID]point{}
	wayNodes := map[osm.WayID][]osm.NodeID{}
	needed := map[osm.NodeID]struct{}{}
	err = scan(path, func(o osm.Object) {
		switch v := o.(type) {
		case *osm.Node:
			if _, ok := memberNodes[v.ID]; ok {
				coords[v.ID] = point{v.Lat, v.Lon}
			}
			if f, ok := feature(int64(v.ID), v.Tags, v.Lat, v.Lon, false); ok {
				feats = append(feats, f)
			}
		case *osm.Way:
			f, tagged := feature(int64(v.ID), v.Tags, 0, 0, true)
			_, member := memberWays[v.ID]
			if (!tagged && !member) || len(v.Nodes) == 0 {
				return
			}
			ids := make([]osm.NodeID, len(v.Nodes))
			for i, n := range v.Nodes {
				ids[i] = n.ID
				needed[n.ID] = struct{}{}
			}
			wi := wayInfo{nodes: ids}
			if tagged {
				wi.feat = &f
			}
			ways = append(ways, wi)
			if member {
				wayNodes[v.ID] = ids
			}
		}
	})
	if err != nil {
		return nil, nil, fmt.Errorf("pass B: %w", err)
	}
	log.Printf("passB   %d node features, %d ways", len(feats), len(ways))

	// Pass C: coordinates of the nodes the ways reference.
	err = scan(path, func(o osm.Object) {
		if n, ok := o.(*osm.Node); ok {
			if _, want := needed[n.ID]; want {
				coords[n.ID] = point{n.Lat, n.Lon}
			}
		}
	})
	if err != nil {
		return nil, nil, fmt.Errorf("pass C: %w", err)
	}

	centroid := func(ids []osm.NodeID) (point, bool) {
		var lat, lon float64
		n := 0
		for _, id := range ids {
			if c, ok := coords[id]; ok {
				lat += c.lat
				lon += c.lon
				n++
			}
		}
		if n == 0 {
			return point{}, false
		}
		return point{lat / float64(n), lon / float64(n)}, true
	}

	for _, w := range ways {
		if w.feat == nil {
			continue
		}
		if c, ok := centroid(w.nodes); ok {
			w.feat.Lat, w.feat.Lon = c.lat, c.lon
			feats = append(feats, *w.feat)
		}
	}

	out := make([]osmfeat.RouteRel, 0, len(rels))
	for _, ri := range rels {
		for _, id := range ri.nodes {
			if c, ok := coords[id]; ok {
				ri.rel.Stops = append(ri.rel.Stops, osmfeat.Point{Lat: c.lat, Lon: c.lon})
			}
		}
		for _, id := range ri.ways {
			if c, ok := centroid(wayNodes[id]); ok {
				ri.rel.Stops = append(ri.rel.Stops, osmfeat.Point{Lat: c.lat, Lon: c.lon})
			}
		}
		out = append(out, ri.rel)
	}
	return feats, out, nil
}
