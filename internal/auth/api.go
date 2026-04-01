package auth

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/PatrickSteil/austria-gtfs-merger/internal/models"
)

func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

const DBP_BASE = "https://data.mobilitaetsverbuende.at"

func GetDatasets(aut *DBPAuth) ([]models.Dataset, error) {
	headers, err := aut.Header()
	if err != nil {
		return nil, err
	}

	req, _ := http.NewRequest("GET", DBP_BASE+"/api/public/v1/data-sets", nil)
	req.Header = headers

	q := req.URL.Query()
	q.Add("tagFilterModeInclusive", "true")
	req.URL.RawQuery = q.Encode()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("DefaultClient() failed due to %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("request failed: %d", resp.StatusCode)
	}

	var datasets []models.Dataset
	err = json.NewDecoder(resp.Body).Decode(&datasets)

	return datasets, err
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

	text := (ds.NameDe + ds.NameEn)
	if containsIgnoreCase(text, "flex") {
		return false
	}

	return true
}
