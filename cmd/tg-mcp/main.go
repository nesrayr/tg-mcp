// Command tg-mcp is an MCP server for reading Telegram through a user account.
package main

import (
	"fmt"
	"os"
)

const usage = `tg-mcp — a read-only MCP server for Telegram.

Usage:
  tg-mcp setup    save api_id and api_hash
  tg-mcp login    log in (QR code or code from Telegram)
  tg-mcp chats    show all chats with IDs to fill in allowed_chats
  tg-mcp serve    run the MCP server on stdio
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	switch os.Args[1] {
	case "setup", "login", "chats", "serve":
		fmt.Fprintf(os.Stderr, "command %q is not implemented yet\n", os.Args[1])
		os.Exit(1)
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
}
