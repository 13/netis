// Command gen rebuilds internal/oui/oui.txt.gz from the IEEE MA-L, MA-M and
// MA-S registries. Run from the repository root: go run ./internal/oui/gen
package main

import (
	"compress/gzip"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"netis/internal/oui"
)

var registries = []string{
	"https://standards-oui.ieee.org/oui/oui.csv",
	"https://standards-oui.ieee.org/oui28/mam.csv",
	"https://standards-oui.ieee.org/oui36/oui36.csv",
}

func main() {
	out := flag.String("o", "internal/oui/oui.txt.gz", "output file")
	flag.Parse()
	entries := map[string]string{}
	client := &http.Client{Timeout: 2 * time.Minute}
	for _, u := range registries {
		if err := fetch(client, u, entries); err != nil {
			log.Fatalf("%s: %v", u, err)
		}
	}
	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	f, err := os.Create(*out)
	if err != nil {
		log.Fatal(err)
	}
	zw, _ := gzip.NewWriterLevel(f, gzip.BestCompression)
	zw.ModTime = time.Time{} // reproducible output
	for _, k := range keys {
		fmt.Fprintf(zw, "%s\t%s\n", k, entries[k])
	}
	if err := zw.Close(); err != nil {
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %d prefixes to %s", len(keys), *out)
}

// fetch reads one registry CSV (Registry,Assignment,Organization Name,
// Organization Address) into entries, keyed by upper-case hex assignment.
func fetch(client *http.Client, url string, entries map[string]string) error {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "netis-oui-gen (+https://github.com/)")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("status %s", resp.Status)
	}
	r := csv.NewReader(resp.Body)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	if _, err := r.Read(); err != nil { // header
		return err
	}
	for {
		rec, err := r.Read()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if len(rec) < 3 {
			continue
		}
		key := strings.ToUpper(strings.TrimSpace(rec[1]))
		name := oui.Normalize(rec[2])
		if key == "" || name == "" || strings.EqualFold(name, "private") {
			continue
		}
		entries[key] = name
	}
}
