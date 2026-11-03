package food

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The Open Food Facts credits. The data is under the Open Database Licence,
// the product photos under Creative Commons Attribution-ShareAlike.
const (
	OFFAttribution      = "Open Food Facts, https://world.openfoodfacts.org (Open Database Licence)"
	OFFPhotoAttribution = "Product photos: Open Food Facts contributors, CC BY-SA 3.0"
)

// The sites a barcode can be looked up in. They share one API: Open Food
// Facts has food, Open Products Facts other household products and Open
// Beauty Facts cosmetics.
const (
	OpenFoodFacts     = "https://world.openfoodfacts.org"
	OpenProductsFacts = "https://world.openproductsfacts.org"
	OpenBeautyFacts   = "https://world.openbeautyfacts.org"
)

// OFFExportURL is the monthly export of every Open Food Facts product, about a
// gigabyte compressed.
const OFFExportURL = "https://static.openfoodfacts.org/data/en.openfoodfacts.org.products.csv.gz"

// BelgiumNetherlands are the countries the selfhostit apps keep products for.
var BelgiumNetherlands = []string{"en:belgium", "en:netherlands"}

// ErrUnknownProduct means the site doesn't have the barcode, or has it
// without a name.
var ErrUnknownProduct = errors.New("unknown product")

// BarcodePattern matches the barcodes products are kept by: 6 to 14 digits.
var BarcodePattern = regexp.MustCompile(`^\d{6,14}$`)

// Client asks Open Food Facts and its sister sites for products. UserAgent
// says which app is asking, as they ask apps to: "Troye/1.2 (self-hosted
// calorie tracker)".
type Client struct {
	UserAgent string
	// HTTP makes the requests; nil means one with a 10 second timeout for
	// lookups, and no timeout for the export, which is read as it streams.
	HTTP *http.Client
}

var lookupClient = &http.Client{Timeout: 10 * time.Second}

func (c Client) do(req *http.Request, fallback *http.Client) (*http.Response, error) {
	req.Header.Set("User-Agent", c.UserAgent)
	client := c.HTTP
	if client == nil {
		client = fallback
	}
	return client.Do(req)
}

// The product fields asked for in a lookup.
var productFields = []string{
	"code", "product_name", "product_name_en", "product_name_nl", "product_name_fr", "generic_name",
	"brands", "quantity", "categories_tags", "image_front_small_url", "image_small_url",
	"nutriments", "serving_quantity", "serving_size",
}

// Product looks a barcode up on one of the sites, such as OpenFoodFacts.
func (c Client) Product(ctx context.Context, site, code string) (Food, error) {
	url := fmt.Sprintf("%s/api/v2/product/%s.json?fields=%s", strings.TrimSuffix(site, "/"), code,
		strings.Join(productFields, ","))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Food{}, err
	}
	res, err := c.do(req, lookupClient)
	if err != nil {
		return Food{}, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return Food{}, ErrUnknownProduct
	}
	if res.StatusCode != http.StatusOK {
		return Food{}, fmt.Errorf("status %d", res.StatusCode)
	}
	var body struct {
		Status  int            `json:"status"`
		Code    string         `json:"code"`
		Product map[string]any `json:"product"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return Food{}, err
	}
	if body.Status != 1 {
		return Food{}, ErrUnknownProduct
	}
	p := body.Product
	nutriments, _ := p["nutriments"].(map[string]any)
	field := func(key string) string {
		switch v := p[key].(type) {
		case string:
			return v
		case []any:
			// categories_tags is a list here and a comma-separated cell in
			// the export
			var parts []string
			for _, s := range v {
				if s, ok := s.(string); ok {
					parts = append(parts, s)
				}
			}
			return strings.Join(parts, ",")
		}
		if v, ok := number(p[key]); ok {
			return strconv.FormatFloat(v, 'f', -1, 64)
		}
		return ""
	}
	if body.Code == "" {
		body.Code = code
	}
	f, ok := product(body.Code, func(key string) string {
		switch key {
		case "image_small_url":
			if front := field("image_front_small_url"); front != "" {
				return front
			}
		}
		return field(key)
	}, func(key string) string {
		if v, ok := number(nutriments[key]); ok {
			return strconv.FormatFloat(v, 'f', -1, 64)
		}
		return ""
	})
	if !ok {
		return Food{}, ErrUnknownProduct
	}
	return f, nil
}

// product makes a food from a product, whether it came from the API or the
// export, given its fields and its nutrients per 100 g. It reports false for a
// product without a name.
func product(code string, field, nutrient func(string) string) (Food, bool) {
	num := func(s string) (float64, bool) {
		v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		return v, err == nil && !math.IsNaN(v) && !math.IsInf(v, 0)
	}
	per100g := func(key string) float64 {
		v, _ := num(nutrient(key + "_100g"))
		return math.Max(v, 0)
	}

	f := Food{
		SourceID:   code,
		Barcode:    code,
		NameNL:     strings.TrimSpace(field("product_name_nl")),
		NameFR:     strings.TrimSpace(field("product_name_fr")),
		Quantity:   strings.TrimSpace(field("quantity")),
		Categories: strings.TrimSpace(field("categories_tags")),
	}
	if url := strings.TrimSpace(field("image_small_url")); strings.HasPrefix(url, "https://") {
		f.ImageURL = url
	}
	// the English name, else Dutch or French, else the name in the product's
	// own language, which stays searchable as an alias
	for _, name := range []string{field("product_name_en"), f.NameNL, f.NameFR, field("product_name"), field("generic_name")} {
		if name = strings.TrimSpace(name); name != "" {
			f.Name = name
			break
		}
	}
	if f.Name == "" {
		return f, false
	}
	if own := strings.TrimSpace(field("product_name")); own != "" && own != f.Name && own != f.NameNL && own != f.NameFR {
		f.Aliases = own
	}
	brand, _, _ := strings.Cut(field("brands"), ",")
	f.Brand = strings.TrimSpace(brand)
	if g, ok := num(field("serving_quantity")); ok && g > 0 {
		f.ServingG, f.ServingLabel = g, field("serving_size")
	}

	kcal, ok := num(nutrient("energy-kcal_100g"))
	if !ok {
		// energy_100g is in kJ
		for _, key := range []string{"energy-kj_100g", "energy_100g"} {
			if kj, ok2 := num(nutrient(key)); ok2 {
				kcal, ok = kj/4.184, true
				break
			}
		}
	}
	if !ok || kcal < 0 {
		return f, true
	}
	f.Nutrition = true
	f.Kcal = kcal
	f.Protein = per100g("proteins")
	f.Carbs = per100g("carbohydrates")
	f.Fat = per100g("fat")
	f.Fiber = per100g("fiber")
	f.Sugar = per100g("sugars")
	f.Salt = per100g("salt")
	if f.Salt == 0 {
		f.Salt = per100g("sodium") * 2.5
	}
	return f, true
}

// Export downloads the Open Food Facts export from url (usually
// OFFExportURL) and returns it uncompressed, to read as it streams in.
func (c Client) Export(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	res, err := c.do(req, http.DefaultClient)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, fmt.Errorf("downloading the Open Food Facts export: status %d", res.StatusCode)
	}
	if !strings.HasSuffix(url, ".gz") {
		return res.Body, nil
	}
	zr, err := gzip.NewReader(res.Body)
	if err != nil {
		res.Body.Close()
		return nil, err
	}
	return gzipBody{zr, res.Body}, nil
}

type gzipBody struct {
	*gzip.Reader
	body io.Closer
}

func (g gzipBody) Close() error {
	g.Reader.Close()
	return g.body.Close()
}

// ReadExport reads the tab-separated export and hands add each product sold
// in one of the countries ("en:belgium") that has a barcode and a name. Those
// without nutrition come too, with Nutrition false; Plausible tells the
// products whose values can't be right.
func ReadExport(r io.Reader, countries []string, add func(Food) error) error {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 1<<20), 64<<20)
	if !s.Scan() {
		if err := s.Err(); err != nil {
			return err
		}
		return errors.New("the Open Food Facts export is empty")
	}
	col := map[string]int{}
	for i, h := range strings.Split(s.Text(), "\t") {
		col[h] = i
	}
	for _, need := range []string{"code", "product_name", "countries_tags"} {
		if _, ok := col[need]; !ok {
			return fmt.Errorf("the Open Food Facts export has no %s column", need)
		}
	}
	at := col["countries_tags"]
	for s.Scan() {
		line := s.Text()
		if !containsAny(line, countries) {
			continue
		}
		cells := strings.Split(line, "\t")
		cell := func(key string) string {
			if i, ok := col[key]; ok && i < len(cells) {
				return cells[i]
			}
			return ""
		}
		if at >= len(cells) || !containsAny(cells[at], countries) {
			continue
		}
		code := cell("code")
		if !BarcodePattern.MatchString(code) {
			continue
		}
		f, ok := product(code, cell, cell)
		if !ok {
			continue
		}
		if err := add(f); err != nil {
			return err
		}
	}
	return s.Err()
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// number reads a JSON value that is a number, or a number in a string.
func number(v any) (float64, bool) {
	switch v := v.(type) {
	case float64:
		return v, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f, err == nil
	}
	return 0, false
}
