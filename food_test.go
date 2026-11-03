package food

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"Pâté, de campagne":    "pate de campagne",
		"Crème brûlée":         "creme brulee",
		"Kaas Goudse 48+ jong": "kaas goudse 48 jong",
		"  Œuf  dur ":          "oeuf dur",
		"Vol-au-vent (kip)":    "vol au vent kip",
		"Würstchen; Straße":    "wurstchen strasse",
	} {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReadUSDA(t *testing.T) {
	tbl, err := ReadUSDA("testdata/usda.zip")
	if err != nil {
		t.Fatal(err)
	}
	// branded foods and foods without energy are left out
	if tbl.Version != "2018-04" || len(tbl.Foods) != 3 {
		t.Fatalf("version %q, %d foods", tbl.Version, len(tbl.Foods))
	}
	rice := tbl.Foods[0]
	if rice.SourceID != "168878" || rice.Name != "Rice, white, long-grain, regular, enriched, cooked" || rice.Group != "Cereal Grains and Pasta" ||
		rice.Kcal != 130 || rice.Protein != 2.69 || rice.Carbs != 28.17 || rice.Salt != 0.003 || rice.ServingG != 158 || rice.ServingLabel != "1 cup" {
		t.Errorf("rice %+v", rice)
	}
	// salt from sodium, and a portion with its amount
	chicken := tbl.Foods[1]
	if chicken.Salt != 0.185 || chicken.ServingG != 86 || chicken.ServingLabel != "0.5 breast, bone and skin removed" {
		t.Errorf("chicken %+v", chicken)
	}
	// energy from the specific Atwater factors when there is no plain kcal
	apple := tbl.Foods[2]
	if apple.Kcal != 58 || apple.Fiber != 2.4 || apple.Sugar != 10.39 || apple.ServingLabel != `1 medium (3" dia)` {
		t.Errorf("apple %+v", apple)
	}
	// the same zip twice adds nothing
	if twice, err := ReadUSDA("testdata/usda.zip", "testdata/usda.zip"); err != nil || len(twice.Foods) != 3 {
		t.Errorf("twice: %d foods, %v", len(twice.Foods), err)
	}
}

func TestBundledUSDA(t *testing.T) {
	tbl, err := BundledUSDA()
	if err != nil {
		t.Fatal(err)
	}
	if tbl.Version == "" || len(tbl.Foods) == 0 {
		t.Fatalf("version %q, %d foods", tbl.Version, len(tbl.Foods))
	}
	for _, f := range tbl.Foods {
		if f.SourceID == "" || f.Name == "" || f.Kcal < 0 {
			t.Fatalf("incomplete food %+v", f)
		}
	}
}

func TestReadNEVO(t *testing.T) {
	csv := "Voedingsmiddelgroep|Food group|NEVO-code|Voedingsmiddelnaam/Dutch food name|Engelse naam/Food name|Synoniem|Hoeveelheid/Quantity|ENERCJ|ENERCC|PROT|CHO|SUGAR|FAT|FIBT|NA\n" +
		"Kaas|Cheese|449|Kaas Goudse 48+ jong|Cheese Gouda 48+ young|Jonge kaas|per 100g|1479|356|23,5|0|0|29|0|820\n" +
		"Groenten|Vegetables|1|Witlof rauw|Chicory raw|Chicon/Brussels lof|per 100g|71|17|1,2|2,3|2,3|0,2|1,2|3\n" +
		"Overig|Other|2|Zonder energie|No energy||per 100g||||||||\n"
	xlsx, err := os.ReadFile("testdata/NEVO2025_v9.0.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	// the real download quotes the headers and adds units to the codes
	withUnits := strings.NewReplacer("ENERCJ|", `"ENERCJ (kJ)"|`, "ENERCC|", `"ENERCC (kcal)"|`, "PROT|", `"PROT (g)"|`,
		"FAT|", `"FAT (g)"|`, "|NA\n", `|"NA (mg)"`+"\n").Replace(csv)
	// and its zip has more tables than the foods, some before it
	var download bytes.Buffer
	zw := zip.NewWriter(&download)
	for _, f := range []struct{ name, data string }{
		{"NEVO2025_v9.0_Details.csv", "NEVO-code|Voedingsmiddelnaam/Dutch food name|ENERCC (kcal)\n1|Iets|1\n"},
		{"NEVO2025_v9.0.csv", withUnits},
	} {
		w, err := zw.Create(f.name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(f.data))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	for _, file := range []struct {
		name string
		data []byte
	}{
		{"NEVO2025_v9.0.csv", []byte(csv)},
		{"NEVO2025_v9.0.csv", []byte(withUnits)},
		{"NEVO2025_v9.0.zip", download.Bytes()},
		{"NEVO2025_v9.0.csv", []byte(strings.ReplaceAll(strings.ReplaceAll(csv, ",", "."), "|", ";"))},
		{"NEVO2025_v9.0.xlsx", xlsx},
	} {
		tbl, err := ReadNEVO(file.name, file.data)
		if err != nil {
			t.Fatalf("%s: %v", file.name, err)
		}
		if tbl.Version != "2025/9.0" || len(tbl.Foods) != 2 {
			t.Fatalf("%s: version %q, %d foods", file.name, tbl.Version, len(tbl.Foods))
		}
		cheese, chicory := tbl.Foods[0], tbl.Foods[1]
		if cheese.SourceID != "449" || cheese.Name != "Cheese Gouda 48+ young" || cheese.NameNL != "Kaas Goudse 48+ jong" ||
			cheese.Aliases != "Jonge kaas" || cheese.Group != "Cheese" || cheese.Kcal != 356 || cheese.Protein != 23.5 || cheese.Fat != 29 {
			t.Errorf("%s: cheese %+v", file.name, cheese)
		}
		// sodium in mg becomes salt in g
		if cheese.Salt < 2.049 || cheese.Salt > 2.051 {
			t.Errorf("%s: salt %v", file.name, cheese.Salt)
		}
		if chicory.Aliases != "Chicon; Brussels lof" {
			t.Errorf("%s: aliases %q", file.name, chicory.Aliases)
		}
	}

	if _, err := ReadNEVO("other.csv", []byte("name,kcal\nApple,52\n")); err == nil || !strings.Contains(err.Error(), "NEVO table") {
		t.Errorf("another CSV: %v", err)
	}
}

func TestReadExport(t *testing.T) {
	export := strings.Join([]string{
		"code\tproduct_name\tgeneric_name\tbrands\tquantity\tcategories_tags\tcountries_tags\timage_url\timage_small_url\tenergy-kcal_100g\tenergy_100g\tproteins_100g\tcarbohydrates_100g\tfat_100g\tsugars_100g\tsalt_100g\tsodium_100g\tserving_quantity\tserving_size",
		"5410000000001\tSpeculoos\t\tLotus,Lotus Bakeries\t250 g\ten:biscuits,en:speculoos\ten:belgium,en:france\thttps://images.openfoodfacts.org/images/products/541/000/000/0001/front_nl.4.400.jpg\thttps://images.openfoodfacts.org/images/products/541/000/000/0001/front_nl.4.200.jpg\t484\t\t4.9\t72.6\t19\t38\t0.9\t\t6.25\t1 biscuit",
		"8710000000001\t\tKarnemelk\tAH\t1 L\t\ten:netherlands\t\t\t\t159\t3.5\t4\t0.5\t\t\t0.04\t\t",
		"3000000000001\tCroissant\t\t\t\t\ten:france\t\t\t400\t\t8\t45\t21\t\t1\t\t\t",
		"5410000000002\tWC-papier\t\tBoni\t8 rollen\t\ten:belgium\t\t\t\t\t\t\t\t\t\t\t\t",
		"5410000000003\tImpossible\t\t\t\t\ten:belgium\t\t\t2000\t\t\t\t\t\t\t\t\t",
		"5410000000004\t\t\t\t\t\ten:belgium\t\t\t100\t\t\t\t\t\t\t\t\t",
		"short\tline",
	}, "\n")
	var got []Food
	err := ReadExport(strings.NewReader(export), BelgiumNetherlands, func(f Food) error {
		got = append(got, f)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// not the French croissant, nor the product without a name
	if len(got) != 4 {
		t.Fatalf("kept %+v", got)
	}
	lotus, milk, paper, impossible := got[0], got[1], got[2], got[3]
	if lotus.Barcode != "5410000000001" || lotus.Name != "Speculoos" || lotus.Brand != "Lotus" || lotus.Quantity != "250 g" ||
		lotus.Categories != "en:biscuits,en:speculoos" || !strings.HasSuffix(lotus.ImageURL, "front_nl.4.200.jpg") ||
		!lotus.Nutrition || lotus.Kcal != 484 || lotus.ServingG != 6.25 {
		t.Errorf("speculoos %+v", lotus)
	}
	// the generic name stands in, kJ become kcal and sodium becomes salt
	if milk.Name != "Karnemelk" || milk.Kcal < 37.9 || milk.Kcal > 38.1 || milk.Salt != 0.1 || milk.ImageURL != "" {
		t.Errorf("karnemelk %+v", milk)
	}
	// a product without nutrition is kept, for apps that don't need it
	if paper.Name != "WC-papier" || paper.Nutrition || paper.Kcal != 0 || paper.Quantity != "8 rollen" {
		t.Errorf("wc-papier %+v", paper)
	}
	if !Plausible(lotus) || Plausible(impossible) {
		t.Error("plausible")
	}

	if err := ReadExport(strings.NewReader("code\tproduct_name\n1\tx\n"), BelgiumNetherlands, nil); err == nil ||
		!strings.Contains(err.Error(), "countries_tags") {
		t.Errorf("missing column: %v", err)
	}
}

func TestProduct(t *testing.T) {
	var agent, asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent, asked = r.UserAgent(), r.URL.Path
		switch r.URL.Path {
		case "/api/v2/product/5410000000001.json":
			w.Write([]byte(`{"status":1,"code":"5410000000001","product":{
				"product_name":"Halfvolle melk","product_name_nl":"Halfvolle melk","product_name_fr":"Lait demi-écrémé","brands":"Boni,Colruyt",
				"quantity":"1 L","categories_tags":["en:dairies","en:milks","en:semi-skimmed-milks"],
				"image_front_small_url":"https://images.openfoodfacts.org/images/products/541/front_nl.3.200.jpg",
				"image_small_url":"https://images.openfoodfacts.org/images/products/541/ingredients_nl.5.200.jpg",
				"nutriments":{"energy-kcal_100g":46,"proteins_100g":"3.4","fat_100g":1.5}}}`))
		case "/api/v2/product/5410000000002.json":
			w.Write([]byte(`{"status":1,"product":{"product_name":"Afwasmiddel","quantity":"500 ml"}}`))
		case "/api/v2/product/5410000000003.json":
			w.Write([]byte(`{"status":1,"product":{"brands":"Nameless"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"status":0}`))
		}
	}))
	defer srv.Close()
	c := Client{UserAgent: "Test/1.0 (testing)"}
	ctx := context.Background()

	milk, err := c.Product(ctx, srv.URL, "5410000000001")
	if err != nil {
		t.Fatal(err)
	}
	if agent != "Test/1.0 (testing)" {
		t.Errorf("user agent %q", agent)
	}
	// Dutch stands in for English, and the front photo comes before others
	if milk.Name != "Halfvolle melk" || milk.NameFR != "Lait demi-écrémé" || milk.Brand != "Boni" || milk.Quantity != "1 L" ||
		milk.Categories != "en:dairies,en:milks,en:semi-skimmed-milks" || !strings.HasSuffix(milk.ImageURL, "front_nl.3.200.jpg") ||
		!milk.Nutrition || milk.Kcal != 46 || milk.Protein != 3.4 {
		t.Errorf("milk %+v", milk)
	}
	soap, err := c.Product(ctx, srv.URL+"/", "5410000000002")
	if err != nil || soap.Name != "Afwasmiddel" || soap.Barcode != "5410000000002" || soap.Nutrition {
		t.Errorf("soap %+v, %v", soap, err)
	}
	for _, code := range []string{"5410000000003", "5410000000009"} {
		if _, err := c.Product(ctx, srv.URL, code); !errors.Is(err, ErrUnknownProduct) {
			t.Errorf("%s: %v (%s)", code, err, asked)
		}
	}
}

func TestExport(t *testing.T) {
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	zw.Write([]byte("code\tproduct_name\tcountries_tags\n5410000000001\tSpeculoos\ten:belgium\n"))
	zw.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.UserAgent() != "Test/1.0" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Write(gz.Bytes())
	}))
	defer srv.Close()
	body, err := Client{UserAgent: "Test/1.0"}.Export(context.Background(), srv.URL+"/products.csv.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	n := 0
	if err := ReadExport(body, BelgiumNetherlands, func(Food) error { n++; return nil }); err != nil || n != 1 {
		t.Errorf("%d products, %v", n, err)
	}
	if _, err := (Client{}).Export(context.Background(), srv.URL+"/products.csv.gz"); err == nil {
		t.Error("a failed download")
	}
}

func TestScore(t *testing.T) {
	// whole words beat the start of a word, which beats a word inside another
	words := Words("Melk")
	if !(FieldScore("melk halfvolle", words) > FieldScore("melkchocolade", words) &&
		FieldScore("melkchocolade", words) > FieldScore("chocomelk", words)) {
		t.Error("melk ranks wrong")
	}
	if got := Stemmed([]string{"appels", "kip"}); len(got) != 2 || got[0] != "appel" || got[1] != "kip" {
		t.Errorf("stemmed %q", got)
	}
	if Stemmed([]string{"kip", "rijst"}) != nil {
		t.Error("stemmed words without plurals")
	}
	if !CoversAll("rice", []string{"white", "rice"}) || CoversAll("rice flour", []string{"white", "rice"}) {
		t.Error("covers all")
	}
	if !HasOtherWord("apples dried", []string{"dried"}, []string{"apples"}) ||
		HasOtherWord("apples dried", []string{"dried"}, []string{"apples", "dried"}) {
		t.Error("has other word")
	}
	if len(Words("a b c d e f g h")) != 6 {
		t.Error("words")
	}
}

func TestUSDARenamed(t *testing.T) {
	tbl, err := BundledUSDA()
	if err != nil {
		t.Fatal(err)
	}
	renamed := USDARenamed(tbl)
	if len(renamed) == 0 {
		t.Fatal("nothing renamed")
	}
	for _, f := range renamed {
		if !f.Nutrition || !strings.Contains(f.Aliases, ",") {
			t.Errorf("%+v", f)
		}
	}
}
