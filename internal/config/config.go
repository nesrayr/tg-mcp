// Package config loads and saves tg-mcp settings.
//
// All files live in a directory with mode 0700, and the files themselves are 0600: the config
// holds api_hash, and next to it is the session file with full access to the account.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"

	"github.com/nesrayr/tg-mcp/internal/domain"
)

const (
	dirPerm  fs.FileMode = 0o700
	filePerm fs.FileMode = 0o600

	configDirEnv = "TG_MCP_CONFIG_DIR"
	cacheDirEnv  = "TG_MCP_CACHE_DIR"
)

var (
	ErrNotConfigured       = errors.New("tg-mcp is not configured, run `tg-mcp setup`")
	ErrInsecurePermissions = errors.New("insecure permissions")
	ErrInvalidConfig       = errors.New("invalid config")
	apiHashPattern         = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

// Config is the content of config.json.
type Config struct {
	APIID        int      `json:"api_id"`
	APIHash      string   `json:"api_hash"`
	AllowedChats []string `json:"allowed_chats"`
}

// Paths is the location of tg-mcp files.
type Paths struct {
	ConfigDir   string
	ConfigFile  string
	SessionFile string
	MediaDir    string
}

// DefaultPaths returns paths under ~/.config/tg-mcp and ~/.cache/tg-mcp.
// The directories can be overridden with TG_MCP_CONFIG_DIR and TG_MCP_CACHE_DIR.
func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("resolve home dir: %w", err)
	}

	configDir := envOr(configDirEnv, filepath.Join(home, ".config", "tg-mcp"))
	cacheDir := envOr(cacheDirEnv, filepath.Join(home, ".cache", "tg-mcp"))

	return Paths{
		ConfigDir:   configDir,
		ConfigFile:  filepath.Join(configDir, "config.json"),
		SessionFile: filepath.Join(configDir, "session.json"),
		MediaDir:    filepath.Join(cacheDir, "media"),
	}, nil
}

// Load reads and validates the config. It refuses to work if the directory or file
// is accessible to anyone other than the owner.
func Load(paths Paths) (Config, error) {
	if err := CheckPrivate(paths.ConfigDir); err != nil {
		return Config{}, err
	}
	if err := CheckPrivate(paths.ConfigFile); err != nil {
		return Config{}, err
	}

	cfg, err := Read(paths)
	if err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Read reads the config without permission checks or validation. setup needs it to keep
// allowed_chats when overwriting the keys, even if the old config is invalid.
func Read(paths Paths) (Config, error) {
	data, err := os.ReadFile(paths.ConfigFile)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, ErrNotConfigured
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("%w: parse %s: %v", ErrInvalidConfig, paths.ConfigFile, err)
	}
	return cfg, nil
}

// Save atomically writes the config with mode 0600.
func Save(paths Paths, cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := EnsurePrivateDir(paths.ConfigDir); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	return writeFileAtomic(paths.ConfigFile, append(data, '\n'))
}

// Validate checks the required fields and the chat allowlist.
func (c Config) Validate() error {
	if c.APIID <= 0 {
		return fmt.Errorf("%w: api_id must be a positive number", ErrInvalidConfig)
	}
	if !apiHashPattern.MatchString(c.APIHash) {
		return fmt.Errorf("%w: api_hash must be 32 lowercase hex characters", ErrInvalidConfig)
	}
	if _, err := c.ChatRefs(); err != nil {
		return err
	}
	return nil
}

// ChatRefs parses the chat allowlist.
func (c Config) ChatRefs() ([]domain.ChatRef, error) {
	refs := make([]domain.ChatRef, 0, len(c.AllowedChats))
	for _, raw := range c.AllowedChats {
		ref, err := domain.ParseChatRef(raw)
		if err != nil {
			return nil, fmt.Errorf("%w: allowed_chats: %w", ErrInvalidConfig, err)
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

// Allow adds chats to the allowlist, skipping those already present. Returns the number of new ones.
func (c *Config) Allow(refs ...domain.ChatRef) (int, error) {
	current, err := c.ChatRefs()
	if err != nil {
		return 0, err
	}

	added := 0
	for _, ref := range refs {
		if slices.Contains(current, ref) {
			continue
		}
		current = append(current, ref)
		c.AllowedChats = append(c.AllowedChats, ref.String())
		added++
	}
	return added, nil
}

// Deny removes chats from the allowlist. Returns the number of removed ones.
func (c *Config) Deny(refs ...domain.ChatRef) (int, error) {
	current, err := c.ChatRefs()
	if err != nil {
		return 0, err
	}

	kept := make([]string, 0, len(current))
	for i, ref := range current {
		if !slices.Contains(refs, ref) {
			kept = append(kept, c.AllowedChats[i])
		}
	}
	removed := len(c.AllowedChats) - len(kept)
	c.AllowedChats = kept
	return removed, nil
}

// EnsurePrivateDir creates a directory with mode 0700 and fixes the mode of an existing one.
func EnsurePrivateDir(dir string) error {
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("create dir %s: %w", dir, err)
	}
	if err := os.Chmod(dir, dirPerm); err != nil {
		return fmt.Errorf("chmod dir %s: %w", dir, err)
	}
	return nil
}

// CheckPrivate checks that a file or directory is accessible only by its owner.
func CheckPrivate(path string) error {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNotConfigured
	}
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%w: %s has mode %04o, expected access for owner only (chmod %s)",
			ErrInsecurePermissions, path, info.Mode().Perm(), modeHint(info))
	}
	return nil
}

func modeHint(info fs.FileInfo) string {
	if info.IsDir() {
		return "700"
	}
	return "600"
}

func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmp.Name())

	if err := tmp.Chmod(filePerm); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
