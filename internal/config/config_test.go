package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nesrayr/tg-mcp/internal/domain"
)

const testAPIHash = "0123456789abcdef0123456789abcdef"

func testPaths(t *testing.T) Paths {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "tg-mcp")
	return Paths{
		ConfigDir:   dir,
		ConfigFile:  filepath.Join(dir, "config.json"),
		SessionFile: filepath.Join(dir, "session.json"),
		MediaDir:    filepath.Join(t.TempDir(), "media"),
	}
}

func Test_SaveLoad(t *testing.T) {
	t.Parallel()

	t.Run("roundtrip with private permissions", func(t *testing.T) {
		t.Parallel()

		paths := testPaths(t)
		cfg := Config{
			APIID:        12345,
			APIHash:      testAPIHash,
			AllowedChats: []string{"@durov", "-1001234567890"},
		}

		require.NoError(t, Save(paths, cfg))

		got, err := Load(paths)
		require.NoError(t, err)
		require.Equal(t, cfg, got)

		dirInfo, err := os.Stat(paths.ConfigDir)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm())

		fileInfo, err := os.Stat(paths.ConfigFile)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), fileInfo.Mode().Perm())
	})

	t.Run("save fixes existing dir permissions", func(t *testing.T) {
		t.Parallel()

		paths := testPaths(t)
		require.NoError(t, os.MkdirAll(paths.ConfigDir, 0o755))

		require.NoError(t, Save(paths, Config{
			APIID:   1,
			APIHash: testAPIHash,
		}))

		dirInfo, err := os.Stat(paths.ConfigDir)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm())
	})

	t.Run("save rejects invalid config", func(t *testing.T) {
		t.Parallel()

		paths := testPaths(t)

		err := Save(paths, Config{
			APIID:   1,
			APIHash: "short",
		})
		require.ErrorIs(t, err, ErrInvalidConfig)
		require.NoFileExists(t, paths.ConfigFile)
	})
}

func Test_Load(t *testing.T) {
	t.Parallel()

	t.Run("not configured", func(t *testing.T) {
		t.Parallel()

		_, err := Load(testPaths(t))
		require.ErrorIs(t, err, ErrNotConfigured)
	})

	t.Run("file readable by others", func(t *testing.T) {
		t.Parallel()

		paths := testPaths(t)
		require.NoError(t, Save(paths, Config{
			APIID:   1,
			APIHash: testAPIHash,
		}))
		require.NoError(t, os.Chmod(paths.ConfigFile, 0o644))

		_, err := Load(paths)
		require.ErrorIs(t, err, ErrInsecurePermissions)
	})

	t.Run("dir readable by others", func(t *testing.T) {
		t.Parallel()

		paths := testPaths(t)
		require.NoError(t, Save(paths, Config{
			APIID:   1,
			APIHash: testAPIHash,
		}))
		require.NoError(t, os.Chmod(paths.ConfigDir, 0o755))

		_, err := Load(paths)
		require.ErrorIs(t, err, ErrInsecurePermissions)
	})

	t.Run("malformed json", func(t *testing.T) {
		t.Parallel()

		paths := testPaths(t)
		require.NoError(t, EnsurePrivateDir(paths.ConfigDir))
		require.NoError(t, os.WriteFile(paths.ConfigFile, []byte("{"), 0o600))

		_, err := Load(paths)
		require.ErrorIs(t, err, ErrInvalidConfig)
	})
}

func Test_Config_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid without allowlist",
			cfg: Config{
				APIID:   1,
				APIHash: testAPIHash,
			},
		},
		{
			name: "valid with allowlist",
			cfg: Config{
				APIID:        1,
				APIHash:      testAPIHash,
				AllowedChats: []string{"@durov", "42"},
			},
		},
		{
			name:    "zero api id",
			cfg:     Config{APIHash: testAPIHash},
			wantErr: true,
		},
		{
			name: "negative api id",
			cfg: Config{
				APIID:   -1,
				APIHash: testAPIHash,
			},
			wantErr: true,
		},
		{
			name: "uppercase api hash",
			cfg: Config{
				APIID:   1,
				APIHash: "0123456789ABCDEF0123456789ABCDEF",
			},
			wantErr: true,
		},
		{
			name: "short api hash",
			cfg: Config{
				APIID:   1,
				APIHash: "abc",
			},
			wantErr: true,
		},
		{
			name: "bad allowed chat",
			cfg: Config{
				APIID:        1,
				APIHash:      testAPIHash,
				AllowedChats: []string{"durov"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.cfg.Validate()
			if tt.wantErr {
				require.ErrorIs(t, err, ErrInvalidConfig)
				return
			}
			require.NoError(t, err)
		})
	}
}

func Test_Config_Allow(t *testing.T) {
	t.Parallel()

	cfg := Config{AllowedChats: []string{"@Durov", "42"}}

	added, err := cfg.Allow(domain.ChatRef{Username: "durov"}, domain.ChatRef{ID: -1001}, domain.ChatRef{ID: -1001})
	require.NoError(t, err)
	require.Equal(t, 1, added)
	require.Equal(t, []string{"@Durov", "42", "-1001"}, cfg.AllowedChats)
}

func Test_Config_Deny(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		allowed     []string
		deny        []domain.ChatRef
		wantRemoved int
		want        []string
	}{
		{
			name:        "removes by normalized ref",
			allowed:     []string{"@Durov", "42", "-1001"},
			deny:        []domain.ChatRef{{Username: "durov"}, {ID: -1001}},
			wantRemoved: 2,
			want:        []string{"42"},
		},
		{
			name:    "unknown ref keeps list",
			allowed: []string{"42"},
			deny:    []domain.ChatRef{{ID: 7}},
			want:    []string{"42"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := Config{AllowedChats: tt.allowed}

			removed, err := cfg.Deny(tt.deny...)
			require.NoError(t, err)
			require.Equal(t, tt.wantRemoved, removed)
			require.Equal(t, tt.want, cfg.AllowedChats)
		})
	}
}

func Test_Config_ChatRefs(t *testing.T) {
	t.Parallel()

	cfg := Config{AllowedChats: []string{"@Durov", "-1001234567890"}}

	refs, err := cfg.ChatRefs()
	require.NoError(t, err)
	require.Equal(t, []domain.ChatRef{{Username: "durov"}, {ID: -1001234567890}}, refs)
}
