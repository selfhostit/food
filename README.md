# food

**The food tables the [selfhostit](https://github.com/selfhostit) apps share.**

A small Go module that reads the food and product tables used by
[Troye](https://github.com/selfhostit/troye) (a calorie tracker) and
[Shoppa](https://github.com/selfhostit/shoppa) (a shopping list), and helps search
them. It has no database code: each app stores the foods its own way and ranks search
results by what matters to it.

- **Open Food Facts:** reads the monthly export for a few countries (Belgium and the
  Netherlands for the apps), with names in English, Dutch and French, brand, package
  size, categories, a small photo and the nutrients when the product has them. Looks
  barcodes up live in Open Food Facts, Open Products Facts and Open Beauty Facts.
- **USDA FoodData Central:** about 8,000 generic foods, bundled in `data/`.
- **NEVO:** reads the RIVM download (CSV, Excel or the zip), which you download yourself.
- **Search helpers:** accent-insensitive normalizing ("Pâté" finds "pate"), plural
  stemming in English, Dutch and French, and name scoring.

```go
import "github.com/selfhostit/food"

c := food.Client{UserAgent: "Shoppa/1.0 (self-hosted shopping list)"}
p, err := c.Product(ctx, food.OpenFoodFacts, "5410000000001")

body, err := c.Export(ctx, food.OFFExportURL)
defer body.Close()
err = food.ReadExport(body, food.BelgiumNetherlands, func(p food.Food) error { … })
```

Open Food Facts asks every app to send a User-Agent that names it; set yours in
`Client`.

## Credits

Show these where the data is used:

- Open Food Facts data: `food.OFFAttribution` (Open Database Licence); product
  photos: `food.OFFPhotoAttribution` (CC BY-SA 3.0)
- USDA: `food.USDAAttribution` (public domain)
- NEVO: `food.NEVOAttribution`. NEVO is free to use but can't be redistributed, so
  it is never bundled.

To rebuild the bundled USDA table from the FoodData Central CSV downloads:

```sh
go run ./cmd/usda FoodData_Central_sr_legacy_food_csv_2018-04.zip FoodData_Central_foundation_food_csv_2025-04-24.zip
```

## Development

`gofmt`, `go vet ./...` and `go test ./...` must pass before every commit. A change
here reaches an app when the app bumps the version in its `go.mod`; tag releases
`v0.x.y`.

## License

MIT. The bundled USDA data is in the public domain.
