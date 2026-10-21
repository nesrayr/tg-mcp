package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"
	"strconv"
	"text/tabwriter"
	"time"

	gotd "github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth/qrlogin"
	"github.com/gotd/td/tg"

	"github.com/nesrayr/tg-mcp/internal/config"
	"github.com/nesrayr/tg-mcp/internal/domain"
	"github.com/nesrayr/tg-mcp/internal/login"
	"github.com/nesrayr/tg-mcp/internal/telegram"
)

const loginTimeout = 10 * time.Minute

var ErrNotLoggedIn = errors.New("not logged in, run `tg-mcp login` first")

func runSetup(ctx context.Context, args []string) error {
	if err := flag.NewFlagSet("setup", flag.ContinueOnError).Parse(args); err != nil {
		return err
	}
	prompt, err := login.NewTerminal()
	if err != nil {
		return err
	}
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	// Overwriting the keys must not lose an already configured allowlist.
	existing, err := config.Read(paths)
	if err != nil && !errors.Is(err, config.ErrNotConfigured) {
		fmt.Fprintf(os.Stderr, "Could not read the old config (%v), allowed_chats will be empty.\n", err)
	}

	fmt.Fprintln(os.Stderr, "Get the keys at https://my.telegram.org → API development tools.")
	rawID, err := prompt.Line(ctx, "api_id: ")
	if err != nil {
		return err
	}
	apiID, err := strconv.Atoi(rawID)
	if err != nil {
		return fmt.Errorf("api_id must be a number: %w", err)
	}
	apiHash, err := prompt.Secret(ctx, "api_hash (input hidden): ")
	if err != nil {
		return err
	}

	cfg := config.Config{APIID: apiID, APIHash: apiHash, AllowedChats: existing.AllowedChats}
	if err := config.Save(paths, cfg); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Saved to %s (mode 0600). Next: tg-mcp login\n", paths.ConfigFile)
	return nil
}

func runLogin(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("login", flag.ContinueOnError)
	withCode := flags.Bool("code", false, "log in with a phone number and code instead of QR")
	if err := flags.Parse(args); err != nil {
		return err
	}

	prompt, err := login.NewTerminal()
	if err != nil {
		return err
	}
	paths, cfg, err := loadConfig()
	if err != nil {
		return err
	}

	dispatcher := tg.NewUpdateDispatcher()
	loggedIn := qrlogin.OnLoginToken(dispatcher)
	client := telegram.NewClient(telegram.ClientOptions{
		APIID:         cfg.APIID,
		APIHash:       cfg.APIHash,
		SessionFile:   paths.SessionFile,
		Policy:        telegram.LoginPolicy,
		AppVersion:    version,
		UpdateHandler: dispatcher,
	})

	ctx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()

	return client.Run(ctx, func(ctx context.Context) error {
		user, err := login.Status(ctx, client)
		if err != nil {
			return err
		}
		if user == nil {
			if *withCode {
				user, err = login.WithCode(ctx, client, prompt)
			} else {
				user, err = login.WithQR(ctx, client, loggedIn, prompt, os.Stdout)
			}
			if err != nil {
				return err
			}
		} else {
			fmt.Fprintln(os.Stderr, "The session is already authorized.")
		}

		if err := os.Chmod(paths.SessionFile, 0o600); err != nil {
			return fmt.Errorf("chmod session: %w", err)
		}
		fmt.Fprintf(os.Stderr, "\nLogged in as %s. Session: %s\n", describeUser(user), paths.SessionFile)
		fmt.Fprintln(os.Stderr, "You can revoke access in Telegram: Settings → Devices → tg-mcp.")
		fmt.Fprintln(os.Stderr, "Next: tg-mcp chats, then tg-mcp allow <ID or @username>.")
		return nil
	})
}

func runChats(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("chats", flag.ContinueOnError)
	limit := flags.Int("limit", 200, "how many recent chats to show")
	if err := flags.Parse(args); err != nil {
		return err
	}

	// The full chat list is for the owner's eyes only: not to a pipe, not to agent logs.
	if !login.IsTerminal(os.Stdout) {
		return login.ErrNotTerminal
	}

	paths, cfg, err := loadConfig()
	if err != nil {
		return err
	}
	refs, err := cfg.ChatRefs()
	if err != nil {
		return err
	}

	return withSession(ctx, paths, cfg, func(ctx context.Context, client *gotd.Client) error {
		chats, err := telegram.AllDialogs(ctx, client.API(), *limit)
		if err != nil {
			return err
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "\tID\tTYPE\tUSERNAME\tUNREAD\tTITLE")
		for _, chat := range chats {
			mark := ""
			if slices.ContainsFunc(refs, func(ref domain.ChatRef) bool { return ref.Matches(chat) }) {
				mark = "✓"
			}
			username := ""
			if chat.Username != "" {
				username = "@" + chat.Username
			}
			fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%d\t%s\n", mark, chat.ID, chat.Kind, username, chat.UnreadCount, chat.Title)
		}
		if err := w.Flush(); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "\n✓ = already allowed. To allow: tg-mcp allow <ID or @username>\n")
		return nil
	})
}

func runAllow(_ context.Context, args []string) error {
	// Only a human at the terminal can extend access: this way an agent that was
	// talked into it by a chat message cannot open new chats for itself.
	if !login.IsTerminal(os.Stdin) || !login.IsTerminal(os.Stdout) {
		return login.ErrNotTerminal
	}
	return editAllowlist(args, func(cfg *config.Config, refs []domain.ChatRef) (int, error) {
		return cfg.Allow(refs...)
	}, "added")
}

func runDeny(_ context.Context, args []string) error {
	return editAllowlist(args, func(cfg *config.Config, refs []domain.ChatRef) (int, error) {
		return cfg.Deny(refs...)
	}, "removed")
}

func editAllowlist(args []string, edit func(*config.Config, []domain.ChatRef) (int, error), verb string) error {
	if len(args) == 0 {
		return errors.New("specify at least one chat: ID or @username")
	}
	refs := make([]domain.ChatRef, 0, len(args))
	for _, arg := range args {
		ref, err := domain.ParseChatRef(arg)
		if err != nil {
			return err
		}
		refs = append(refs, ref)
	}

	paths, cfg, err := loadConfig()
	if err != nil {
		return err
	}
	changed, err := edit(&cfg, refs)
	if err != nil {
		return err
	}
	if err := config.Save(paths, cfg); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "%s: %d. Allowed chats: %v\n", verb, changed, cfg.AllowedChats)
	if changed > 0 {
		fmt.Fprintln(os.Stderr, "Restart the MCP server (or Claude Code) for the changes to take effect.")
	}
	return nil
}

func runServe(context.Context, []string) error {
	return errors.New("serve is not implemented yet")
}

func loadConfig() (config.Paths, config.Config, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return config.Paths{}, config.Config{}, err
	}
	cfg, err := config.Load(paths)
	if err != nil {
		return config.Paths{}, config.Config{}, err
	}
	return paths, cfg, nil
}

// withSession starts a read-only client on top of an existing session.
func withSession(ctx context.Context, paths config.Paths, cfg config.Config, fn func(context.Context, *gotd.Client) error) error {
	if err := config.CheckPrivate(paths.SessionFile); err != nil {
		if errors.Is(err, config.ErrNotConfigured) {
			return ErrNotLoggedIn
		}
		return err
	}

	client := telegram.NewClient(telegram.ClientOptions{
		APIID:       cfg.APIID,
		APIHash:     cfg.APIHash,
		SessionFile: paths.SessionFile,
		Policy:      telegram.ReadOnlyPolicy,
		AppVersion:  version,
	})
	return client.Run(ctx, func(ctx context.Context) error {
		user, err := login.Status(ctx, client)
		if err != nil {
			return err
		}
		if user == nil {
			return ErrNotLoggedIn
		}
		return fn(ctx, client)
	})
}

func describeUser(user *tg.User) string {
	name := user.FirstName
	if user.LastName != "" {
		name += " " + user.LastName
	}
	if user.Username != "" {
		name += " (@" + user.Username + ")"
	}
	return name
}
