package office

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

const wordNS = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"`

func buildZip(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := writer.Create(name)
		require.NoError(t, err)
		_, err = w.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	return buf.Bytes()
}

func docx(t *testing.T, body string) []byte {
	t.Helper()

	return buildZip(t, map[string]string{
		"word/document.xml": `<?xml version="1.0"?><w:document ` + wordNS + `><w:body>` + body + `</w:body></w:document>`,
	})
}

func Test_DocxText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "paragraphs and runs",
			body: `<w:p><w:r><w:t>Schedule</w:t></w:r><w:r><w:t xml:space="preserve"> for the week</w:t></w:r></w:p><w:p><w:r><w:t>Mon</w:t></w:r></w:p>`,
			want: "Schedule for the week\nMon",
		},
		{
			name: "tab and break inside run, tab stops ignored",
			body: `<w:p><w:pPr><w:tabs><w:tab w:val="left" w:pos="720"/></w:tabs></w:pPr><w:r><w:t>16:30</w:t><w:tab/><w:t>Smith</w:t><w:br/><w:t>online</w:t></w:r></w:p>`,
			want: "16:30\tSmith\nonline",
		},
		{
			name: "deleted text and field codes are skipped",
			body: `<w:p><w:r><w:t>Room </w:t></w:r><w:del><w:r><w:delText>B-324</w:delText></w:r></w:del><w:r><w:instrText>HYPERLINK</w:instrText><w:t>B-326</w:t></w:r></w:p>`,
			want: "Room B-326",
		},
		{
			name: "table rows with multi-paragraph cells",
			body: `<w:p><w:r><w:t>Before</w:t></w:r></w:p>` +
				`<w:tbl><w:tr><w:tc><w:p><w:r><w:t>Day</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>Class</w:t></w:r></w:p></w:tc></w:tr>` +
				`<w:tr><w:tc><w:p><w:r><w:t>Tue</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>Smith</w:t></w:r></w:p><w:p><w:r><w:t>16:30</w:t></w:r></w:p></w:tc></w:tr></w:tbl>` +
				`<w:p><w:r><w:t>After</w:t></w:r></w:p>`,
			want: "Before\n| Day | Class |\n| Tue | Smith / 16:30 |\n\nAfter",
		},
		{
			name: "nested table stays inside outer cell",
			body: `<w:tbl><w:tr><w:tc><w:p><w:r><w:t>A</w:t></w:r></w:p>` +
				`<w:tbl><w:tr><w:tc><w:p><w:r><w:t>x</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>y</w:t></w:r></w:p></w:tc></w:tr></w:tbl>` +
				`</w:tc><w:tc><w:p><w:r><w:t>B</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`,
			want: "| A / | x | y | | B |",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := DocxText(docx(t, tt.body))
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func Test_DocxText_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data func(t *testing.T) []byte
	}{
		{name: "not a zip", data: func(*testing.T) []byte { return []byte("plain text") }},
		{name: "no document part", data: func(t *testing.T) []byte { return buildZip(t, map[string]string{"word/other.xml": "<x/>"}) }},
		{name: "broken xml", data: func(t *testing.T) []byte {
			return buildZip(t, map[string]string{"word/document.xml": "<w:document><w:body>"})
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := DocxText(tt.data(t))
			require.ErrorIs(t, err, ErrInvalidDocument)
		})
	}
}

func Test_archive_UnpackLimit(t *testing.T) {
	t.Parallel()

	data := buildZip(t, map[string]string{"word/document.xml": strings.Repeat("a", 100)})

	arc, err := openArchiveLimit(data, 10, 1000)
	require.NoError(t, err)

	_, err = arc.read("word/document.xml")
	require.ErrorIs(t, err, ErrTooLarge)
}

func Test_archive_TotalUnpackLimit(t *testing.T) {
	t.Parallel()

	data := buildZip(t, map[string]string{"xl/worksheets/sheet1.xml": strings.Repeat("a", 60)})

	arc, err := openArchiveLimit(data, 100, 100)
	require.NoError(t, err)

	_, err = arc.read("xl/worksheets/sheet1.xml")
	require.NoError(t, err)
	_, err = arc.read("xl/worksheets/sheet1.xml")
	require.ErrorIs(t, err, ErrTooLarge)
}

func xlsx(t *testing.T, sheet string, extra map[string]string) []byte {
	t.Helper()

	files := map[string]string{
		"xl/workbook.xml": `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
			`<sheets><sheet name="Fall" sheetId="1" r:id="rId1"/><sheet name="Empty" sheetId="2" r:id="rId2"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Target="/xl/worksheets/sheet2.xml"/></Relationships>`,
		"xl/sharedStrings.xml": `<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
			`<si><t>Day</t></si><si><t>Class</t></si><si><r><t>Smi</t></r><r><t>th</t></r><rPh><t>skip</t></rPh></si><si><t>Mon</t></si></sst>`,
		"xl/styles.xml": `<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
			`<numFmts><numFmt numFmtId="164" formatCode="h:mm"/><numFmt numFmtId="165" formatCode="&quot;day&quot; 0"/></numFmts>` +
			`<cellXfs><xf numFmtId="0"/><xf numFmtId="14"/><xf numFmtId="164"/><xf numFmtId="165"/></cellXfs></styleSheet>`,
		"xl/worksheets/sheet1.xml": sheet,
		"xl/worksheets/sheet2.xml": `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData/></worksheet>`,
	}
	for name, content := range extra {
		files[name] = content
	}
	return buildZip(t, files)
}

func Test_XlsxText(t *testing.T) {
	t.Parallel()

	sheet := `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>` +
		`<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c><c r="C1" t="inlineStr"><is><t>Time</t></is></c></row>` +
		`<row r="2"><c r="A2" t="s"><v>3</v></c><c r="B2" t="s"><v>2</v></c><c r="C2" s="2"><v>0.6875</v></c><c r="E2" s="1"><v>46316</v></c></row>` +
		`<row r="3"><c r="C3" s="2"><v>0.76041666666666663</v></c><c r="D3" t="b"><v>1</v></c><c r="E3" s="3"><v>5</v></c></row>` +
		`<row r="4"><c r="A4" t="str"><v>Total</v></c><c r="B4"><v>7</v></c><c r="D4"><v>2.5</v></c></row>` +
		`<row r="6"><c r="A6" t="s"><v>99</v></c></row>` +
		`</sheetData><mergeCells><mergeCell ref="A2:A3"/><mergeCell ref="B2:B3"/><mergeCell ref="A4:C4"/></mergeCells></worksheet>`

	got, err := XlsxText(xlsx(t, sheet, nil))
	require.NoError(t, err)
	require.Equal(t, strings.Join([]string{
		"## Sheet: Fall",
		"1 | Day | Class | Time |",
		"2 | Mon | Smith | 16:30 |  | 2026-10-21 |",
		"3 | Mon | Smith | 18:15 | TRUE | 5 |",
		"4 | Total | 2.5 |",
		"",
		"## Sheet: Empty",
	}, "\n"), got)
}

func Test_XlsxText_Errors(t *testing.T) {
	t.Parallel()

	t.Run("missing workbook", func(t *testing.T) {
		t.Parallel()

		_, err := XlsxText(buildZip(t, map[string]string{"xl/styles.xml": "<styleSheet/>"}))
		require.ErrorIs(t, err, ErrInvalidDocument)
	})

	t.Run("broken sheet", func(t *testing.T) {
		t.Parallel()

		_, err := XlsxText(xlsx(t, "<worksheet><sheetData>", nil))
		require.ErrorIs(t, err, ErrInvalidDocument)
	})
}

func Test_XlsxText_Limits(t *testing.T) {
	t.Parallel()

	sheet := `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>` +
		`<row r="1"><c r="A1" t="inlineStr"><is><t>ok</t></is></c><c r="ZZ1" t="inlineStr"><is><t>far</t></is></c></row>` +
		`</sheetData><mergeCells><mergeCell ref="A1:XFD1048576"/></mergeCells></worksheet>`

	got, err := XlsxText(xlsx(t, sheet, nil))
	require.NoError(t, err)
	require.NotContains(t, got, "far")
	require.Contains(t, got, "[sheet truncated")
}

func Test_XlsxText_OutputLimit(t *testing.T) {
	t.Parallel()

	// Every cell references one large shared string: a tiny file must not expand
	// into hundreds of megabytes of text.
	var cells strings.Builder
	for row := 1; row <= 20; row++ {
		fmt.Fprintf(&cells, `<row r="%d">`, row)
		for col := 'A'; col <= 'T'; col++ {
			fmt.Fprintf(&cells, `<c r="%c%d" t="s"><v>0</v></c>`, col, row)
		}
		cells.WriteString(`</row>`)
	}
	sheet := `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>` +
		cells.String() + `</sheetData></worksheet>`
	shared := `<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><si><t>` +
		strings.Repeat("é\n", 1<<15) + `</t></si></sst>`

	got, err := XlsxText(xlsx(t, sheet, map[string]string{"xl/sharedStrings.xml": shared}))
	require.NoError(t, err)
	require.LessOrEqual(t, len(got), maxOutputBytes+len(outputTruncatedMark)+1)
	require.True(t, strings.HasSuffix(got, outputTruncatedMark))
	require.True(t, utf8.ValidString(got))
}

func Test_formatCodeKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		code string
		want dateKind
	}{
		{code: "dd.mm.yyyy", want: dateOnly},
		{code: "h:mm", want: timeOnly},
		{code: "[h]:mm:ss", want: timeOnly},
		{code: "dd/mm/yy hh:mm", want: dateTime},
		{code: "mmm", want: dateOnly},
		{code: "0.00", want: notDate},
		{code: `"day" 0`, want: notDate},
		{code: `[Red]0.00`, want: notDate},
		{code: `\d0`, want: notDate},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, formatCodeKind(tt.code))
		})
	}
}

func Test_formatSerial(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value float64
		kind  dateKind
		want  string
	}{
		{name: "date", value: 46316, kind: dateOnly, want: "2026-10-21"},
		{name: "time", value: 0.4861111111, kind: timeOnly, want: "11:40"},
		{name: "duration over a day", value: 1.5, kind: timeOnly, want: "36:00"},
		{name: "date and time", value: 46316.6875, kind: dateTime, want: "2026-10-21 16:30"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, formatSerial(tt.value, tt.kind))
		})
	}
}

func Test_parseRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		ref    string
		want   cellPos
		wantOK bool
	}{
		{ref: "A1", want: cellPos{row: 1, col: 1}, wantOK: true},
		{ref: "$AB$12", want: cellPos{row: 12, col: 28}, wantOK: true},
		{ref: "12", wantOK: false},
		{ref: "A0", wantOK: false},
		{ref: "A", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			t.Parallel()

			got, ok := parseRef(tt.ref)
			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.want, got)
		})
	}
}
