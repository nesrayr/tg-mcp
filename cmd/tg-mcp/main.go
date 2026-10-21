// Command tg-mcp is an MCP server for reading Telegram through a user account.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

const version = "0.1.0"

const usage = `tg-mcp — a read-only MCP server for Telegram.

Usage:
  tg-mcp setup              save api_id and api_hash
  tg-mcp login [--code]     log in: QR code (default) or code from Telegram
  tg-mcp chats [--limit N]  show chats with IDs to choose which ones to allow
  tg-mcp allow <chat>...    allow chats: ID or @username
  tg-mcp deny <chat>...     deny chats
  tg-mcp serve              run the MCP server on stdio

setup, login, chats and allow only work in an interactive terminal.
`

type command func(ctx context.Context, args []string) error

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))

	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	commands := map[string]command{
		"setup": runSetup,
		"login": runLogin,
		"chats": runChats,
		"allow": runAllow,
		"deny":  runDeny,
		"serve": runServe,
	}

	name := os.Args[1]
	switch name {
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	case "-v", "--version", "version":
		fmt.Println(version)
		return
	}

	cmd, ok := commands[name]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", name, usage)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := cmd(ctx, os.Args[2:]); err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, "canceled")
			os.Exit(130)
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
