package telegram

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgmock"
	"github.com/stretchr/testify/require"

	"github.com/nesrayr/tg-mcp/internal/domain"
)

func newTestReader(t *testing.T) (*tgmock.Mock, *Reader) {
	t.Helper()

	mock, api := newMock(t)
	expectResolveUsernames(mock)
	return mock, NewReader(api, NewDirectory(api, allowedRefs(), discardLogs), ReaderOptions{
		MaxMediaSize:    testMaxMedia,
		DownloadTimeout: 5 * time.Second,
	})
}

func Test_Reader_ListChats(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	momChat := domain.Chat{
		ID:            momChatID,
		Kind:          domain.ChatKindPrivate,
		Title:         "Mom",
		Username:      "mama_tg",
		UnreadCount:   3,
		LastMessageAt: baseTime,
	}
	newsChat := domain.Chat{
		ID:            newsChatID,
		Kind:          domain.ChatKindChannel,
		Title:         "Go News",
		Username:      "gonews",
		LastMessageAt: baseTime.Add(time.Hour),
	}

	tests := []struct {
		name       string
		unreadOnly bool
		want       []domain.Chat
	}{
		{
			name: "all allowed chats, newest first",
			want: []domain.Chat{newsChat, momChat},
		},
		{
			name:       "only unread",
			unreadOnly: true,
			want:       []domain.Chat{momChat},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, reader := newTestReader(t)
			mock.ExpectCall(&tg.MessagesGetPeerDialogsRequest{Peers: []tg.InputDialogPeerClass{
				&tg.InputDialogPeer{Peer: momPeer},
				&tg.InputDialogPeer{Peer: newsPeer},
			}}).ThenResult(&tg.MessagesPeerDialogs{
				Dialogs: []tg.DialogClass{
					&tg.Dialog{
						Peer:        &tg.PeerUser{UserID: momUserID},
						TopMessage:  10,
						UnreadCount: 3,
					},
					&tg.Dialog{
						Peer:       &tg.PeerChannel{ChannelID: newsChanID},
						TopMessage: 20,
					},
				},
				Messages: []tg.MessageClass{
					&tg.Message{
						ID:     10,
						PeerID: &tg.PeerUser{UserID: momUserID},
						Date:   unix(0),
					},
					&tg.MessageService{
						ID:     20,
						PeerID: &tg.PeerChannel{ChannelID: newsChanID},
						Date:   unix(time.Hour),
						Action: &tg.MessageActionPinMessage{},
					},
				},
				State: tg.UpdatesState{},
			})

			got, err := reader.ListChats(ctx, tt.unreadOnly)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func Test_Reader_History(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("maps messages in chronological order", func(t *testing.T) {
		t.Parallel()

		mock, reader := newTestReader(t)
		mock.ExpectCall(&tg.MessagesGetHistoryRequest{
			Peer:     momPeer,
			OffsetID: 50,
			MinID:    5,
			Limit:    20,
		}).ThenResult(&tg.MessagesMessages{
			Messages: []tg.MessageClass{
				&tg.Message{
					ID:      12,
					Out:     true,
					PeerID:  &tg.PeerUser{UserID: momUserID},
					FromID:  &tg.PeerUser{UserID: selfUserID},
					Date:    unix(time.Minute),
					Message: "Check out this photo",
					ReplyTo: &tg.MessageReplyHeader{ReplyToMsgID: 11},
					Media:   &tg.MessageMediaPhoto{Photo: testPhoto()},
				},
				&tg.MessageService{
					ID:     13,
					PeerID: &tg.PeerUser{UserID: momUserID},
					Date:   unix(0),
					Action: &tg.MessageActionHistoryClear{},
				},
				&tg.Message{
					ID:       11,
					PeerID:   &tg.PeerUser{UserID: momUserID},
					Date:     unix(0),
					Message:  "Hi!",
					EditDate: unix(30 * time.Second),
					FwdFrom:  tg.MessageFwdHeader{Date: unix(-time.Hour)},
				},
			},
			Users: []tg.UserClass{selfUser(), momUser()},
		})

		got, err := reader.History(ctx, domain.ChatRef{ID: momChatID}, HistoryParams{
			BeforeID: 50,
			AfterID:  5,
		})
		require.NoError(t, err)
		require.Equal(t, []domain.Message{
			{
				ID:        11,
				ChatID:    momChatID,
				Date:      baseTime,
				EditedAt:  baseTime.Add(30 * time.Second),
				SenderID:  momChatID,
				Sender:    "Mom",
				Text:      "Hi!",
				Forwarded: true,
			},
			{
				ID:        12,
				ChatID:    momChatID,
				Date:      baseTime.Add(time.Minute),
				SenderID:  userChatID(selfUserID),
				Sender:    "Me",
				Outgoing:  true,
				Text:      "Check out this photo",
				ReplyToID: 11,
				Media: &domain.Media{
					Kind:         domain.MediaKindPhoto,
					MimeType:     "image/jpeg",
					Size:         500,
					Downloadable: true,
				},
			},
		}, got)
	})

	t.Run("after id pages forward from that message", func(t *testing.T) {
		t.Parallel()

		mock, reader := newTestReader(t)
		mock.ExpectCall(&tg.MessagesGetHistoryRequest{
			Peer:      momPeer,
			OffsetID:  10,
			AddOffset: -5,
			MinID:     10,
			Limit:     5,
		}).ThenResult(&tg.MessagesMessages{
			Messages: []tg.MessageClass{
				&tg.Message{
					ID:      12,
					PeerID:  &tg.PeerUser{UserID: momUserID},
					Date:    unix(time.Minute),
					Message: "second",
				},
				&tg.Message{
					ID:      11,
					PeerID:  &tg.PeerUser{UserID: momUserID},
					Date:    unix(0),
					Message: "first",
				},
			},
			Users: []tg.UserClass{momUser()},
		})

		got, err := reader.History(ctx, domain.ChatRef{ID: momChatID}, HistoryParams{
			Limit:   5,
			AfterID: 10,
		})
		require.NoError(t, err)
		require.Equal(t, []int{11, 12}, []int{got[0].ID, got[1].ID})
	})

	t.Run("limit is clamped", func(t *testing.T) {
		t.Parallel()

		mock, reader := newTestReader(t)
		mock.ExpectCall(&tg.MessagesGetHistoryRequest{
			Peer:  newsPeer,
			Limit: MaxMessagesLimit,
		}).ThenResult(&tg.MessagesChannelMessages{})

		got, err := reader.History(ctx, domain.ChatRef{Username: "gonews"}, HistoryParams{Limit: 500})
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("chat outside allowlist", func(t *testing.T) {
		t.Parallel()

		_, reader := newTestReader(t)

		_, err := reader.History(ctx, domain.ChatRef{ID: 31337}, HistoryParams{})
		require.ErrorIs(t, err, ErrChatUnavailable)
	})
}

func Test_Reader_Search(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	momMessage := func(id int, offset time.Duration) tg.MessageClass {
		return &tg.Message{
			ID:      id,
			PeerID:  &tg.PeerUser{UserID: momUserID},
			Date:    unix(offset),
			Message: "report",
		}
	}
	newsMessage := func(id int, offset time.Duration) tg.MessageClass {
		return &tg.Message{
			ID:      id,
			PeerID:  &tg.PeerChannel{ChannelID: newsChanID},
			Date:    unix(offset),
			Message: "report",
		}
	}

	t.Run("across allowed chats, newest first and trimmed", func(t *testing.T) {
		t.Parallel()

		mock, reader := newTestReader(t)
		mock.ExpectCall(&tg.MessagesSearchRequest{
			Peer:   momPeer,
			Q:      "report",
			Filter: &tg.InputMessagesFilterEmpty{},
			Limit:  2,
		}).
			ThenResult(&tg.MessagesMessages{
				Messages: []tg.MessageClass{momMessage(3, 3*time.Hour), momMessage(1, time.Hour)},
				Users:    []tg.UserClass{momUser()},
			})
		mock.ExpectCall(&tg.MessagesSearchRequest{
			Peer:   newsPeer,
			Q:      "report",
			Filter: &tg.InputMessagesFilterEmpty{},
			Limit:  2,
		}).
			ThenResult(&tg.MessagesChannelMessages{
				Messages: []tg.MessageClass{newsMessage(7, 2*time.Hour)},
				Chats:    []tg.ChatClass{newsChannel()},
			})

		got, err := reader.Search(ctx, SearchParams{
			Query: "report",
			Limit: 2,
		})
		require.NoError(t, err)
		require.Len(t, got.Messages, 2)
		require.Equal(t, []int{3, 7}, []int{got.Messages[0].ID, got.Messages[1].ID})
		require.Equal(t, []int64{momChatID, newsChatID}, []int64{got.Messages[0].ChatID, got.Messages[1].ChatID})
		require.Equal(t, "Go News", got.Messages[1].Sender)
		require.Zero(t, got.SkippedChats)
	})

	t.Run("reports chats beyond the search limit", func(t *testing.T) {
		t.Parallel()

		mock, api := newMock(t)
		dialogs := &tg.MessagesDialogs{}
		refs := make([]domain.ChatRef, 0, MaxSearchChats+2)
		for id := int64(100); id < 100+MaxSearchChats+2; id++ {
			dialogs.Dialogs = append(dialogs.Dialogs, &tg.Dialog{
				Peer:       &tg.PeerUser{UserID: id},
				TopMessage: 1,
			})
			dialogs.Users = append(dialogs.Users, &tg.User{
				ID:         id,
				AccessHash: id,
				FirstName:  "User",
			})
			refs = append(refs, domain.ChatRef{ID: userChatID(id)})
		}
		mock.ExpectFunc(func(b bin.Encoder) {
			_, ok := b.(*tg.MessagesGetDialogsRequest)
			require.True(t, ok)
		}).ThenResult(dialogs)
		for id := int64(100); id < 100+MaxSearchChats; id++ {
			mock.ExpectCall(&tg.MessagesSearchRequest{
				Peer: &tg.InputPeerUser{
					UserID:     id,
					AccessHash: id,
				},
				Q:      "report",
				Filter: &tg.InputMessagesFilterEmpty{},
				Limit:  20,
			}).ThenResult(&tg.MessagesMessages{})
		}
		reader := NewReader(api, NewDirectory(api, refs, discardLogs), ReaderOptions{
			MaxMediaSize:    testMaxMedia,
			DownloadTimeout: 5 * time.Second,
		})

		got, err := reader.Search(ctx, SearchParams{Query: "report"})
		require.NoError(t, err)
		require.Empty(t, got.Messages)
		require.Equal(t, 2, got.SkippedChats)
	})

	t.Run("in a single chat", func(t *testing.T) {
		t.Parallel()

		mock, reader := newTestReader(t)
		mock.ExpectCall(&tg.MessagesSearchRequest{
			Peer:   newsPeer,
			Q:      "release",
			Filter: &tg.InputMessagesFilterEmpty{},
			Limit:  20,
		}).
			ThenResult(&tg.MessagesChannelMessages{
				Messages: []tg.MessageClass{newsMessage(7, 0)},
				Chats:    []tg.ChatClass{newsChannel()},
			})

		got, err := reader.Search(ctx, SearchParams{
			Query: "release",
			Chat:  &domain.ChatRef{ID: newsChatID},
		})
		require.NoError(t, err)
		require.Len(t, got.Messages, 1)
	})

	t.Run("chat outside allowlist", func(t *testing.T) {
		t.Parallel()

		_, reader := newTestReader(t)

		_, err := reader.Search(ctx, SearchParams{
			Query: "x",
			Chat:  &domain.ChatRef{Username: "durov"},
		})
		require.ErrorIs(t, err, ErrChatUnavailable)
	})
}

func Test_Reader_DownloadMedia(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	momMessages := func(msg *tg.Message) *tg.MessagesMessages {
		return &tg.MessagesMessages{
			Messages: []tg.MessageClass{msg},
			Users:    []tg.UserClass{momUser()},
		}
	}
	expectFile := func(mock *tgmock.Mock, location tg.InputFileLocationClass, data []byte) {
		mock.ExpectFunc(func(b bin.Encoder) {
			request, ok := b.(*tg.UploadGetFileRequest)
			require.True(t, ok)
			require.Equal(t, location, request.Location)
			require.Zero(t, request.Offset)
		}).ThenResult(&tg.UploadFile{
			Type:  &tg.StorageFileJpeg{},
			Bytes: data,
		})
	}

	t.Run("photo from private chat", func(t *testing.T) {
		t.Parallel()

		mock, reader := newTestReader(t)
		mock.ExpectCall(&tg.MessagesGetMessagesRequest{ID: []tg.InputMessageClass{&tg.InputMessageID{ID: 12}}}).
			ThenResult(momMessages(&tg.Message{
				ID:     12,
				PeerID: &tg.PeerUser{UserID: momUserID},
				Date:   unix(0),
				Media:  &tg.MessageMediaPhoto{Photo: testPhoto()},
			}))
		expectFile(mock, &tg.InputPhotoFileLocation{
			ID:            900,
			AccessHash:    901,
			FileReference: []byte{9, 9},
			ThumbSize:     "y",
		}, []byte("jpeg-bytes"))

		var buf bytes.Buffer
		attachment, err := reader.DownloadMedia(ctx, domain.ChatRef{ID: momChatID}, 12, &buf)
		require.NoError(t, err)
		require.Equal(t, momChatID, attachment.ChatID)
		require.Equal(t, domain.MediaKindPhoto, attachment.Media.Kind)
		require.Equal(t, "jpeg-bytes", buf.String())
	})

	t.Run("pdf from channel", func(t *testing.T) {
		t.Parallel()

		mock, reader := newTestReader(t)
		document := testDocument("application/pdf", 3, &tg.DocumentAttributeFilename{FileName: "report.pdf"})
		mock.ExpectCall(&tg.ChannelsGetMessagesRequest{
			Channel: &tg.InputChannel{
				ChannelID:  newsChanID,
				AccessHash: 7770,
			},
			ID: []tg.InputMessageClass{&tg.InputMessageID{ID: 7}},
		}).ThenResult(&tg.MessagesChannelMessages{
			Messages: []tg.MessageClass{&tg.Message{
				ID:     7,
				PeerID: &tg.PeerChannel{ChannelID: newsChanID},
				Date:   unix(0),
				Media:  &tg.MessageMediaDocument{Document: document},
			}},
			Chats: []tg.ChatClass{newsChannel()},
		})
		expectFile(mock, &tg.InputDocumentFileLocation{
			ID:            800,
			AccessHash:    801,
			FileReference: []byte{8},
		}, []byte("pdf"))

		var buf bytes.Buffer
		attachment, err := reader.DownloadMedia(ctx, domain.ChatRef{Username: "gonews"}, 7, &buf)
		require.NoError(t, err)
		require.Equal(t, domain.Attachment{
			ChatID:    newsChatID,
			MessageID: 7,
			Media: domain.Media{
				Kind:         domain.MediaKindPDF,
				FileName:     "report.pdf",
				MimeType:     "application/pdf",
				Size:         3,
				Downloadable: true,
			},
		}, attachment)
		require.Equal(t, "pdf", buf.String())
	})

	t.Run("rejects", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name    string
			message *tg.Message
			wantErr error
		}{
			{
				name: "voice message",
				message: &tg.Message{
					ID:     12,
					PeerID: &tg.PeerUser{UserID: momUserID},
					Media:  &tg.MessageMediaDocument{Document: testDocument("audio/ogg", 10, &tg.DocumentAttributeAudio{Voice: true})},
				},
				wantErr: ErrMediaNotDownloadable,
			},
			{
				name: "message without media",
				message: &tg.Message{
					ID:      12,
					PeerID:  &tg.PeerUser{UserID: momUserID},
					Message: "text",
				},
				wantErr: ErrMediaNotDownloadable,
			},
			{
				name: "pdf over limit",
				message: &tg.Message{
					ID:     12,
					PeerID: &tg.PeerUser{UserID: momUserID},
					Media:  &tg.MessageMediaDocument{Document: testDocument("application/pdf", testMaxMedia+1)},
				},
				wantErr: ErrMediaTooLarge,
			},
			{
				name: "message from another chat",
				message: &tg.Message{
					ID:     12,
					PeerID: &tg.PeerUser{UserID: 555},
					Media:  &tg.MessageMediaPhoto{Photo: testPhoto()},
				},
				wantErr: ErrMessageNotFound,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				mock, reader := newTestReader(t)
				mock.ExpectCall(&tg.MessagesGetMessagesRequest{ID: []tg.InputMessageClass{&tg.InputMessageID{ID: 12}}}).
					ThenResult(momMessages(tt.message))

				_, err := reader.DownloadMedia(ctx, domain.ChatRef{ID: momChatID}, 12, &bytes.Buffer{})
				require.ErrorIs(t, err, tt.wantErr)
			})
		}
	})

	t.Run("file larger than declared size", func(t *testing.T) {
		t.Parallel()

		mock, reader := newTestReader(t)
		document := testDocument("application/pdf", 10)
		mock.ExpectCall(&tg.MessagesGetMessagesRequest{ID: []tg.InputMessageClass{&tg.InputMessageID{ID: 12}}}).
			ThenResult(momMessages(&tg.Message{
				ID:     12,
				PeerID: &tg.PeerUser{UserID: momUserID},
				Media:  &tg.MessageMediaDocument{Document: document},
			}))
		expectFile(mock, document.AsInputDocumentFileLocation(""), bytes.Repeat([]byte{1}, int(testMaxMedia)+1))

		_, err := reader.DownloadMedia(ctx, domain.ChatRef{ID: momChatID}, 12, &bytes.Buffer{})
		require.ErrorIs(t, err, ErrMediaTooLarge)
	})
}
