# tg-mcp

An MCP server that gives an AI agent (Claude Code and other MCP clients) **read-only access**
to selected chats of your personal Telegram account: messages, search, photos and PDFs.

## Security model

| Threat | Mitigation |
|---|---|
| The agent sends, deletes or forwards a message | There are no such calls in the code, and the `Guard` middleware rejects any RPC method outside an allowlist of 10 read methods before it reaches the network |
| The agent "reads" messages on your behalf | `messages.readHistory` and similar methods are blocked: unread stays unread, the "online" status does not change |
| Access to all conversations | Only chats in `allowed_chats` are visible. The rest are indistinguishable from nonexistent ones; search runs over each allowed chat separately, never globally |
| The agent extends its own access | `tg-mcp allow` and `tg-mcp chats` only work in an interactive terminal, not from a pipe or the agent's shell |
| Prompt injection in messages | Every response is marked as untrusted data. Control characters, bidi overrides, zero-width characters and Unicode tags are stripped from the text. Length is limited |
| Session leak | Config and session live in `~/.config/tg-mcp/` (directory 0700, files 0600); the server refuses to start with looser permissions |
| Malicious file name | Attachments are saved as `<chat_id>_<message_id>.<ext>`; the sender's file name never becomes part of the path |

Residual risk: chat text ends up in the model's context. The server sends nothing, but
keep the agent's network tools (WebFetch and the like) behind a confirmation prompt.

To revoke access: Telegram → Settings → Devices → **tg-mcp** → Terminate session.

## Installation

Requires Go 1.27+.

```sh
cd ~/tg-mcp
make install          # binary goes to ~/go/bin/tg-mcp
```

## Setup

Run all commands below **in a separate terminal tab**, not through the agent.

1. **App credentials.** At https://my.telegram.org → API development tools, create an application
   (Platform: Desktop) to get an `api_id` and `api_hash`. They identify the application and do not grant access
   to the account, but you still should not publish them.

   ```sh
   tg-mcp setup      # api_hash input is hidden
   ```

2. **Login.** QR code by default: Telegram on your phone → Settings → Devices → Link Desktop
   Device. Only scan a QR code you generated yourself.

   ```sh
   tg-mcp login          # QR
   tg-mcp login --code   # fallback: phone number and code from Telegram
   ```

3. **Choose chats.**

   ```sh
   tg-mcp chats                        # all chats with IDs; ✓ = already allowed
   tg-mcp allow @username -1001234567  # allow
   tg-mcp deny -1001234567             # deny
   ```

   After changing the list, restart the MCP server (in Claude Code: `/mcp` → reconnect, or restart it).

4. **Connect to Claude Code.**

   ```sh
   claude mcp add tg-mcp -s user -- ~/go/bin/tg-mcp serve
   ```

   To let the agent open downloaded PDFs without asking every time, add this to `~/.claude/settings.json`:

   ```json
   { "permissions": { "allow": ["Read(~/.cache/tg-mcp/media/**)"] } }
   ```

## MCP tools

| Tool | What it does |
|---|---|
| `list_chats` | Allowed chats: ID, type, title, unread count, time of the last message |
| `read_messages` | Chat messages in order; `before_id`/`after_id` for paging, up to 100 at a time |
| `search_messages` | Text search in one or all allowed chats |
| `get_media` | Photos as an image in the response; PDFs and large images as a file in `~/.cache/tg-mcp/media/` |

Limits: attachments up to 20 MB, inline images up to 5 MB, photos at up to 1280 px,
up to ~60,000 characters of text per response; files are deleted after 7 days.

## Files

| Path | Contents |
|---|---|
| `~/.config/tg-mcp/config.json` | `api_id`, `api_hash`, `allowed_chats` |
| `~/.config/tg-mcp/session.json` | Telegram session — **full access to the account**, never share it |
| `~/.cache/tg-mcp/media/` | Downloaded PDFs and large images |

The directories can be overridden with `TG_MCP_CONFIG_DIR` and `TG_MCP_CACHE_DIR`.

## Development

```sh
make test       # go test -race ./...
make lint       # go vet + staticcheck
make generate   # mocks (mockgen)
```

Layout:

- `internal/telegram` — layer over gotd: `Guard` (RPC allowlist), `Directory` (chat allowlist), `Reader`
- `internal/mcpserver` — MCP tools, text sanitization, attachment storage
- `internal/login` — QR and code login
- `internal/config` — config with permission checks
- `cmd/tg-mcp` — CLI

When adding a Telegram API call, add the method to `ReadOnlyPolicy` (`internal/telegram/guard.go`)
and to the `Test_ReadOnlyPolicy_Methods` test, otherwise the request will be rejected with `ErrMethodNotAllowed`.
