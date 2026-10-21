package telegram

import (
	"context"
	"errors"
	"testing"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/stretchr/testify/require"

	"github.com/nesrayr/tg-mcp/internal/domain"
)

func Test_Directory_Allowed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("resolves usernames and ids in allowlist order", func(t *testing.T) {
		t.Parallel()

		mock, api := newMock(t)
		mock.ExpectCall(&tg.ContactsResolveUsernameRequest{Username: "gonews"}).ThenResult(&tg.ContactsResolvedPeer{
			Peer:  &tg.PeerChannel{ChannelID: newsChanID},
			Chats: []tg.ChatClass{newsChannel()},
		})
		mock.ExpectFunc(func(b bin.Encoder) {
			request, ok := b.(*tg.MessagesGetDialogsRequest)
			require.True(t, ok)
			require.Equal(t, dialogsBatchSize, request.Limit)
		}).ThenResult(&tg.MessagesDialogs{
			Dialogs: []tg.DialogClass{
				&tg.Dialog{Peer: &tg.PeerChat{ChatID: 5}, TopMessage: 11},
				&tg.Dialog{Peer: &tg.PeerUser{UserID: momUserID}, TopMessage: 10},
			},
			Messages: []tg.MessageClass{
				&tg.Message{ID: 11, PeerID: &tg.PeerChat{ChatID: 5}, Date: unix(0)},
				&tg.Message{ID: 10, PeerID: &tg.PeerUser{UserID: momUserID}, Date: unix(0)},
			},
			Chats: []tg.ChatClass{&tg.Chat{ID: 5, Title: "Work", Photo: &tg.ChatPhotoEmpty{}}},
			Users: []tg.UserClass{momUser()},
		})
		mock.ExpectFunc(func(b bin.Encoder) {
			_, ok := b.(*tg.MessagesGetDialogsRequest)
			require.True(t, ok)
		}).ThenResult(&tg.MessagesDialogs{})

		refs := []domain.ChatRef{{ID: momChatID}, {Username: "gonews"}, {ID: 999}, {ID: newsChatID}}
		directory := NewDirectory(api, refs, discardLogs)

		allowed, err := directory.Allowed(ctx)
		require.NoError(t, err)
		require.Equal(t, []resolvedChat{
			{chat: domain.Chat{ID: momChatID, Kind: domain.ChatKindPrivate, Title: "Mom", Username: "mama_tg"}, peer: momPeer},
			{chat: domain.Chat{ID: newsChatID, Kind: domain.ChatKindChannel, Title: "Go News", Username: "gonews"}, peer: newsPeer},
		}, allowed)

		cached, err := directory.Allowed(ctx)
		require.NoError(t, err)
		require.Equal(t, allowed, cached)
	})

	t.Run("empty allowlist makes no requests", func(t *testing.T) {
		t.Parallel()

		_, api := newMock(t)

		allowed, err := NewDirectory(api, nil, discardLogs).Allowed(ctx)
		require.NoError(t, err)
		require.Empty(t, allowed)
	})

	t.Run("unknown username is skipped", func(t *testing.T) {
		t.Parallel()

		mock, api := newMock(t)
		mock.ExpectCall(&tg.ContactsResolveUsernameRequest{Username: "nobody_here"}).
			ThenRPCErr(tgerr.New(400, "USERNAME_NOT_OCCUPIED"))

		allowed, err := NewDirectory(api, []domain.ChatRef{{Username: "nobody_here"}}, discardLogs).Allowed(ctx)
		require.NoError(t, err)
		require.Empty(t, allowed)
	})

	t.Run("network error is not cached", func(t *testing.T) {
		t.Parallel()

		mock, api := newMock(t)
		mock.ExpectCall(&tg.ContactsResolveUsernameRequest{Username: "gonews"}).ThenErr(errors.New("connection reset"))
		mock.ExpectCall(&tg.ContactsResolveUsernameRequest{Username: "gonews"}).ThenResult(&tg.ContactsResolvedPeer{
			Peer:  &tg.PeerChannel{ChannelID: newsChanID},
			Chats: []tg.ChatClass{newsChannel()},
		})
		directory := NewDirectory(api, []domain.ChatRef{{Username: "gonews"}}, discardLogs)

		_, err := directory.Allowed(ctx)
		require.Error(t, err)

		allowed, err := directory.Allowed(ctx)
		require.NoError(t, err)
		require.Len(t, allowed, 1)
	})
}

func Test_Directory_Resolve(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name    string
		ref     domain.ChatRef
		want    int64
		wantErr bool
	}{
		{name: "by id", ref: domain.ChatRef{ID: newsChatID}, want: newsChatID},
		{name: "by username in any case", ref: domain.ChatRef{Username: "MAMA_TG"}, want: momChatID},
		{name: "chat outside allowlist", ref: domain.ChatRef{ID: 12345}, wantErr: true},
		{name: "username outside allowlist", ref: domain.ChatRef{Username: "durov"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, api := newMock(t)
			expectResolveUsernames(mock)

			got, err := NewDirectory(api, allowedRefs(), discardLogs).Resolve(ctx, tt.ref)
			if tt.wantErr {
				require.ErrorIs(t, err, ErrChatUnavailable)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got.chat.ID)
		})
	}
}
