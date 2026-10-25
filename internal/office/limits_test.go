//go:build !race

package office

import (
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	floodBytes    = 8 << 20
	maxHeapGrowth = 48 << 20
)

func sheetXML(rows string) string {
	return `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>` +
		rows + `</sheetData></worksheet>`
}

func peakHeapGrowth(t *testing.T, fn func()) uint64 {
	t.Helper()

	defer debug.SetGCPercent(debug.SetGCPercent(10))
	runtime.GC()

	read := func() uint64 {
		sample := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}
		metrics.Read(sample)
		return sample[0].Value.Uint64()
	}
	base := read()

	var peak atomic.Uint64
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			peak.Store(max(peak.Load(), read()))
			select {
			case <-done:
				return
			case <-ticker.C:
			}
		}
	}()

	fn()
	close(done)
	<-stopped
	peak.Store(max(peak.Load(), read()))
	return peak.Load() - min(base, peak.Load())
}

func Test_MemoryBound(t *testing.T) {
	tests := []struct {
		name    string
		data    func(t *testing.T) []byte
		extract func([]byte) (string, error)
		wantErr error
	}{
		{
			name: "sheet cells",
			data: func(t *testing.T) []byte {
				return xlsx(t, sheetXML(`<row r="1">`+strings.Repeat("<c/>", floodBytes/4)+`</row>`), nil)
			},
			extract: XlsxText,
		},
		{
			name: "inline string runs",
			data: func(t *testing.T) []byte {
				cell := `<c r="A1" t="inlineStr"><is>` + strings.Repeat("<r/>", floodBytes/4) + `</is></c>`
				return xlsx(t, sheetXML(`<row r="1">`+cell+`</row>`), nil)
			},
			extract: XlsxText,
		},
		{
			name: "sheet list",
			data: func(t *testing.T) []byte {
				return xlsx(t, sheetXML(""), map[string]string{
					"xl/workbook.xml": `<workbook><sheets>` + strings.Repeat("<sheet/>", floodBytes/8) + `</sheets></workbook>`,
				})
			},
			extract: XlsxText,
		},
		{
			name: "cell styles",
			data: func(t *testing.T) []byte {
				return xlsx(t, sheetXML(""), map[string]string{
					"xl/styles.xml": `<styleSheet><cellXfs>` + strings.Repeat("<xf/>", 2*floodBytes/5) + `</cellXfs></styleSheet>`,
				})
			},
			extract: XlsxText,
		},
		{
			name: "shared strings",
			data: func(t *testing.T) []byte {
				return xlsx(t, sheetXML(""), map[string]string{
					"xl/sharedStrings.xml": `<sst>` + strings.Repeat("<si/>", floodBytes/5) + `</sst>`,
				})
			},
			extract: XlsxText,
			wantErr: ErrTooLarge,
		},
		{
			name: "docx cells outside rows",
			data: func(t *testing.T) []byte {
				return docx(t, strings.Repeat("<w:tc/>", floodBytes/7))
			},
			extract: DocxText,
		},
		{
			name: "docx deep nesting",
			data: func(t *testing.T) []byte {
				return docx(t, strings.Repeat("<w:tc>", floodBytes/13)+strings.Repeat("</w:tc>", floodBytes/13))
			},
			extract: DocxText,
			wantErr: ErrInvalidDocument,
		},
		{
			name: "tag with many attributes",
			data: func(t *testing.T) []byte {
				var attrs strings.Builder
				for i := 0; attrs.Len() < floodBytes; i++ {
					attrs.WriteString(" a" + strconv.Itoa(i) + `=""`)
				}
				return docx(t, `<w:p`+attrs.String()+`/>`)
			},
			extract: DocxText,
			wantErr: ErrInvalidDocument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := tt.data(t)

			var err error
			growth := peakHeapGrowth(t, func() {
				_, err = tt.extract(data)
			})

			require.Less(t, growth, uint64(maxHeapGrowth))
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}
