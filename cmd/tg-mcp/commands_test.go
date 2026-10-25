package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nesrayr/tg-mcp/internal/config"
	"github.com/nesrayr/tg-mcp/internal/login"
)

func Test_RequireTerminal(t *testing.T) {
	tests := []struct {
		name string
		run  command
		args []string
	}{
		{
			name: "allow",
			run:  runAllow,
			args: []string{"@durov"},
		},
		{
			name: "chats",
			run:  runChats,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "tg-mcp")
			t.Setenv("TG_MCP_CONFIG_DIR", dir)
			t.Setenv("TG_MCP_CACHE_DIR", t.TempDir())
			paths, err := config.DefaultPaths()
			require.NoError(t, err)
			cfg := config.Config{
				APIID:   1,
				APIHash: "0123456789abcdef0123456789abcdef",
			}
			require.NoError(t, config.Save(paths, cfg))

			stdin, stdout := os.Stdin, os.Stdout
			t.Cleanup(func() {
				os.Stdin, os.Stdout = stdin, stdout
			})
			reader, writer, err := os.Pipe()
			require.NoError(t, err)
			t.Cleanup(func() {
				_ = reader.Close()
				_ = writer.Close()
			})
			os.Stdin, os.Stdout = reader, writer

			err = tt.run(context.Background(), tt.args)

			require.ErrorIs(t, err, login.ErrNotTerminal)
			got, err := config.Load(paths)
			require.NoError(t, err)
			require.Equal(t, cfg, got)
		})
	}
}
