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

var (
	// ErrChatUnavailable does not tell a missing chat from one outside allowed_chats, so the allowlist leaks nothing.
	ErrChatUnavailable = errors.New("chat not found or not in allowed_chats")
	// ErrUsernameNotFound means no chat has the username now.
	ErrUsernameNotFound = errors.New("username not found")
)

const (
	dialogsBatchSize = 100
	maxDialogsToScan = 10_000
)

// Directory knows only allowlisted chats. Anything outside the allowlist does not exist for the server.
type Directory struct {
	api    *tg.Client
	refs   []domain.ChatRef
	logger *slog.Logger

	mu       sync.Mutex
	resolved []resolvedChat
	ready    bool
}

func NewDirectory(api *tg.Client, refs []domain.ChatRef, logger *slog.Logger) *Directory {
	return &Directory{
		api:    api,
		refs:   refs,
		logger: logger,
	}
}

// Allowed returns the allowed chats in allowed_chats order.
func (d *Directory) Allowed(ctx context.Context) ([]resolvedChat, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Resolved once: access_hash is stable, so changing allowed_chats needs a restart.
	if d.ready {
		return d.resolved, nil
	}

	resolved, err := d.resolve(ctx)
	if err != nil {
		// Not cached, so a network failure is retried on the next call.
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
	unpinned := 0

	for _, ref := range d.refs {
		if ref.Username == "" {
			pendingIDs[ref.ID] = ref
			continue
		}
		chat, ok, err := resolveUsername(ctx, d.api, ref.Username)
		if err != nil {
			return nil, err
		}
		switch {
		case !ok:
		case ref.Pinned() && chat.chat.ID != ref.ID:
			// A released username can be taken by someone else: never follow it to another chat.
			d.logger.Warn("allowed username now points to another chat, skipped; run `tg-mcp allow` again to trust it",
				"ref", ref.String(), "chat_id", chat.chat.ID)
		default:
			found[ref] = chat
		}
		if !ref.Pinned() {
			unpinned++
		}
	}
	if unpinned > 0 {
		d.logger.Warn("allowed usernames are not pinned to a chat ID; run `tg-mcp allow @username` again to pin them",
			"count", unpinned)
	}

	// A chat may be given both by username and by ID: no need to look it up twice.
	for _, chat := range found {
		if ref, ok := pendingIDs[chat.chat.ID]; ok {
			found[ref] = chat
			delete(pendingIDs, chat.chat.ID)
		}
	}

	// The dialog list is the only place to get an access_hash for a chat known only by ID.
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

// PinUsernames pins every unpinned @username reference to the ID of the chat it points to now.
func PinUsernames(ctx context.Context, api *tg.Client, refs []domain.ChatRef) ([]domain.ChatRef, error) {
	pinned := make([]domain.ChatRef, 0, len(refs))
	for _, ref := range refs {
		if ref.Username == "" || ref.Pinned() {
			pinned = append(pinned, ref)
			continue
		}
		chat, ok, err := resolveUsername(ctx, api, ref.Username)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("%w: @%s", ErrUsernameNotFound, ref.Username)
		}
		pinned = append(pinned, domain.ChatRef{
			ID:       chat.chat.ID,
			Username: ref.Username,
		})
	}
	return pinned, nil
}

func resolveUsername(ctx context.Context, api *tg.Client, username string) (resolvedChat, bool, error) {
	resolved, err := api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: username})
	if tgerr.Is(err, "USERNAME_NOT_OCCUPIED", "USERNAME_INVALID") {
		return resolvedChat{}, false, nil
	}
	if err != nil {
		return resolvedChat{}, false, fmt.Errorf("resolve username: %w", err)
	}

	chat, ok := newEntities(resolved.Users, resolved.Chats).resolvePeer(resolved.Peer)
	return chat, ok, nil
}

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
