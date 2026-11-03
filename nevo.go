package food

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// NEVOAttribution is the citation RIVM asks for wherever NEVO data is used.
const NEVOAttribution = "NEVO-online version 2025/9.0, RIVM, Bilthoven"

var nevoVersion = regexp.MustCompile(`(?i)nevo[\s_-]*(\d{4})(?:[\s_-]*v?(\d+(?:[._]\d+)?))?`)

// ReadNEVO reads the NEVO table from a CSV (any common delimiter), an Excel
// file, or a zip with either.
func ReadNEVO(filename string, data []byte) (Table, error) {
	var t Table
	if m := nevoVersion.FindStringSubmatch(path.Base(filename)); m != nil {
		t.Version = m[1]
		if m[2] != "" {
			t.Version += "/" + strings.ReplaceAll(m[2], "_", ".")
		}
	}
	rows, err := nevoRows(filename, data)
	if err == nil {
		t.Foods, err = nevoFoods(rows)
	}
	if err != nil {
		return t, FormatError{"That doesn't look like the NEVO table: " + err.Error() + "."}
	}
	return t, nil
}

// FormatError means a file isn't the table it should be.
type FormatError struct{ msg string }

func (e FormatError) Error() string { return e.msg }

func nevoRows(filename string, data []byte) ([][]string, error) {
	if bytes.HasPrefix(data, []byte("PK")) {
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, err
		}
		if isXLSX(zr) {
			return xlsxRows(zr)
		}
		// a zip with the download: use its CSV or Excel file
		for _, f := range zr.File {
			ext := strings.ToLower(path.Ext(f.Name))
			if isNEVOTable(f.Name) && (ext == ".csv" || ext == ".txt" || ext == ".xlsx") {
				r, err := f.Open()
				if err != nil {
					return nil, err
				}
				inner, err := io.ReadAll(r)
				r.Close()
				if err != nil {
					return nil, err
				}
				return nevoRows(f.Name, inner)
			}
		}
		return nil, errors.New("the zip has no NEVO CSV or Excel file")
	}
	return csvRows(data)
}

// isNEVOTable tells the foods file in the download from the other tables next
// to it (NEVO2025_v9.0_Details.csv, _Recepten_Recipes.csv and so on).
func isNEVOTable(name string) bool {
	name = strings.ToLower(path.Base(name))
	if !strings.Contains(name, "nevo") {
		return false
	}
	for _, other := range []string{"details", "nutrient", "recept", "recipe", "referen"} {
		if strings.Contains(name, other) {
			return false
		}
	}
	return true
}

// csvRows reads CSV with whichever of | ; tab or , its header line uses.
func csvRows(data []byte) ([][]string, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	header, _, _ := bytes.Cut(data, []byte("\n"))
	delim, most := ',', 0
	for _, d := range []rune{'|', ';', '\t', ','} {
		if n := bytes.Count(header, []byte(string(d))); n > most {
			delim, most = d, n
		}
	}
	r := csv.NewReader(bufio.NewReader(bytes.NewReader(data)))
	r.Comma = delim
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	return r.ReadAll()
}

// nevoFoods finds the columns by their headers, in Dutch or English, and the
// nutrients by their INFOODS codes.
func nevoFoods(rows [][]string) ([]Food, error) {
	if len(rows) < 2 {
		return nil, errors.New("it has no foods")
	}
	col := map[string]int{}
	for i, h := range rows[0] {
		h = strings.TrimSpace(h)
		l := strings.ToLower(h)
		set := func(key string) {
			if _, ok := col[key]; !ok {
				col[key] = i
			}
		}
		switch {
		case strings.Contains(l, "nevo-code") || strings.Contains(l, "nevo code") || strings.Contains(l, "nevo_code"):
			set("code")
		case strings.Contains(l, "voedingsmiddelnaam") || strings.Contains(l, "dutch food name"):
			set("nl")
		case strings.Contains(l, "engelse naam") || l == "food name" || strings.HasSuffix(l, "/food name"):
			set("en")
		case strings.Contains(l, "synoniem") || strings.Contains(l, "synonym"):
			set("synonyms")
		case l == "food group" || strings.HasSuffix(l, "/food group"):
			set("group")
		case strings.Contains(l, "hoeveelheid") || strings.Contains(l, "quantity"):
			set("quantity")
		default:
			// the download adds the unit to the code: "ENERCC (kcal)"
			code, _, _ := strings.Cut(h, " (")
			set(strings.ToUpper(strings.TrimSpace(code)))
		}
	}
	for _, need := range []string{"code", "nl", "ENERCC"} {
		if _, ok := col[need]; !ok {
			if need == "ENERCC" {
				if _, ok := col["ENERCJ"]; ok {
					continue
				}
			}
			return nil, fmt.Errorf("it has no %s column", need)
		}
	}

	cell := func(row []string, key string) string {
		i, ok := col[key]
		if !ok || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}
	num := func(row []string, key string) (float64, bool) {
		s := strings.ReplaceAll(cell(row, key), ",", ".")
		v, err := strconv.ParseFloat(s, 64)
		return v, err == nil
	}

	var foods []Food
	for _, row := range rows[1:] {
		code := cell(row, "code")
		nl := cell(row, "nl")
		if code == "" || nl == "" {
			continue
		}
		kcal, ok := num(row, "ENERCC")
		if !ok {
			kj, ok2 := num(row, "ENERCJ")
			if !ok2 {
				continue
			}
			kcal = kj / 4.184
		}
		f := Food{SourceID: code, Name: cell(row, "en"), NameNL: nl, Group: cell(row, "group"), Nutrition: true, Kcal: kcal}
		if f.Name == "" {
			f.Name = nl
		}
		f.Aliases = strings.Join(strings.FieldsFunc(cell(row, "synonyms"), func(r rune) bool { return r == ';' || r == '/' }), "; ")
		f.Protein, _ = num(row, "PROT")
		f.Carbs, _ = num(row, "CHO")
		f.Fat, _ = num(row, "FAT")
		f.Fiber, _ = num(row, "FIBT")
		f.Sugar, _ = num(row, "SUGAR")
		// NEVO gives sodium in mg; salt is 2.5 times the sodium
		if na, ok := num(row, "NA"); ok {
			f.Salt = na * 2.5 / 1000
		}
		foods = append(foods, f)
	}
	if len(foods) == 0 {
		return nil, errors.New("it has no foods with an energy value")
	}
	return foods, nil
}
