// Package food reads the food tables the selfhostit apps share, and helps
// search them:
//
//   - Open Food Facts products: the monthly export, read for a few countries,
//     and live barcode lookups (also in Open Products Facts and Open Beauty
//     Facts, for things that aren't food)
//   - USDA FoodData Central (SR Legacy and Foundation Foods), bundled, in the
//     public domain
//   - NEVO (RIVM, Netherlands), read from the download the owner uploads
//
// It holds no database code: each app stores the foods its own way, and ranks
// search results by what matters to it.
package food

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Food is one food from a table, with its values per 100 g.
type Food struct {
	SourceID string `json:"id"`
	Name     string `json:"name"`
	NameNL   string `json:"name_nl,omitempty"`
	NameFR   string `json:"name_fr,omitempty"`
	Aliases  string `json:"aliases,omitempty"`
	Group    string `json:"group,omitempty"`
	Brand    string `json:"brand,omitempty"`
	Barcode  string `json:"barcode,omitempty"`

	// Products only: the package size ("1 L", "6 x 33 cl"), Open Food Facts'
	// category tags joined by commas, and the URL of a small photo of the
	// front, about 200 pixels high.
	Quantity   string `json:"quantity,omitempty"`
	Categories string `json:"categories,omitempty"`
	ImageURL   string `json:"image_url,omitempty"`

	// Nutrition is false for a product that gives no energy value, in which
	// case the values below are all zero.
	Nutrition bool    `json:"-"`
	Kcal      float64 `json:"kcal"`
	Protein   float64 `json:"protein"`
	Carbs     float64 `json:"carbs"`
	Fat       float64 `json:"fat"`
	Fiber     float64 `json:"fiber"`
	Sugar     float64 `json:"sugar"`
	Salt      float64 `json:"salt"`

	ServingG     float64 `json:"serving_g,omitempty"`
	ServingLabel string  `json:"serving_label,omitempty"`
}

// Plausible leaves out foods whose values can't be right, which happens in a
// crowd-sourced database: more energy than pure fat, or more than 100 g of
// macronutrients in 100 g.
func Plausible(f Food) bool {
	return f.Kcal <= 900 && f.Protein+f.Carbs+f.Fat <= 105
}

var fold = transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)

// Normalize lowercases s, drops accents and turns everything but letters and
// digits into single spaces, so "Pâté, de campagne" becomes "pate de campagne".
func Normalize(s string) string {
	folded, _, err := transform.String(fold, s)
	if err != nil {
		folded = s
	}
	folded = strings.NewReplacer("ß", "ss", "æ", "ae", "œ", "oe", "Æ", "ae", "Œ", "oe").Replace(folded)
	var b strings.Builder
	space := true
	for _, r := range strings.ToLower(folded) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			space = false
		} else if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}
