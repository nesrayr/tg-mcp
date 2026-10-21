package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

	"github.com/nesrayr/tg-mcp/internal/domain"
)

// ErrChatUnavailable means the chat was not found or is not in allowed_chats. The reasons are deliberately
// not distinguished, so as not to reveal the existence of chats outside the allowlist.
var ErrChatUnavailable = errors.New("chat not found or not in allowed_chats")

const (
	dialogsBatchSize = 100
	maxDialogsToScan = 10_000
)

// Directory knows only allowlisted chats. Anything outside the allowlist does not exist for the server.
//
// Chats are resolved once (access_hash is stable in Telegram), so after changing
// allowed_chats the server must be restarted. Resolution failures caused by the network are not cached.
type Directory struct {
	api    *tg.Client
	refs   []domain.ChatRef
	logger *slog.Logger

	mu       sync.Mutex
	resolved []resolvedChat
	ready    bool
}

func NewDirectory(api *tg.Client, refs []domain.ChatRef, logger *slog.Logger) *Directory {
	return &Directory{api: api, refs: refs, logger: logger}
}

// Allowed returns the allowed chats in allowed_chats order.
func (d *Directory) Allowed(ctx context.Context) ([]resolvedChat, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.ready {
		return d.resolved, nil
	}

	resolved, err := d.resolve(ctx)
	if err != nil {
		return nil, err
	}
	d.resolved, d.ready = resolved, true
	return d.resolved, nil
}

// Resolve looks up a chat by reference among the allowed chats only.
func (d *Directory) Resolve(ctx context.Context, ref domain.ChatRef) (resolvedChat, error) {
	allowed, err := d.Allowed(ctx)
	if err != nil {
		return resolvedChat{}, err
	}
	for _, chat := range allowed {
		if ref.Matches(chat.chat) {
			return chat, nil
		}
	}
	return resolvedChat{}, fmt.Errorf("%w: %s", ErrChatUnavailable, ref)
}

func (d *Directory) resolve(ctx context.Context) ([]resolvedChat, error) {
	found := make(map[domain.ChatRef]resolvedChat, len(d.refs))
	pendingIDs := make(map[int64]domain.ChatRef)

	for _, ref := range d.refs {
		if ref.Username == "" {
			pendingIDs[ref.ID] = ref
			continue
		}
		chat, ok, err := d.resolveUsername(ctx, ref.Username)
		if err != nil {
			return nil, err
		}
		if ok {
			found[ref] = chat
		}
	}

	// A chat may be given both by username and by ID: no need to look it up twice.
	for _, chat := range found {
		if ref, ok := pendingIDs[chat.chat.ID]; ok {
			found[ref] = chat
			delete(pendingIDs, chat.chat.ID)
		}
	}

	if len(pendingIDs) > 0 {
		if err := d.scanDialogs(ctx, pendingIDs, found); err != nil {
			return nil, err
		}
	}

	result := make([]resolvedChat, 0, len(found))
	seen := make(map[int64]struct{}, len(found))
	for _, ref := range d.refs {
		chat, ok := found[ref]
		if !ok {
			continue
		}
		if _, dup := seen[chat.chat.ID]; dup {
			continue
		}
		seen[chat.chat.ID] = struct{}{}
		result = append(result, chat)
	}

	if missing := len(d.refs) - len(found); missing > 0 {
		d.logger.Warn("some allowed chats were not found", "missing", missing, "total", len(d.refs))
	}
	return result, nil
}

func (d *Directory) resolveUsername(ctx context.Context, username string) (resolvedChat, bool, error) {
	resolved, err := d.api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: username})
	if tgerr.Is(err, "USERNAME_NOT_OCCUPIED", "USERNAME_INVALID") {
		return resolvedChat{}, false, nil
	}
	if err != nil {
		return resolvedChat{}, false, fmt.Errorf("resolve username: %w", err)
	}

	chat, ok := newEntities(resolved.Users, resolved.Chats).resolvePeer(resolved.Peer)
	return chat, ok, nil
}

// scanDialogs looks for numeric IDs in the dialog list: it is the only place to get an
// access_hash for a chat known only by ID. Stops as soon as all of them are found.
func (d *Directory) scanDialogs(ctx context.Context, pending map[int64]domain.ChatRef, found map[domain.ChatRef]resolvedChat) error {
	iter := dialogs.NewQueryBuilder(d.api).GetDialogs().BatchSize(dialogsBatchSize).Iter()

	for scanned := 0; len(pending) > 0 && scanned < maxDialogsToScan && iter.Next(ctx); scanned++ {
		elem := iter.Value()
		dialog, ok := elem.Dialog.(*tg.Dialog)
		if !ok {
			continue
		}
		id, ok := peerChatID(dialog.Peer)
		if !ok {
			continue
		}
		ref, wanted := pending[id]
		if !wanted {
			continue
		}

		ent := entities{
			users:    elem.Entities.Users(),
			chats:    elem.Entities.Chats(),
			channels: elem.Entities.Channels(),
		}
		if chat, ok := ent.resolvePeer(dialog.Peer); ok {
			found[ref] = chat
			delete(pending, id)
		}
	}

	if err := iter.Err(); err != nil {
		return fmt.Errorf("scan dialogs: %w", err)
	}
	return nil
}
