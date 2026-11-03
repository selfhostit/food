package food

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"io"
	"strconv"
	"strings"
)

// Just enough of the Excel format to read a table from the first sheet of an
// .xlsx file: shared strings, inline strings and numbers.

func isXLSX(zr *zip.Reader) bool {
	for _, f := range zr.File {
		if f.Name == "xl/workbook.xml" {
			return true
		}
	}
	return false
}

func xlsxRows(zr *zip.Reader) ([][]string, error) {
	var shared []string
	var sheet *zip.File
	for _, f := range zr.File {
		switch {
		case f.Name == "xl/sharedStrings.xml":
			var sst struct {
				Items []struct {
					T    string `xml:"t"`
					Runs []struct {
						T string `xml:"t"`
					} `xml:"r"`
				} `xml:"si"`
			}
			if err := decodeZipXML(f, &sst); err != nil {
				return nil, err
			}
			for _, si := range sst.Items {
				s := si.T
				for _, r := range si.Runs {
					s += r.T
				}
				shared = append(shared, s)
			}
		case f.Name == "xl/worksheets/sheet1.xml":
			sheet = f
		}
	}
	if sheet == nil {
		return nil, errors.New("the Excel file has no first sheet")
	}

	var ws struct {
		Rows []struct {
			Cells []struct {
				Ref    string `xml:"r,attr"`
				Type   string `xml:"t,attr"`
				Value  string `xml:"v"`
				Inline string `xml:"is>t"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
	}
	if err := decodeZipXML(sheet, &ws); err != nil {
		return nil, err
	}
	rows := make([][]string, 0, len(ws.Rows))
	for _, r := range ws.Rows {
		var row []string
		for i, c := range r.Cells {
			col := columnIndex(c.Ref)
			if col < 0 {
				col = i
			}
			for len(row) <= col {
				row = append(row, "")
			}
			switch c.Type {
			case "s":
				if n, err := strconv.Atoi(c.Value); err == nil && n >= 0 && n < len(shared) {
					row[col] = shared[n]
				}
			case "inlineStr":
				row[col] = c.Inline
			default:
				row[col] = c.Value
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// columnIndex turns a cell reference like "AB12" into its column, 27.
func columnIndex(ref string) int {
	n := 0
	letters := strings.TrimRight(ref, "0123456789")
	if letters == "" {
		return -1
	}
	for _, r := range strings.ToUpper(letters) {
		if r < 'A' || r > 'Z' {
			return -1
		}
		n = n*26 + int(r-'A'+1)
	}
	return n - 1
}

func decodeZipXML(f *zip.File, v any) error {
	r, err := f.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	return xml.NewDecoder(io.LimitReader(r, 200<<20)).Decode(v)
}
