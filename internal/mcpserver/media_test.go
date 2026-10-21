package mcpserver

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func Test_MediaStore_Cleanup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewMediaStore(dir, 7*24*time.Hour, func() time.Time { return testTime })

	oldFile := filepath.Join(dir, "1_1.pdf")
	freshFile := filepath.Join(dir, "1_2.pdf")
	require.NoError(t, os.WriteFile(oldFile, []byte("old"), 0o600))
	require.NoError(t, os.WriteFile(freshFile, []byte("fresh"), 0o600))
	require.NoError(t, os.Chtimes(oldFile, testTime.Add(-8*24*time.Hour), testTime.Add(-8*24*time.Hour)))
	require.NoError(t, os.Chtimes(freshFile, testTime.Add(-time.Hour), testTime.Add(-time.Hour)))

	require.NoError(t, store.Cleanup())

	require.NoFileExists(t, oldFile)
	require.FileExists(t, freshFile)
}

func Test_MediaStore_Cleanup_MissingDir(t *testing.T) {
	t.Parallel()

	store := NewMediaStore(filepath.Join(t.TempDir(), "missing"), time.Hour, time.Now)

	require.NoError(t, store.Cleanup())
}

func Test_MediaStore_Save_OverwritesWithPrivateMode(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "media")
	store := NewMediaStore(dir, time.Hour, func() time.Time { return testTime })
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, "5_6.pdf")
	require.NoError(t, os.WriteFile(path, []byte("stale"), 0o644))

	got, err := store.Save(5, 6, "application/pdf", []byte("new"))
	require.NoError(t, err)
	require.Equal(t, path, got)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, []byte("new"), data)

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}
