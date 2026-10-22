// Package office extracts text from Office Open XML documents (DOCX, XLSX).
//
// Files from chats are untrusted: macros and external links are ignored, XML is parsed
// with the standard library (it does not load external entities), and the decompressed size
// is limited so that a zip bomb cannot exhaust memory.
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
)

var (
	ErrInvalidDocument = errors.New("invalid office document")
	ErrTooLarge        = errors.New("office document is too large when unpacked")
)

type archive struct {
	files map[string]*zip.File
	limit int64
}

func openArchive(data []byte) (*archive, error) {
	return openArchiveLimit(data, maxUncompressedPart)
}

func openArchiveLimit(data []byte, limit int64) (*archive, error) {
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
	return &archive{files: files, limit: limit}, nil
}

// read returns the contents of an archive part, or nil if the part is missing.
func (a *archive) read(name string) ([]byte, error) {
	file, ok := a.files[name]
	if !ok {
		return nil, nil
	}
	if file.UncompressedSize64 > uint64(a.limit) {
		return nil, fmt.Errorf("%w: %s", ErrTooLarge, name)
	}

	rc, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("%w: open %s: %v", ErrInvalidDocument, name, err)
	}
	defer rc.Close()

	// The zip header can be forged, so the limit is also checked while reading.
	data, err := io.ReadAll(io.LimitReader(rc, a.limit+1))
	if err != nil {
		return nil, fmt.Errorf("%w: read %s: %v", ErrInvalidDocument, name, err)
	}
	if int64(len(data)) > a.limit {
		return nil, fmt.Errorf("%w: %s", ErrTooLarge, name)
	}
	return data, nil
}

func (a *archive) mustRead(name string) ([]byte, error) {
	data, err := a.read(name)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, fmt.Errorf("%w: missing %s", ErrInvalidDocument, name)
	}
	return data, nil
}

// resolveTarget converts a path from .rels to a path inside the archive.
func resolveTarget(baseDir, target string) string {
	if strings.HasPrefix(target, "/") {
		return strings.TrimPrefix(target, "/")
	}
	return path.Clean(path.Join(baseDir, target))
}
