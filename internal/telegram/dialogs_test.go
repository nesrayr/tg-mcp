package telegram

import (
	"context"
	"testing"
	"time"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
	"github.com/stretchr/testify/require"

	"github.com/nesrayr/tg-mcp/internal/domain"
)

func Test_AllDialogs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	page := func() *tg.MessagesDialogs {
		return &tg.MessagesDialogs{
			Dialogs: []tg.DialogClass{
				&tg.Dialog{
					Peer:       &tg.PeerChannel{ChannelID: newsChanID},
					TopMessage: 20,
				},
				&tg.Dialog{
					Peer:        &tg.PeerUser{UserID: momUserID},
					TopMessage:  10,
					UnreadCount: 2,
				},
			},
			Messages: []tg.MessageClass{
				&tg.Message{
					ID:     20,
					PeerID: &tg.PeerChannel{ChannelID: newsChanID},
					Date:   unix(time.Hour),
				},
				&tg.Message{
					ID:     10,
					PeerID: &tg.PeerUser{UserID: momUserID},
					Date:   unix(0),
				},
			},
			Chats: []tg.ChatClass{newsChannel()},
			Users: []tg.UserClass{momUser()},
		}
	}

	t.Run("lists every dialog regardless of allowlist", func(t *testing.T) {
		t.Parallel()

		mock, api := newMock(t)
		mock.ExpectFunc(func(b bin.Encoder) {
			_, ok := b.(*tg.MessagesGetDialogsRequest)
			require.True(t, ok)
		}).ThenResult(page())
		mock.ExpectFunc(func(b bin.Encoder) {
			_, ok := b.(*tg.MessagesGetDialogsRequest)
			require.True(t, ok)
		}).ThenResult(&tg.MessagesDialogs{})

		got, err := AllDialogs(ctx, api, 10)
		require.NoError(t, err)
		require.Equal(t, []domain.Chat{
			{
				ID:            newsChatID,
				Kind:          domain.ChatKindChannel,
				Title:         "Go News",
				Username:      "gonews",
				LastMessageAt: baseTime.Add(time.Hour),
			},
			{
				ID:            momChatID,
				Kind:          domain.ChatKindPrivate,
				Title:         "Mom",
				Username:      "mama_tg",
				UnreadCount:   2,
				LastMessageAt: baseTime,
			},
		}, got)
	})

	t.Run("stops at limit", func(t *testing.T) {
		t.Parallel()

		mock, api := newMock(t)
		mock.ExpectFunc(func(b bin.Encoder) {
			_, ok := b.(*tg.MessagesGetDialogsRequest)
			require.True(t, ok)
		}).ThenResult(page())

		got, err := AllDialogs(ctx, api, 1)
		require.NoError(t, err)
		require.Len(t, got, 1)
	})
}
