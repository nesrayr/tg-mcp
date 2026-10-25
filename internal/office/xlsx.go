package office

import (
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
	// The limits below bound memory: every listed element stays in memory until the
	// sheet is rendered, and XML lets millions of them fit into a small file.
	maxSheets        = 1000
	maxSharedStrings = 1_000_000
	maxCellStyles    = 65_536
	maxSheetCells    = maxSheetRows * maxSheetCols
	maxMergeRanges   = 10_000
)

const relationshipsNS = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"

// XlsxText returns the sheets of an Excel workbook as "5 | cell | cell |" rows.
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
		grid, err := parseSheet(raw, sheet.path, shared, styles)
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

type workbookSheet struct {
	name string
	rid  string
}

func readSheetList(arc *archive) ([]sheetRef, error) {
	workbook, err := arc.mustRead("xl/workbook.xml")
	if err != nil {
		return nil, err
	}
	var listed []workbookSheet
	err = walk(workbook, "xl/workbook.xml", func(d *xml.Decoder, start xml.StartElement, parent string) (bool, error) {
		if parent != "sheets" || start.Name.Local != "sheet" {
			return false, nil
		}
		if len(listed) < maxSheets {
			listed = append(listed, workbookSheet{
				name: attr(start, "", "name"),
				rid:  attr(start, relationshipsNS, "id"),
			})
		}
		return true, d.Skip()
	})
	if err != nil {
		return nil, err
	}

	rels, err := arc.mustRead("xl/_rels/workbook.xml.rels")
	if err != nil {
		return nil, err
	}
	targets := make(map[string]string)
	err = walk(rels, "xl/_rels/workbook.xml.rels", func(d *xml.Decoder, start xml.StartElement, parent string) (bool, error) {
		if parent != "Relationships" || start.Name.Local != "Relationship" {
			return false, nil
		}
		if len(targets) < maxEntries {
			targets[attr(start, "", "Id")] = attr(start, "", "Target")
		}
		return true, d.Skip()
	})
	if err != nil {
		return nil, err
	}

	sheets := make([]sheetRef, 0, len(listed))
	for _, sheet := range listed {
		target, ok := targets[sheet.rid]
		if !ok {
			continue
		}
		sheets = append(sheets, sheetRef{
			name: sheet.name,
			path: resolveTarget("xl", target),
		})
	}
	return sheets, nil
}

func readSharedStrings(arc *archive) ([]string, error) {
	data, err := arc.read("xl/sharedStrings.xml")
	if err != nil || data == nil {
		return nil, err
	}
	var strs []string
	err = walk(data, "xl/sharedStrings.xml", func(d *xml.Decoder, start xml.StartElement, parent string) (bool, error) {
		if parent != "sst" || start.Name.Local != "si" {
			return false, nil
		}
		if len(strs) == maxSharedStrings {
			return true, fmt.Errorf("%w: more than %d shared strings", ErrTooLarge, maxSharedStrings)
		}
		text, readErr := readRichText(d)
		strs = append(strs, text)
		return true, readErr
	})
	if err != nil {
		return nil, err
	}
	return strs, nil
}

type dateKind int

const (
	notDate dateKind = iota
	dateOnly
	timeOnly
	dateTime
)

func readDateStyles(arc *archive) ([]dateKind, error) {
	data, err := arc.read("xl/styles.xml")
	if err != nil || data == nil {
		return nil, err
	}
	custom := make(map[int]string)
	var formats []int
	err = walk(data, "xl/styles.xml", func(d *xml.Decoder, start xml.StartElement, parent string) (bool, error) {
		switch {
		case parent == "numFmts" && start.Name.Local == "numFmt":
			id, convErr := strconv.Atoi(attr(start, "", "numFmtId"))
			if convErr == nil && len(custom) < maxCellStyles {
				custom[id] = attr(start, "", "formatCode")
			}
		case parent == "cellXfs" && start.Name.Local == "xf":
			if len(formats) < maxCellStyles {
				id, _ := strconv.Atoi(attr(start, "", "numFmtId"))
				formats = append(formats, id)
			}
		default:
			return false, nil
		}
		return true, d.Skip()
	})
	if err != nil {
		return nil, err
	}

	kinds := make([]dateKind, len(formats))
	for i, id := range formats {
		kinds[i] = numFmtKind(id, custom)
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

func formatCodeKind(code string) dateKind {
	var tokens strings.Builder
	inQuote, inBracket := false, false
	// Quoted literals, escaped characters and [...] sections (colors, conditions) carry no date tokens.
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

type cellPos struct {
	row int
	col int
}

type mergeRange struct {
	top    int
	left   int
	bottom int
	right  int
}

type grid struct {
	cells  map[cellPos]string
	merges map[cellPos]int // cell → merge index
	maxRow int
	maxCol int
	capped bool
}

type rawCell struct {
	typ    string
	style  int
	value  string
	inline string
}

func parseSheet(data []byte, part string, shared []string, styles []dateKind) (*grid, error) {
	g := &grid{
		cells:  make(map[cellPos]string),
		merges: make(map[cellPos]int),
	}
	var ranges []mergeRange
	decoded := 0
	err := walk(data, part, func(d *xml.Decoder, start xml.StartElement, parent string) (bool, error) {
		switch {
		case parent == "sheetData" && start.Name.Local == "row":
			row, err := strconv.Atoi(attr(start, "", "r"))
			if err == nil && row > maxSheetRows {
				g.capped = true
				return true, d.Skip()
			}
			return false, nil
		case parent == "row" && start.Name.Local == "c":
			pos, ok := parseRef(attr(start, "", "r"))
			if !ok {
				return true, d.Skip()
			}
			if pos.row > maxSheetRows || pos.col > maxSheetCols || decoded == maxSheetCells {
				g.capped = true
				return true, d.Skip()
			}
			decoded++
			c, err := readCell(d, start)
			if err != nil {
				return true, err
			}
			if value := cellValue(c, shared, styles); value != "" {
				g.set(pos, value)
			}
			return true, nil
		case parent == "mergeCells" && start.Name.Local == "mergeCell":
			r, ok := parseRange(attr(start, "", "ref"))
			switch {
			case !ok, r.top > maxSheetRows, r.left > maxSheetCols:
			case len(ranges) == maxMergeRanges:
				g.capped = true
			default:
				ranges = append(ranges, r)
			}
			return true, d.Skip()
		default:
			return false, nil
		}
	})
	if err != nil {
		return nil, err
	}
	g.applyMerges(ranges)
	return g, nil
}

func readCell(d *xml.Decoder, start xml.StartElement) (rawCell, error) {
	c := rawCell{typ: attr(start, "", "t")}
	c.style, _ = strconv.Atoi(attr(start, "", "s"))
	for {
		token, err := d.Token()
		if err != nil {
			return c, err
		}
		switch t := token.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "v":
				c.value, err = readValue(d)
			case "is":
				c.inline, err = readRichText(d)
			default:
				err = d.Skip()
			}
		case xml.EndElement:
			return c, nil
		}
		if err != nil {
			return c, err
		}
	}
}

func (g *grid) applyMerges(ranges []mergeRange) {
	budget := maxMergedCells
	for i, r := range ranges {
		value := g.cells[cellPos{r.top, r.left}]
		for row := r.top; row <= min(r.bottom, maxSheetRows); row++ {
			for col := r.left; col <= min(r.right, maxSheetCols); col++ {
				if budget == 0 {
					g.capped = true
					return
				}
				budget--
				g.merges[cellPos{row, col}] = i
				if value != "" {
					g.set(cellPos{row, col}, value)
				}
			}
		}
	}
}

func (g *grid) set(pos cellPos, value string) {
	g.cells[pos] = value
	g.maxRow = max(g.maxRow, pos.row)
	g.maxCol = max(g.maxCol, pos.col)
}

func cellValue(c rawCell, shared []string, styles []dateKind) string {
	raw, style := c.value, c.style
	switch c.typ {
	case "s":
		index, err := strconv.Atoi(raw)
		if err != nil || index < 0 || index >= len(shared) {
			return ""
		}
		return strings.TrimSpace(shared[index])
	case "inlineStr":
		return strings.TrimSpace(c.inline)
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
	return cellPos{
		row: row,
		col: col,
	}, true
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
	return mergeRange{
		top:    min(a.row, b.row),
		left:   min(a.col, b.col),
		bottom: max(a.row, b.row),
		right:  max(a.col, b.col),
	}, true
}
