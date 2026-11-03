package food

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// USDAAttribution is shown with the USDA foods. The data is in the public
// domain; USDA asks to be cited.
const USDAAttribution = "U.S. Department of Agriculture, Agricultural Research Service. FoodData Central. https://fdc.nal.usda.gov/ (public domain)"

// The USDA table ships with the module as data/usda.json.gz, made from the SR
// Legacy and Foundation Foods CSV downloads on fdc.nal.usda.gov by:
// go run ./cmd/usda <the zips>
//
//go:embed data/usda.json.gz
var bundledUSDA []byte

// Table is a food table: its edition and its foods.
type Table struct {
	Version string `json:"version"`
	Foods   []Food `json:"foods"`
}

// BundledUSDA reads the USDA table that ships with the module.
func BundledUSDA() (Table, error) {
	var t Table
	zr, err := gzip.NewReader(bytes.NewReader(bundledUSDA))
	if err != nil {
		return t, err
	}
	if err = json.NewDecoder(zr).Decode(&t); err != nil {
		return t, err
	}
	for i := range t.Foods {
		t.Foods[i].Nutrition = true
		renameUSDA(&t.Foods[i])
	}
	return t, nil
}

// A few everyday foods hide behind USDA's names, "Beverages, coffee, brewed,
// espresso, restaurant-prepared": a search for espresso then puts any branded
// product called Espresso first, such as coffee beans whose calories are per
// 100 g of beans. These get the names people search for, in English, Dutch and
// French, and the serving they are drunk in. The USDA name stays as an alias.
var usdaRenames = map[string]Food{
	"171891": {Name: "Espresso, restaurant-prepared", NameNL: "Espresso", NameFR: "Café expresso",
		ServingG: 30, ServingLabel: "1 shot"},
	"174125": {Name: "Espresso, decaffeinated, restaurant-prepared", NameNL: "Espresso, cafeïnevrij",
		NameFR: "Café expresso, décaféiné", ServingG: 30, ServingLabel: "1 shot"},
	"171890": {Name: "Coffee, brewed", NameNL: "Koffie, zwart", NameFR: "Café noir", Aliases: "Black coffee, brewed",
		ServingG: 200, ServingLabel: "1 cup"},
	"171889": {Name: "Coffee, brewed, decaffeinated", NameNL: "Koffie, zwart, cafeïnevrij",
		NameFR: "Café noir, décaféiné", Aliases: "Black coffee, decaffeinated", ServingG: 200, ServingLabel: "1 cup"},
}

func renameUSDA(f *Food) {
	r, ok := usdaRenames[f.SourceID]
	if !ok || f.Name == r.Name {
		return
	}
	f.Aliases = strings.Trim(r.Aliases+"; "+f.Name, "; ")
	f.Name, f.NameNL, f.NameFR = r.Name, r.NameNL, r.NameFR
	f.ServingG, f.ServingLabel = r.ServingG, r.ServingLabel
}

// USDARenamed returns the foods of the table that the module renames, so an
// app can update them in a table it stored before they had those names.
func USDARenamed(t Table) []Food {
	var renamed []Food
	for _, f := range t.Foods {
		if _, ok := usdaRenames[f.SourceID]; ok {
			renamed = append(renamed, f)
		}
	}
	return renamed
}

// The FoodData Central nutrients kept, by their id. Foundation Foods
// often give energy only by the Atwater factors, so those stand in for 1008.
var usdaNutrients = map[string]func(*Food) *float64{
	"1008": func(f *Food) *float64 { return &f.Kcal },    // Energy (kcal)
	"1003": func(f *Food) *float64 { return &f.Protein }, // Protein (g)
	"1005": func(f *Food) *float64 { return &f.Carbs },   // Carbohydrate, by difference (g)
	"1004": func(f *Food) *float64 { return &f.Fat },     // Total lipid (fat) (g)
	"1079": func(f *Food) *float64 { return &f.Fiber },   // Fiber, total dietary (g)
	"2000": func(f *Food) *float64 { return &f.Sugar },   // Sugars, total (g)
}

const (
	usdaSodium         = "1093" // Sodium, Na (mg)
	usdaAtwaterGeneral = "2047" // Energy (Atwater General Factors) (kcal)
	usdaAtwaterSpec    = "2048" // Energy (Atwater Specific Factors) (kcal)
)

// the kinds of food taken from FoodData Central: generic foods, not the
// branded products (Open Food Facts has those) or survey recipes
var usdaDataTypes = map[string]bool{"sr_legacy_food": true, "foundation_food": true}

var usdaEdition = regexp.MustCompile(`(\d{4}-\d{2}(-\d{2})?)`)

// a note some names carry that says nothing about the food
var usdaProgramNote = regexp.MustCompile(`\s*\(Includes foods? for USDA's Food Distribution Program\)`)

// ReadUSDA reads FoodData Central CSV downloads (the SR Legacy and Foundation
// Foods zips): food.csv, food_nutrient.csv, food_category.csv and, for
// serving sizes, food_portion.csv. Foods without energy are left out. A food
// in more than one zip keeps the first.
func ReadUSDA(zipPaths ...string) (Table, error) {
	var t Table
	var editions []string
	seen := map[string]bool{}
	for _, p := range zipPaths {
		foods, edition, err := readUSDAZip(p)
		if err != nil {
			return t, fmt.Errorf("%s: %w", path.Base(p), err)
		}
		editions = append(editions, edition)
		for _, f := range foods {
			key := strings.ToLower(f.Name)
			if seen[key] {
				continue
			}
			seen[key] = true
			t.Foods = append(t.Foods, f)
		}
	}
	if len(t.Foods) == 0 {
		return t, errors.New("no foods with an energy value in the download")
	}
	sort.Strings(editions)
	t.Version = strings.Join(editions, " + ")
	return t, nil
}

func readUSDAZip(zipPath string) ([]Food, string, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, "", err
	}
	defer zr.Close()

	files := map[string]*zip.File{}
	edition := ""
	for _, f := range zr.File {
		base := strings.ToLower(path.Base(f.Name))
		switch base {
		case "food.csv", "food_nutrient.csv", "food_category.csv", "food_portion.csv", "measure_unit.csv":
			files[strings.TrimSuffix(base, ".csv")] = f
			if m := usdaEdition.FindString(f.Name); m != "" {
				edition = m
			}
		}
	}
	for _, kind := range []string{"food", "food_nutrient", "food_category"} {
		if files[kind] == nil {
			return nil, "", fmt.Errorf("the zip has no %s.csv", kind)
		}
	}
	if edition == "" {
		edition = path.Base(zipPath)
	}

	groups := map[string]string{}
	if err := eachCSVRow(files["food_category"], func(row map[string]string) {
		groups[row["id"]] = row["description"]
	}); err != nil {
		return nil, "", err
	}

	byID := map[string]*Food{}
	var order []string
	if err := eachCSVRow(files["food"], func(row map[string]string) {
		id := row["fdc_id"]
		if id == "" || byID[id] != nil || !usdaDataTypes[row["data_type"]] {
			return
		}
		byID[id] = &Food{SourceID: id, Name: usdaName(row["description"]), Group: groups[row["food_category_id"]]}
		order = append(order, id)
	}); err != nil {
		return nil, "", err
	}

	hasKcal := map[string]bool{}
	atwater := map[string]float64{}
	if err := eachCSVRow(files["food_nutrient"], func(row map[string]string) {
		f := byID[row["fdc_id"]]
		if f == nil {
			return
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(row["amount"]), 64)
		if err != nil || v < 0 {
			return
		}
		switch id := row["nutrient_id"]; id {
		case usdaSodium:
			f.Salt = v / 1000 * 2.5
		case usdaAtwaterGeneral, usdaAtwaterSpec:
			// the specific factors are closer; take them over the general ones
			if _, ok := atwater[f.SourceID]; !ok || id == usdaAtwaterSpec {
				atwater[f.SourceID] = v
			}
		default:
			if field := usdaNutrients[id]; field != nil {
				*field(f) = v
				if id == "1008" {
					hasKcal[f.SourceID] = true
				}
			}
		}
	}); err != nil {
		return nil, "", err
	}
	for id, kcal := range atwater {
		if !hasKcal[id] {
			byID[id].Kcal, hasKcal[id] = kcal, true
		}
	}

	units := map[string]string{}
	if files["measure_unit"] != nil {
		if err := eachCSVRow(files["measure_unit"], func(row map[string]string) {
			if row["name"] != "undetermined" {
				units[row["id"]] = row["name"]
			}
		}); err != nil {
			return nil, "", err
		}
	}
	if files["food_portion"] != nil {
		if err := eachCSVRow(files["food_portion"], func(row map[string]string) {
			f := byID[row["fdc_id"]]
			if f == nil || f.ServingG > 0 {
				return
			}
			g, err := strconv.ParseFloat(row["gram_weight"], 64)
			if label := portionLabel(row, units[row["measure_unit_id"]]); err == nil && g > 0 && label != "" {
				f.ServingG, f.ServingLabel = g, label
			}
		}); err != nil {
			return nil, "", err
		}
	}

	var out []Food
	for _, id := range order {
		if hasKcal[id] {
			f := byID[id]
			f.Nutrition = true
			f.Salt = float64(int(f.Salt*1000+0.5)) / 1000
			out = append(out, *f)
		}
	}
	return out, edition, nil
}

// usdaName tidies a food's description: no program note, no odd spaces.
func usdaName(s string) string {
	s = usdaProgramNote.ReplaceAllString(s, "")
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "\u00a0", " ")), " ")
}

// portionLabel names a portion the way people say it, "1 cup, chopped", from
// its amount and its description, modifier or unit. Modifiers that are only
// codes give no label.
func portionLabel(row map[string]string, unit string) string {
	what := strings.TrimSpace(row["portion_description"])
	if what == "" || strings.EqualFold(what, "Quantity not specified") {
		what = strings.TrimSpace(row["modifier"])
	}
	if what == "" || strings.Trim(what, "0123456789.") == "" {
		what = unit
	}
	if what == "" || strings.Trim(what, "0123456789.") == "" {
		return ""
	}
	amount := strings.TrimSpace(row["amount"])
	if n, err := strconv.ParseFloat(amount, 64); err == nil && n > 0 && !strings.HasPrefix(what, amount) {
		return strconv.FormatFloat(n, 'f', -1, 64) + " " + what
	}
	return what
}

// eachCSVRow calls fn with each row of a CSV file in a zip, by its header names.
func eachCSVRow(f *zip.File, fn func(map[string]string)) error {
	r, err := f.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	c := csv.NewReader(r)
	c.ReuseRecord = true
	c.FieldsPerRecord = -1
	header, err := c.Read()
	if err != nil {
		return fmt.Errorf("%s: %w", f.Name, err)
	}
	names := make([]string, len(header))
	for i, h := range header {
		names[i] = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\ufeff")))
	}
	row := map[string]string{}
	for {
		rec, err := c.Read()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%s: %w", f.Name, err)
		}
		clear(row)
		for i, v := range rec {
			if i < len(names) {
				row[names[i]] = v
			}
		}
		fn(row)
	}
}
