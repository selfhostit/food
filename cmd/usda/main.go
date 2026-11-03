// Command usda turns the FoodData Central CSV downloads from fdc.nal.usda.gov
// (SR Legacy and Foundation Foods) into the table that ships with the module,
// data/usda.json.gz.
//
//	go run ./cmd/usda FoodData_Central_sr_legacy_food_csv_2018-04.zip FoodData_Central_foundation_food_csv_2025-04-24.zip
package main

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/selfhostit/food"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/usda <FoodData Central CSV zip>...")
		os.Exit(2)
	}
	t, err := food.ReadUSDA(os.Args[1:]...)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_, here, _, _ := runtime.Caller(0)
	out := filepath.Join(filepath.Dir(here), "..", "..", "data", "usda.json.gz")
	if err := write(out, t); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("USDA %s: %d foods written to %s\n", t.Version, len(t.Foods), out)
}

func write(path string, t food.Table) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	zw, _ := gzip.NewWriterLevel(f, gzip.BestCompression)
	if err := json.NewEncoder(zw).Encode(t); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return f.Close()
}
