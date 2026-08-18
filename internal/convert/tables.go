package convert

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// Table is the neutral in-memory shape every tabular format is read into and
// written out of. Header is always populated (the first row / the object keys);
// Rows are the data rows as strings.
type Table struct {
	Header []string
	Rows   [][]string
}

// ConvertTable converts a tabular file entirely in-process — no subprocess, so
// this category never reports a missing tool. For xlsx only the first sheet is
// read (the UI says so).
func ConvertTable(inPath, outPath, from, to string) error {
	t, err := readTable(inPath, from)
	if err != nil {
		return fmt.Errorf("read %s: %w", from, err)
	}
	if err := writeTable(outPath, t, to); err != nil {
		return fmt.Errorf("write %s: %w", to, err)
	}
	return nil
}

// ---- readers ---------------------------------------------------------------

func readTable(path, format string) (*Table, error) {
	switch format {
	case "csv":
		return readDelimited(path, ',')
	case "tsv":
		return readDelimited(path, '\t')
	case "json":
		return readJSON(path)
	case "xlsx":
		return readXLSX(path)
	}
	return nil, fmt.Errorf("unsupported table input %q", format)
}

func readDelimited(path string, comma rune) (*Table, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.Comma = comma
	r.FieldsPerRecord = -1 // tolerate ragged rows
	r.LazyQuotes = true
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	return fromRecords(records), nil
}

func fromRecords(records [][]string) *Table {
	t := &Table{}
	if len(records) == 0 {
		return t
	}
	t.Header = records[0]
	t.Rows = records[1:]
	normalize(t)
	return t
}

func readXLSX(path string) (*Table, error) {
	f, err := excelize.OpenFile(path, excelize.Options{
		// A crafted .xlsx is a zip that can expand far beyond its file size —
		// bound the decompressed total so a bomb fails instead of eating RAM.
		UnzipSizeLimit:    256 << 20,
		UnzipXMLSizeLimit: 64 << 20,
	})
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return &Table{}, nil
	}
	rows, err := f.GetRows(sheets[0])
	if err != nil {
		return nil, err
	}
	return fromRecords(rows), nil
}

func readJSON(path string) (*Table, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return &Table{}, nil
	}

	// Array form (objects or arrays) is the common case.
	if data[0] == '[' {
		var elems []json.RawMessage
		if err := json.Unmarshal(data, &elems); err != nil {
			return nil, err
		}
		return tableFromArray(elems)
	}
	// A single object → a one-row table.
	if data[0] == '{' {
		keys, vals, err := orderedObject(data)
		if err != nil {
			return nil, err
		}
		return &Table{Header: keys, Rows: [][]string{vals}}, nil
	}
	return nil, fmt.Errorf("JSON must be an array or object at the top level")
}

func tableFromArray(elems []json.RawMessage) (*Table, error) {
	t := &Table{}
	seen := map[string]int{} // key → column index, first-seen order
	var pending [][]struct{ k, v string }

	arrayMode := false
	for i, raw := range elems {
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 {
			continue
		}
		switch raw[0] {
		case '{':
			keys, vals, err := orderedObject(raw)
			if err != nil {
				return nil, err
			}
			row := make([]struct{ k, v string }, len(keys))
			for j, k := range keys {
				if _, ok := seen[k]; !ok {
					seen[k] = len(t.Header)
					t.Header = append(t.Header, k)
				}
				row[j] = struct{ k, v string }{k, vals[j]}
			}
			pending = append(pending, row)
		case '[':
			// Array-of-arrays: first inner array is the header.
			arrayMode = true
			var cells []any
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.UseNumber()
			if err := dec.Decode(&cells); err != nil {
				return nil, err
			}
			strs := make([]string, len(cells))
			for j, c := range cells {
				strs[j] = jsonCellString(c)
			}
			if i == 0 {
				t.Header = strs
			} else {
				t.Rows = append(t.Rows, strs)
			}
		default:
			// Scalar → single "value" column.
			if len(t.Header) == 0 {
				t.Header = []string{"value"}
			}
			var v any
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.UseNumber()
			if err := dec.Decode(&v); err != nil {
				return nil, err
			}
			t.Rows = append(t.Rows, []string{jsonCellString(v)})
		}
	}

	if !arrayMode && len(pending) > 0 {
		for _, row := range pending {
			cells := make([]string, len(t.Header))
			for _, kv := range row {
				cells[seen[kv.k]] = kv.v
			}
			t.Rows = append(t.Rows, cells)
		}
	}
	normalize(t)
	return t, nil
}

// orderedObject decodes a JSON object preserving key order (Go maps do not),
// so column order round-trips.
func orderedObject(raw []byte) (keys, vals []string, err error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return nil, nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, nil, fmt.Errorf("not a JSON object")
	}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		key, _ := kt.(string)
		var v any
		if err := dec.Decode(&v); err != nil {
			return nil, nil, err
		}
		keys = append(keys, key)
		vals = append(vals, jsonCellString(v))
	}
	return keys, vals, nil
}

// ---- writers ---------------------------------------------------------------

func writeTable(path string, t *Table, format string) error {
	switch format {
	case "csv":
		return writeDelimited(path, t, ',')
	case "tsv":
		return writeDelimited(path, t, '\t')
	case "json":
		return writeJSON(path, t)
	case "xlsx":
		return writeXLSX(path, t)
	case "md":
		return os.WriteFile(path, []byte(renderMarkdown(t)), 0o644)
	case "html":
		return os.WriteFile(path, []byte(renderHTML(t)), 0o644)
	}
	return fmt.Errorf("unsupported table output %q", format)
}

func writeDelimited(path string, t *Table, comma rune) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	w.Comma = comma
	if len(t.Header) > 0 {
		if err := w.Write(t.Header); err != nil {
			return err
		}
	}
	for _, row := range t.Rows {
		if err := w.Write(row); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

// writeJSON emits an array of objects with keys in header order. Go's json
// package sorts map keys, so the objects are assembled by hand to keep column
// order stable across a round trip.
func writeJSON(path string, t *Table) error {
	var b bytes.Buffer
	b.WriteString("[")
	for i, row := range t.Rows {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString("\n  {")
		for j, h := range t.Header {
			if j > 0 {
				b.WriteString(", ")
			}
			b.Write(jsonRaw(h))
			b.WriteString(": ")
			cell := ""
			if j < len(row) {
				cell = row[j]
			}
			b.Write(jsonRaw(jsonCellValue(cell)))
		}
		b.WriteByte('}')
	}
	if len(t.Rows) > 0 {
		b.WriteByte('\n')
	}
	b.WriteString("]\n")
	return os.WriteFile(path, b.Bytes(), 0o644)
}

// jsonRaw marshals a value without HTML escaping and without a trailing
// newline, so assembled JSON preserves cell bytes exactly.
func jsonRaw(v any) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return bytes.TrimRight(buf.Bytes(), "\n")
}

func writeXLSX(path string, t *Table) error {
	f := excelize.NewFile()
	defer f.Close()
	sheet := f.GetSheetName(0)
	rowIdx := 1
	if len(t.Header) > 0 {
		cell, _ := excelize.CoordinatesToCellName(1, rowIdx)
		if err := f.SetSheetRow(sheet, cell, ptrRow(t.Header)); err != nil {
			return err
		}
		rowIdx++
	}
	for _, row := range t.Rows {
		cell, _ := excelize.CoordinatesToCellName(1, rowIdx)
		if err := f.SetSheetRow(sheet, cell, ptrRow(row)); err != nil {
			return err
		}
		rowIdx++
	}
	return f.SaveAs(path)
}

func ptrRow(cells []string) *[]any {
	row := make([]any, len(cells))
	for i, c := range cells {
		row[i] = c
	}
	return &row
}

func renderMarkdown(t *Table) string {
	var b strings.Builder
	cols := len(t.Header)
	for _, r := range t.Rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	if cols == 0 {
		return ""
	}
	header := padCells(t.Header, cols)
	b.WriteString("| " + strings.Join(mapCells(header, mdCell), " | ") + " |\n")
	seps := make([]string, cols)
	for i := range seps {
		seps[i] = "---"
	}
	b.WriteString("| " + strings.Join(seps, " | ") + " |\n")
	for _, r := range t.Rows {
		b.WriteString("| " + strings.Join(mapCells(padCells(r, cols), mdCell), " | ") + " |\n")
	}
	return b.String()
}

func renderHTML(t *Table) string {
	var b strings.Builder
	b.WriteString("<table>\n")
	if len(t.Header) > 0 {
		b.WriteString("  <thead>\n    <tr>")
		for _, h := range t.Header {
			b.WriteString("<th>" + html.EscapeString(h) + "</th>")
		}
		b.WriteString("</tr>\n  </thead>\n")
	}
	b.WriteString("  <tbody>\n")
	for _, r := range t.Rows {
		b.WriteString("    <tr>")
		for _, c := range r {
			b.WriteString("<td>" + html.EscapeString(c) + "</td>")
		}
		b.WriteString("</tr>\n")
	}
	b.WriteString("  </tbody>\n</table>\n")
	return b.String()
}

// ---- helpers ---------------------------------------------------------------

// normalize pads every row to the header width so writers never index past a
// short row.
func normalize(t *Table) {
	cols := len(t.Header)
	for i := range t.Rows {
		if len(t.Rows[i]) < cols {
			t.Rows[i] = padCells(t.Rows[i], cols)
		}
	}
}

func padCells(cells []string, n int) []string {
	if len(cells) >= n {
		return cells
	}
	out := make([]string, n)
	copy(out, cells)
	return out
}

func mapCells(cells []string, fn func(string) string) []string {
	out := make([]string, len(cells))
	for i, c := range cells {
		out[i] = fn(c)
	}
	return out
}

func mdCell(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return s
}

// jsonCellValue types a string cell for JSON output when it round-trips
// exactly, so csv→json yields real numbers/bools without corrupting values
// like "01" or "1.50".
func jsonCellValue(s string) any {
	if s == "" {
		return ""
	}
	if i, err := strconv.ParseInt(s, 10, 64); err == nil && strconv.FormatInt(i, 10) == s {
		return i
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil && strconv.FormatFloat(f, 'f', -1, 64) == s {
		return f
	}
	switch s {
	case "true":
		return true
	case "false":
		return false
	}
	return s
}

// jsonCellString renders a decoded JSON value as a cell string. Numbers arrive
// as json.Number (decoder UseNumber) so precision is preserved.
func jsonCellString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case json.Number:
		return x.String()
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		b, _ := json.Marshal(x)
		return string(b)
	}
}
