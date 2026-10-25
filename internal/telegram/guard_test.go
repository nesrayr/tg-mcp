package telegram

import (
	"context"
	"testing"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgmock"
	"github.com/stretchr/testify/require"
)

func guardedAPI(t *testing.T, policy Policy) (*tgmock.Mock, *tg.Client) {
	t.Helper()

	mock := tgmock.NewRequire(t)
	return mock, tg.NewClient(Guard(policy).Handle(mock))
}

func Test_ReadOnlyPolicy_Methods(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{
		"auth.exportAuthorization",
		"channels.getMessages",
		"contacts.resolveUsername",
		"messages.getDialogs",
		"messages.getHistory",
		"messages.getMessages",
		"messages.getPeerDialogs",
		"messages.search",
		"upload.getFile",
		"users.getUsers",
	}, ReadOnlyPolicy.Methods())
}

func Test_LoginPolicy_Methods(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{
		"account.getPassword",
		"auth.checkPassword",
		"auth.exportAuthorization",
		"auth.exportLoginToken",
		"auth.importLoginToken",
		"auth.sendCode",
		"auth.signIn",
		"channels.getMessages",
		"contacts.resolveUsername",
		"messages.getDialogs",
		"messages.getHistory",
		"messages.getMessages",
		"messages.getPeerDialogs",
		"messages.search",
		"upload.getFile",
		"users.getUsers",
	}, LoginPolicy.Methods())
	require.False(t, LoginPolicy.Allows(&tg.AuthSignUpRequest{}))
	require.False(t, ReadOnlyPolicy.Allows(&tg.AuthSendCodeRequest{}))
}

func Test_Guard_BlocksWriteMethods(t *testing.T) {
	t.Parallel()

	peer := &tg.InputPeerUser{
		UserID:     42,
		AccessHash: 1,
	}

	tests := []struct {
		name   string
		method string
		call   func(ctx context.Context, api *tg.Client) error
	}{
		{
			name:   "send message",
			method: "messages.sendMessage",
			call: func(ctx context.Context, api *tg.Client) error {
				_, err := api.MessagesSendMessage(ctx, &tg.MessagesSendMessageRequest{
					Peer:     peer,
					Message:  "hi",
					RandomID: 1,
				})
				return err
			},
		},
		{
			name:   "forward messages",
			method: "messages.forwardMessages",
			call: func(ctx context.Context, api *tg.Client) error {
				_, err := api.MessagesForwardMessages(ctx, &tg.MessagesForwardMessagesRequest{
					FromPeer: peer,
					ToPeer:   peer,
					ID:       []int{1},
					RandomID: []int64{1},
				})
				return err
			},
		},
		{
			name:   "delete messages",
			method: "messages.deleteMessages",
			call: func(ctx context.Context, api *tg.Client) error {
				_, err := api.MessagesDeleteMessages(ctx, &tg.MessagesDeleteMessagesRequest{ID: []int{1}})
				return err
			},
		},
		{
			name:   "mark history read",
			method: "messages.readHistory",
			call: func(ctx context.Context, api *tg.Client) error {
				_, err := api.MessagesReadHistory(ctx, &tg.MessagesReadHistoryRequest{
					Peer:  peer,
					MaxID: 1,
				})
				return err
			},
		},
		{
			name:   "mark channel read",
			method: "channels.readHistory",
			call: func(ctx context.Context, api *tg.Client) error {
				_, err := api.ChannelsReadHistory(ctx, &tg.ChannelsReadHistoryRequest{
					Channel: &tg.InputChannel{ChannelID: 1},
					MaxID:   1,
				})
				return err
			},
		},
		{
			name:   "typing status",
			method: "messages.setTyping",
			call: func(ctx context.Context, api *tg.Client) error {
				_, err := api.MessagesSetTyping(ctx, &tg.MessagesSetTypingRequest{
					Peer:   peer,
					Action: &tg.SendMessageTypingAction{},
				})
				return err
			},
		},
		{
			name:   "online status",
			method: "account.updateStatus",
			call: func(ctx context.Context, api *tg.Client) error {
				_, err := api.AccountUpdateStatus(ctx, false)
				return err
			},
		},
		{
			name:   "join channel",
			method: "channels.joinChannel",
			call: func(ctx context.Context, api *tg.Client) error {
				_, err := api.ChannelsJoinChannel(ctx, &tg.InputChannel{ChannelID: 1})
				return err
			},
		},
		{
			name:   "global search",
			method: "messages.searchGlobal",
			call: func(ctx context.Context, api *tg.Client) error {
				_, err := api.MessagesSearchGlobal(ctx, &tg.MessagesSearchGlobalRequest{
					Q:          "x",
					Filter:     &tg.InputMessagesFilterEmpty{},
					OffsetPeer: &tg.InputPeerEmpty{},
				})
				return err
			},
		},
		{
			name:   "log out",
			method: "auth.logOut",
			call: func(ctx context.Context, api *tg.Client) error {
				_, err := api.AuthLogOut(ctx)
				return err
			},
		},
		{
			name:   "list authorizations",
			method: "account.getAuthorizations",
			call: func(ctx context.Context, api *tg.Client) error {
				_, err := api.AccountGetAuthorizations(ctx)
				return err
			},
		},
		{
			name:   "updates state",
			method: "updates.getState",
			call: func(ctx context.Context, api *tg.Client) error {
				_, err := api.UpdatesGetState(ctx)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, api := guardedAPI(t, ReadOnlyPolicy)

			err := tt.call(context.Background(), api)

			require.ErrorIs(t, err, ErrMethodNotAllowed)
			var notAllowed *MethodNotAllowedError
			require.ErrorAs(t, err, &notAllowed)
			require.Equal(t, tt.method, notAllowed.Method)
		})
	}
}

func Test_Guard_PassesAllowedMethods(t *testing.T) {
	t.Parallel()

	mock, api := guardedAPI(t, ReadOnlyPolicy)
	request := &tg.ContactsResolveUsernameRequest{Username: "durov"}
	mock.ExpectCall(request).ThenResult(&tg.ContactsResolvedPeer{Peer: &tg.PeerUser{UserID: 1}})

	result, err := api.ContactsResolveUsername(context.Background(), request)

	require.NoError(t, err)
	require.Equal(t, &tg.PeerUser{UserID: 1}, result.Peer)
}

func Test_Policy(t *testing.T) {
	t.Parallel()

	t.Run("with extends without mutating base", func(t *testing.T) {
		t.Parallel()

		base := NewPolicy(&tg.MessagesGetHistoryRequest{})
		extended := base.With(&tg.AuthSendCodeRequest{})

		require.Equal(t, []string{"messages.getHistory"}, base.Methods())
		require.Equal(t, []string{"auth.sendCode", "messages.getHistory"}, extended.Methods())
	})

	t.Run("untyped input is denied", func(t *testing.T) {
		t.Parallel()

		require.False(t, ReadOnlyPolicy.Allows(untypedEncoder{}))
	})

	t.Run("empty policy denies everything", func(t *testing.T) {
		t.Parallel()

		require.False(t, NewPolicy().Allows(&tg.MessagesGetHistoryRequest{}))
	})
}

type untypedEncoder struct{}

func (untypedEncoder) Encode(*bin.Buffer) error { return nil }
