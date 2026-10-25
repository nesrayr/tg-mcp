package telegram

import (
	"context"
	"fmt"
	"time"

	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"

	"github.com/nesrayr/tg-mcp/internal/domain"
)

// AllDialogs returns all chats of the account, ignoring the allowlist.
func AllDialogs(ctx context.Context, api *tg.Client, limit int) ([]domain.Chat, error) {
	// Only for the `tg-mcp chats` command; the MCP server must never see chats outside the allowlist.
	iter := dialogs.NewQueryBuilder(api).GetDialogs().BatchSize(dialogsBatchSize).Iter()

	var chats []domain.Chat
	for len(chats) < limit && iter.Next(ctx) {
		elem := iter.Value()
		dialog, ok := elem.Dialog.(*tg.Dialog)
		if !ok {
			continue
		}

		ent := entities{
			users:    elem.Entities.Users(),
			chats:    elem.Entities.Chats(),
			channels: elem.Entities.Channels(),
		}
		resolved, ok := ent.resolvePeer(dialog.Peer)
		if !ok {
			continue
		}

		chat := resolved.chat
		chat.UnreadCount = dialog.UnreadCount
		if elem.Last != nil {
			chat.LastMessageAt = time.Unix(int64(elem.Last.GetDate()), 0).UTC()
		}
		chats = append(chats, chat)
	}

	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("list dialogs: %w", err)
	}
	return chats, nil
}
