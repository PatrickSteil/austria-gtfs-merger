package download

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/PatrickSteil/austria-gtfs-merger/internal/auth"
)

func DownloadDataset(a *auth.DBPAuth, datasetID string, year string, filename string, outDir string) {
	os.MkdirAll(outDir, os.ModePerm)

	path := outDir + "/" + filename
	if _, err := os.Stat(path); err == nil {
		log.Printf("skip    %s (already exists)", path)
		return
	}

	url := fmt.Sprintf("%s/api/public/v1/data-sets/%s/%s/file", auth.DBP_BASE, datasetID, year)

	for attempt := 1; attempt <= 3; attempt++ {
		headers, _ := a.Header()

		req, _ := http.NewRequest("GET", url, nil)
		req.Header = headers

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("retry   %s (attempt %d/3): %v", filename, attempt, err)
			resp.Body.Close()
			time.Sleep(2 * time.Second)
			continue
		}

		if resp.StatusCode == 401 {
			log.Printf("retry   %s (attempt %d/3): 401 unauthorized, refreshing token", filename, attempt)
			// aut.Token = ""
			resp.Body.Close()
			continue
		}

		if resp.StatusCode != 200 {
			log.Printf("retry   %s (attempt %d/3): unexpected status %s", filename, attempt, resp.Status)
			resp.Body.Close()
			continue
		}

		file, _ := os.Create(path)
		defer file.Close()

		io.Copy(file, resp.Body)
		resp.Body.Close()

		log.Printf("saved   %s", path)
		return
	}

	log.Printf("failed  %s: all 3 attempts exhausted", filename)
}
