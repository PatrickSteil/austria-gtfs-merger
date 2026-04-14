package models

type Dataset struct {
	ID             string `json:"id"`
	NameDe         string `json:"nameDe"`
	NameEn         string `json:"nameEn"`
	Tags           []Tag  `json:"tags"`
	ActiveVersions []struct {
		Year           string `json:"year"`
		DataSetVersion struct {
			File struct {
				OriginalName string `json:"originalName"`
			} `json:"file"`
		} `json:"dataSetVersion"`
	} `json:"activeVersions"`
}

type Tag struct {
	ValueEn string `json:"valueEn"`
}

// ManifestEntry records what version of a dataset was last downloaded.
type ManifestEntry struct {
	Year         string `json:"year"`
	OriginalName string `json:"originalName"`
}

// Manifest maps dataset ID → last-downloaded version info.
type Manifest map[string]ManifestEntry
