# tg-mcp

An MCP server that gives an AI agent (Claude Code and other MCP clients) **read-only access**
to selected chats of your personal Telegram account: messages, search, photos, PDF, DOCX and XLSX.

## Security model

| Threat | Mitigation |
|---|---|
| The agent sends, deletes or forwards a message | There are no such calls in the code, and the `Guard` middleware rejects any RPC method outside an allowlist of 10 read methods before it reaches the network |
| The agent "reads" messages on your behalf | `messages.readHistory` and similar methods are blocked: unread stays unread, the "online" status does not change |
| Access to all conversations | Only chats in `allowed_chats` are visible. The rest are indistinguishable from nonexistent ones; search runs over each allowed chat separately, never globally |
| The agent extends its own access through MCP | No MCP tool changes the allowlist. `tg-mcp allow` and `tg-mcp chats` refuse to run without an interactive terminal, which stops attempts from a pipe but not an agent with an unrestricted shell (see [Agents with shell access](#agents-with-shell-access)) |
| Prompt injection in messages | Every response is marked as untrusted data. Control and invisible characters (bidi overrides, zero-width characters, Unicode tags, variation selectors) are stripped from the text and from error messages. Length is limited |
| Session leak | Config and session live in `~/.config/tg-mcp/` (directory 0700, files 0600); the server refuses to start with looser permissions |
| Malicious file name | Attachments are saved as `<chat_id>_<message_id>.<ext>`; the sender's file name never becomes part of the path |
| Malicious DOCX/XLSX | Text is extracted by the server itself using the Go standard library: macros are not executed, external XML entities are not loaded; decompressed size, XML nesting, tag size, element counts and extracted text size are bounded, so a small file cannot exhaust memory |

Residual risk: chat text ends up in the model's context. The server sends nothing, but
keep the agent's network tools (WebFetch and the like) behind a confirmation prompt.

To revoke access: Telegram → Settings → Devices → **tg-mcp** → Terminate session.

### Agents with shell access

The guarantees above cover what the agent can do through the MCP tools. tg-mcp runs as your user,
so the operating system does not separate its files from an agent that can also run shell commands
or read files as your user:

- `~/.config/tg-mcp/session.json` gives **full access to your Telegram account**, including sending
  messages. An agent that can read it bypasses every restriction above.
- An agent can add chats by editing `~/.config/tg-mcp/config.json` directly or by running
  `tg-mcp allow` inside a pseudo-terminal.

In Claude Code, add deny rules to `~/.claude/settings.json`:

```json
{
  "permissions": {
    "deny": [
      "Read(~/.config/tg-mcp/**)",
      "Edit(~/.config/tg-mcp/**)",
      "Bash(tg-mcp allow *)"
    ]
  }
}
```

These rules cover Claude Code's file tools and common shell commands such as `cat`, but they are
not a security boundary: a script or a wrapped command (`sh -c …`) can get around them. For real
isolation, keep shell commands on manual approval, or enable Claude Code's
[sandboxed Bash](https://code.claude.com/docs/en/sandboxing) and deny it reads of `~/.config/tg-mcp`.

## Installation

Requires Go 1.27+.

```sh
go install github.com/nesrayr/tg-mcp/cmd/tg-mcp@latest
```

The binary goes to `$(go env GOPATH)/bin/tg-mcp` (usually `~/go/bin/tg-mcp`); make sure that directory is in your `PATH`.

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
   claude mcp add tg-mcp -s user -- "$(go env GOPATH)/bin/tg-mcp" serve
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
| `search_messages` | Text search in one chat or across allowed chats (up to the first 50 in `allowed_chats` order) |
| `get_media` | Photos as an image in the response; DOCX and XLSX as extracted text (tables row by row, merged cells expanded, dates in the cell's format); PDFs and large images as a file in `~/.cache/tg-mcp/media/` |

Legacy `.doc`/`.xls` formats are not supported.

Limits: attachments up to 20 MB, inline images up to 5 MB, photos at up to 1280 px, Excel sheets up to 2000 rows and 100 columns,
up to ~60,000 characters of text per response; files are deleted after 7 days.

## Files

| Path | Contents |
|---|---|
| `~/.config/tg-mcp/config.json` | `api_id`, `api_hash`, `allowed_chats` |
| `~/.config/tg-mcp/session.json` | Telegram session: **full access to the account**, never share it |
| `~/.cache/tg-mcp/media/` | Downloaded PDFs and large images |

The directories can be overridden with `TG_MCP_CONFIG_DIR` and `TG_MCP_CACHE_DIR`.

## Development

```sh
make test       # go test -race ./... and the memory limit test without -race
make lint       # go vet + staticcheck
make generate   # mocks (mockgen)
```

Layout:

- `internal/telegram`: the layer over gotd with `Guard` (RPC allowlist), `Directory` (chat allowlist) and `Reader`
- `internal/mcpserver`: MCP tools and attachment storage
- `internal/textsafe`: cleaning of untrusted text
- `internal/office`: text extraction from DOCX and XLSX
- `internal/login`: QR and code login
- `internal/config`: config with permission checks
- `cmd/tg-mcp`: CLI

When adding a Telegram API call, add the method to `ReadOnlyPolicy` (`internal/telegram/guard.go`)
and to the `Test_ReadOnlyPolicy_Methods` test, otherwise the request will be rejected with `ErrMethodNotAllowed`.
