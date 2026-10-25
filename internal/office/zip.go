// Package office extracts text from Office Open XML documents (DOCX, XLSX).
//
// Files from chats are untrusted: macros and external links are ignored, XML is parsed
// with the standard library (it does not load external entities), and the decompressed size,
// XML nesting, tag size and element counts are limited so that a small file cannot exhaust memory.
package office

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

const (
	maxEntries          = 10_000
	maxUncompressedPart = 50 << 20
	// maxUncompressedTotal bounds all parts read from one archive: a workbook can list
	// thousands of sheets that point to the same large part.
	maxUncompressedTotal = 200 << 20
)

var (
	ErrInvalidDocument = errors.New("invalid office document")
	ErrTooLarge        = errors.New("office document is too large when unpacked")
)

type archive struct {
	files     map[string]*zip.File
	limit     int64
	remaining int64
}

func openArchive(data []byte) (*archive, error) {
	return openArchiveLimit(data, maxUncompressedPart, maxUncompressedTotal)
}

func openArchiveLimit(data []byte, partLimit, totalLimit int64) (*archive, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidDocument, err)
	}
	if len(reader.File) > maxEntries {
		return nil, fmt.Errorf("%w: %d entries", ErrTooLarge, len(reader.File))
	}

	files := make(map[string]*zip.File, len(reader.File))
	for _, file := range reader.File {
		files[strings.TrimPrefix(file.Name, "/")] = file
	}
	return &archive{
		files:     files,
		limit:     partLimit,
		remaining: totalLimit,
	}, nil
}

func (a *archive) read(name string) ([]byte, error) {
	file, ok := a.files[name]
	if !ok {
		return nil, nil
	}
	limit := min(a.limit, a.remaining)
	if file.UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("%w: %q", ErrTooLarge, name)
	}

	rc, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("%w: open %q: %v", ErrInvalidDocument, name, err)
	}
	defer rc.Close()

	// The zip header can be forged, so the limit is also checked while reading.
	data, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%w: read %q: %v", ErrInvalidDocument, name, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: %q", ErrTooLarge, name)
	}
	a.remaining -= int64(len(data))
	return data, nil
}

func (a *archive) mustRead(name string) ([]byte, error) {
	data, err := a.read(name)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, fmt.Errorf("%w: missing %q", ErrInvalidDocument, name)
	}
	return data, nil
}

func resolveTarget(baseDir, target string) string {
	if strings.HasPrefix(target, "/") {
		return strings.TrimPrefix(target, "/")
	}
	return path.Clean(path.Join(baseDir, target))
}
