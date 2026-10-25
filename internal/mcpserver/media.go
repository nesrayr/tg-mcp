package mcpserver

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/nesrayr/tg-mcp/internal/config"
	"github.com/nesrayr/tg-mcp/internal/domain"
)

// MediaStore keeps downloaded attachments that the model reads from disk.
type MediaStore struct {
	dir string
	ttl time.Duration
	now func() time.Time
}

func NewMediaStore(dir string, ttl time.Duration, now func() time.Time) *MediaStore {
	return &MediaStore{
		dir: dir,
		ttl: ttl,
		now: now,
	}
}

// Save writes the file with mode 0600 and returns its path.
func (m *MediaStore) Save(chatID int64, messageID int, ext string, data []byte) (string, error) {
	if err := config.EnsurePrivateDir(m.dir); err != nil {
		return "", err
	}
	if err := m.Cleanup(); err != nil {
		return "", err
	}

	// The name is built from IDs, not the sender's file name, so it cannot point outside the directory.
	path := filepath.Join(m.dir, fmt.Sprintf("%d_%d%s", chatID, messageID, ext))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("save media: %w", err)
	}
	// WriteFile does not change the mode of an existing file.
	if err := os.Chmod(path, 0o600); err != nil {
		return "", fmt.Errorf("chmod media: %w", err)
	}
	return path, nil
}

// Cleanup removes files older than ttl.
func (m *MediaStore) Cleanup() error {
	entries, err := os.ReadDir(m.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read media dir: %w", err)
	}

	deadline := m.now().Add(-m.ttl)
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(deadline) {
			if err := os.Remove(filepath.Join(m.dir, entry.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("remove old media: %w", err)
			}
		}
	}
	return nil
}

func fileExtension(media domain.Media) string {
	// PDFs sometimes arrive as application/octet-stream, and the file reading tool relies on the extension.
	if media.Kind == domain.MediaKindPDF {
		return ".pdf"
	}
	switch media.MimeType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".bin"
	}
}
