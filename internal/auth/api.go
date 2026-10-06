package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/PatrickSteil/austria-gtfs-merger/internal/models"
)

func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

const dbpBase = "https://data.mobilitaetsverbuende.at"

// DBPBase is exported for use in other packages (e.g. download).
const DBPBase = dbpBase

func GetDatasets(aut *DBPAuth) ([]models.Dataset, error) {
	headers, err := aut.Header()
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("GET", dbpBase+"/api/public/v1/data-sets", nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header = headers

	q := req.URL.Query()
	q.Add("tagFilterModeInclusive", "true")
	req.URL.RawQuery = q.Encode()

	resp, err := aut.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("datasets request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("datasets request returned status %d", resp.StatusCode)
	}

	var datasets []models.Dataset
	if err := json.NewDecoder(resp.Body).Decode(&datasets); err != nil {
		return nil, fmt.Errorf("datasets decode failed: %w", err)
	}

	return datasets, nil
}

func IsGTFS(ds models.Dataset) bool {
	hasGTFS := false
	for _, t := range ds.Tags {
		if t.ValueEn == "GTFS" {
			hasGTFS = true
			break
		}
	}

	if !hasGTFS {
		return false
	}

	text := ds.NameDe + ds.NameEn
	return !containsIgnoreCase(text, "flex")
}
