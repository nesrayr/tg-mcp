package mcpserver

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/nesrayr/tg-mcp/internal/config"
)

// MediaStore keeps downloaded attachments that the model reads from disk (PDFs and large
// images). File names are built from IDs rather than the sender's file name, so that
// a foreign name cannot point outside the directory.
type MediaStore struct {
	dir string
	ttl time.Duration
	now func() time.Time
}

func NewMediaStore(dir string, ttl time.Duration, now func() time.Time) *MediaStore {
	return &MediaStore{dir: dir, ttl: ttl, now: now}
}

// Save writes the file with mode 0600 and returns its path.
func (m *MediaStore) Save(chatID int64, messageID int, mime string, data []byte) (string, error) {
	if err := config.EnsurePrivateDir(m.dir); err != nil {
		return "", err
	}
	if err := m.Cleanup(); err != nil {
		return "", err
	}

	path := filepath.Join(m.dir, fmt.Sprintf("%d_%d%s", chatID, messageID, extension(mime)))
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

func extension(mime string) string {
	switch mime {
	case "application/pdf":
		return ".pdf"
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
