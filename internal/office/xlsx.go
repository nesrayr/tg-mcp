package office

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxSheetRows = 2000
	maxSheetCols = 100
	// maxMergedCells limits merge expansion: otherwise a file with thousands of
	// huge merges would force iterating over billions of cells.
	maxMergedCells = 200_000
	// maxOutputBytes bounds the extracted text: every cell can reference the same huge
	// shared string, so a few kilobytes of XML would otherwise expand into gigabytes.
	maxOutputBytes      = 1 << 20
	outputTruncatedMark = "[output truncated]"
)

// XlsxText returns the contents of an Excel workbook: for each sheet, a header and rows
// like "5 | cell | cell |" (row number as in Excel). Merged cells
// are expanded: the value is repeated in every row of the merge, while adjacent cells
// of the same merge are printed once per row. Dates and times are formatted by cell style.
func XlsxText(data []byte) (string, error) {
	arc, err := openArchive(data)
	if err != nil {
		return "", err
	}

	sheets, err := readSheetList(arc)
	if err != nil {
		return "", err
	}
	shared, err := readSharedStrings(arc)
	if err != nil {
		return "", err
	}
	styles, err := readDateStyles(arc)
	if err != nil {
		return "", err
	}

	out := &limitedText{limit: maxOutputBytes}
	for _, sheet := range sheets {
		if out.full {
			break
		}
		raw, err := arc.mustRead(sheet.path)
		if err != nil {
			return "", err
		}
		grid, err := parseSheet(raw, shared, styles)
		if err != nil {
			return "", fmt.Errorf("sheet %q: %w", sheet.name, err)
		}
		fmt.Fprintf(out, "## Sheet: %s\n", sheet.name)
		grid.render(out)
		out.WriteString("\n")
	}

	text := strings.TrimSpace(out.String())
	if out.full {
		text += "\n" + outputTruncatedMark
	}
	return text, nil
}

// limitedText collects text up to limit bytes and silently drops the rest.
type limitedText struct {
	b     strings.Builder
	limit int
	full  bool
}

func (t *limitedText) Write(p []byte) (int, error) {
	t.WriteString(string(p))
	return len(p), nil
}

func (t *limitedText) WriteString(s string) {
	if t.full {
		return
	}
	if room := t.limit - t.b.Len(); len(s) > room {
		for room > 0 && !utf8.RuneStart(s[room]) {
			room--
		}
		s = s[:room]
		t.full = true
	}
	t.b.WriteString(s)
}

func (t *limitedText) String() string {
	return t.b.String()
}

type sheetRef struct {
	name string
	path string
}

func readSheetList(arc *archive) ([]sheetRef, error) {
	workbook, err := arc.mustRead("xl/workbook.xml")
	if err != nil {
		return nil, err
	}
	var wb struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
			RID  string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
		} `xml:"sheets>sheet"`
	}
	if err := xml.Unmarshal(workbook, &wb); err != nil {
		return nil, fmt.Errorf("%w: workbook.xml: %v", ErrInvalidDocument, err)
	}

	rels, err := arc.mustRead("xl/_rels/workbook.xml.rels")
	if err != nil {
		return nil, err
	}
	var relationships struct {
		Items []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if err := xml.Unmarshal(rels, &relationships); err != nil {
		return nil, fmt.Errorf("%w: workbook.xml.rels: %v", ErrInvalidDocument, err)
	}
	targets := make(map[string]string, len(relationships.Items))
	for _, rel := range relationships.Items {
		targets[rel.ID] = rel.Target
	}

	sheets := make([]sheetRef, 0, len(wb.Sheets))
	for _, sheet := range wb.Sheets {
		target, ok := targets[sheet.RID]
		if !ok {
			continue
		}
		sheets = append(sheets, sheetRef{name: sheet.Name, path: resolveTarget("xl", target)})
	}
	return sheets, nil
}

func readSharedStrings(arc *archive) ([]string, error) {
	data, err := arc.read("xl/sharedStrings.xml")
	if err != nil || data == nil {
		return nil, err
	}
	var sst struct {
		Items []richText `xml:"si"`
	}
	if err := xml.Unmarshal(data, &sst); err != nil {
		return nil, fmt.Errorf("%w: sharedStrings.xml: %v", ErrInvalidDocument, err)
	}
	strs := make([]string, len(sst.Items))
	for i, item := range sst.Items {
		strs[i] = item.String()
	}
	return strs, nil
}

// richText is an Excel string: either a plain <t> or a set of <r><t> runs.
// Phonetic hints (<rPh>) are not included.
type richText struct {
	Text string `xml:"t"`
	Runs []struct {
		Text string `xml:"t"`
	} `xml:"r"`
}

func (r richText) String() string {
	if len(r.Runs) == 0 {
		return r.Text
	}
	var b strings.Builder
	b.WriteString(r.Text)
	for _, run := range r.Runs {
		b.WriteString(run.Text)
	}
	return b.String()
}

type dateKind int

const (
	notDate dateKind = iota
	dateOnly
	timeOnly
	dateTime
)

// readDateStyles reports for each cell style index whether it is a date/time.
func readDateStyles(arc *archive) ([]dateKind, error) {
	data, err := arc.read("xl/styles.xml")
	if err != nil || data == nil {
		return nil, err
	}
	var ss struct {
		NumFmts []struct {
			ID   int    `xml:"numFmtId,attr"`
			Code string `xml:"formatCode,attr"`
		} `xml:"numFmts>numFmt"`
		CellXfs []struct {
			NumFmtID int `xml:"numFmtId,attr"`
		} `xml:"cellXfs>xf"`
	}
	if err := xml.Unmarshal(data, &ss); err != nil {
		return nil, fmt.Errorf("%w: styles.xml: %v", ErrInvalidDocument, err)
	}

	custom := make(map[int]string, len(ss.NumFmts))
	for _, f := range ss.NumFmts {
		custom[f.ID] = f.Code
	}
	kinds := make([]dateKind, len(ss.CellXfs))
	for i, xf := range ss.CellXfs {
		kinds[i] = numFmtKind(xf.NumFmtID, custom)
	}
	return kinds, nil
}

func numFmtKind(id int, custom map[int]string) dateKind {
	switch {
	case id >= 14 && id <= 17, id == 30, id == 31, id >= 57 && id <= 58:
		return dateOnly
	case id >= 18 && id <= 21, id >= 45 && id <= 47:
		return timeOnly
	case id == 22:
		return dateTime
	}
	code, ok := custom[id]
	if !ok {
		return notDate
	}
	return formatCodeKind(code)
}

// formatCodeKind parses a custom number format: quoted literals, escaped
// characters and [...] sections (colors, conditions) are ignored.
func formatCodeKind(code string) dateKind {
	var tokens strings.Builder
	inQuote, inBracket := false, false
	for i := 0; i < len(code); i++ {
		c := code[i]
		switch {
		case inQuote:
			inQuote = c != '"'
		case inBracket:
			inBracket = c != ']'
		case c == '"':
			inQuote = true
		case c == '[':
			// [h]:mm is a duration, still a time.
			if strings.HasPrefix(strings.ToLower(code[i:]), "[h") {
				tokens.WriteByte('h')
			}
			inBracket = true
		case c == '\\':
			i++
		default:
			tokens.WriteByte(c | 0x20)
		}
	}

	t := tokens.String()
	hasDate := strings.ContainsAny(t, "dy")
	hasTime := strings.ContainsAny(t, "hs")
	switch {
	case hasDate && hasTime:
		return dateTime
	case hasDate:
		return dateOnly
	case hasTime:
		return timeOnly
	case strings.Contains(t, "m"):
		// "mmm" without d/y/h is a month name.
		return dateOnly
	default:
		return notDate
	}
}

var excelEpoch = time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)

func formatSerial(value float64, kind dateKind) string {
	days := math.Floor(value)
	seconds := math.Round((value - days) * 86400)
	t := excelEpoch.AddDate(0, 0, int(days)).Add(time.Duration(seconds) * time.Second)
	switch kind {
	case dateOnly:
		return t.Format("2006-01-02")
	case timeOnly:
		if days >= 1 {
			return fmt.Sprintf("%d:%02d", int(days)*24+t.Hour(), t.Minute())
		}
		return t.Format("15:04")
	default:
		return t.Format("2006-01-02 15:04")
	}
}

type cellPos struct{ row, col int }

type mergeRange struct{ top, left, bottom, right int }

type grid struct {
	cells  map[cellPos]string
	merges map[cellPos]int // cell → merge index
	maxRow int
	maxCol int
	capped bool
}

func parseSheet(data []byte, shared []string, styles []dateKind) (*grid, error) {
	var ws struct {
		Rows []struct {
			Cells []struct {
				Ref    string   `xml:"r,attr"`
				Type   string   `xml:"t,attr"`
				Style  int      `xml:"s,attr"`
				Value  string   `xml:"v"`
				Inline richText `xml:"is"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
		Merges []struct {
			Ref string `xml:"ref,attr"`
		} `xml:"mergeCells>mergeCell"`
	}
	if err := xml.NewDecoder(bytes.NewReader(data)).Decode(&ws); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidDocument, err)
	}

	g := &grid{cells: make(map[cellPos]string), merges: make(map[cellPos]int)}
	for _, row := range ws.Rows {
		for _, c := range row.Cells {
			pos, ok := parseRef(c.Ref)
			if !ok {
				continue
			}
			if pos.row > maxSheetRows || pos.col > maxSheetCols {
				g.capped = true
				continue
			}
			value := cellValue(c.Type, c.Value, c.Inline, c.Style, shared, styles)
			if value == "" {
				continue
			}
			g.set(pos, value)
		}
	}

	budget := maxMergedCells
	for i, m := range ws.Merges {
		r, ok := parseRange(m.Ref)
		if !ok {
			continue
		}
		value := g.cells[cellPos{r.top, r.left}]
		for row := r.top; row <= min(r.bottom, maxSheetRows); row++ {
			for col := r.left; col <= min(r.right, maxSheetCols); col++ {
				if budget == 0 {
					g.capped = true
					return g, nil
				}
				budget--
				g.merges[cellPos{row, col}] = i
				if value != "" {
					g.set(cellPos{row, col}, value)
				}
			}
		}
	}
	return g, nil
}

func (g *grid) set(pos cellPos, value string) {
	g.cells[pos] = value
	g.maxRow = max(g.maxRow, pos.row)
	g.maxCol = max(g.maxCol, pos.col)
}

func cellValue(typ, raw string, inline richText, style int, shared []string, styles []dateKind) string {
	switch typ {
	case "s":
		index, err := strconv.Atoi(raw)
		if err != nil || index < 0 || index >= len(shared) {
			return ""
		}
		return strings.TrimSpace(shared[index])
	case "inlineStr":
		return strings.TrimSpace(inline.String())
	case "str", "e":
		return strings.TrimSpace(raw)
	case "b":
		if raw == "1" {
			return "TRUE"
		}
		return "FALSE"
	}

	if raw == "" {
		return ""
	}
	number, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return raw
	}
	if style >= 0 && style < len(styles) && styles[style] != notDate {
		return formatSerial(number, styles[style])
	}
	return strconv.FormatFloat(number, 'f', -1, 64)
}

func (g *grid) render(out *limitedText) {
	rows := make([]int, 0, len(g.cells))
	seen := make(map[int]struct{})
	for pos := range g.cells {
		if _, ok := seen[pos.row]; !ok {
			seen[pos.row] = struct{}{}
			rows = append(rows, pos.row)
		}
	}
	slices.Sort(rows)

	for _, row := range rows {
		if out.full {
			return
		}
		values := make([]string, 0, g.maxCol)
		last := 0
		prevMerge := -1
		for col := 1; col <= g.maxCol; col++ {
			merge, merged := g.merges[cellPos{row, col}]
			if merged && merge == prevMerge {
				continue
			}
			prevMerge = -1
			if merged {
				prevMerge = merge
			}
			value := g.cells[cellPos{row, col}]
			values = append(values, value)
			if value != "" {
				last = len(values)
			}
		}

		// Cells are written one by one instead of joined: a single row can hold
		// a hundred references to a multi-megabyte string.
		fmt.Fprintf(out, "%d | ", row)
		for i, value := range values[:last] {
			if i > 0 {
				out.WriteString(" | ")
			}
			writeCell(out, value)
		}
		out.WriteString(" |\n")
	}
	if g.capped {
		fmt.Fprintf(out, "[sheet truncated: showing the first %d rows and %d columns]\n", maxSheetRows, maxSheetCols)
	}
}

// writeCell writes a cell value on one line, replacing line breaks with " / ".
func writeCell(out *limitedText, value string) {
	for !out.full {
		line, rest, found := strings.Cut(value, "\n")
		out.WriteString(line)
		if !found {
			return
		}
		out.WriteString(" / ")
		value = rest
	}
}

// parseRef parses a cell reference like "AB12".
func parseRef(ref string) (cellPos, bool) {
	ref = strings.ReplaceAll(ref, "$", "")
	i := 0
	col := 0
	for i < len(ref) && ref[i] >= 'A' && ref[i] <= 'Z' {
		col = col*26 + int(ref[i]-'A'+1)
		i++
	}
	row, err := strconv.Atoi(ref[i:])
	if i == 0 || err != nil || row < 1 || col > 16384 {
		return cellPos{}, false
	}
	return cellPos{row: row, col: col}, true
}

func parseRange(ref string) (mergeRange, bool) {
	from, to, ok := strings.Cut(ref, ":")
	if !ok {
		return mergeRange{}, false
	}
	a, okA := parseRef(from)
	b, okB := parseRef(to)
	if !okA || !okB {
		return mergeRange{}, false
	}
	return mergeRange{top: min(a.row, b.row), left: min(a.col, b.col), bottom: max(a.row, b.row), right: max(a.col, b.col)}, true
}
