package telegram

import (
	"log/slog"
	"testing"
	"time"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgmock"

	"github.com/nesrayr/tg-mcp/internal/domain"
)

const (
	selfUserID   int64 = 1
	momUserID    int64 = 42
	newsChanID   int64 = 777
	testMaxMedia int64 = 1000
)

var (
	baseTime    = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	momChatID   = userChatID(momUserID)
	newsChatID  = channelChatID(newsChanID)
	momPeer     = &tg.InputPeerUser{UserID: momUserID, AccessHash: 420}
	newsPeer    = &tg.InputPeerChannel{ChannelID: newsChanID, AccessHash: 7770}
	discardLogs = slog.New(slog.DiscardHandler)
)

func newMock(t *testing.T) (*tgmock.Mock, *tg.Client) {
	t.Helper()

	mock := tgmock.NewRequire(t)
	return mock, tg.NewClient(mock)
}

func momUser() *tg.User {
	return &tg.User{ID: momUserID, AccessHash: 420, FirstName: "Mom", Username: "mama_tg"}
}

func selfUser() *tg.User {
	return &tg.User{ID: selfUserID, AccessHash: 10, FirstName: "Me", Self: true}
}

func newsChannel() *tg.Channel {
	return &tg.Channel{
		ID:         newsChanID,
		AccessHash: 7770,
		Title:      "Go News",
		Username:   "gonews",
		Broadcast:  true,
		Photo:      &tg.ChatPhotoEmpty{},
	}
}

func unix(offset time.Duration) int {
	return int(baseTime.Add(offset).Unix())
}

func testPhoto() *tg.Photo {
	return &tg.Photo{
		ID:            900,
		AccessHash:    901,
		FileReference: []byte{9, 9},
		DCID:          2,
		Sizes: []tg.PhotoSizeClass{
			&tg.PhotoSize{Type: "m", W: 320, H: 240, Size: 100},
			&tg.PhotoSize{Type: "y", W: 1280, H: 960, Size: 500},
			&tg.PhotoSize{Type: "w", W: 2560, H: 1920, Size: 2000},
		},
	}
}

func testDocument(mime string, size int64, attrs ...tg.DocumentAttributeClass) *tg.Document {
	return &tg.Document{
		ID:            800,
		AccessHash:    801,
		FileReference: []byte{8},
		MimeType:      mime,
		Size:          size,
		DCID:          2,
		Attributes:    attrs,
	}
}

func expectResolveUsernames(mock *tgmock.Mock) {
	mock.ExpectCall(&tg.ContactsResolveUsernameRequest{Username: "mama_tg"}).ThenResult(&tg.ContactsResolvedPeer{
		Peer:  &tg.PeerUser{UserID: momUserID},
		Users: []tg.UserClass{momUser()},
	})
	mock.ExpectCall(&tg.ContactsResolveUsernameRequest{Username: "gonews"}).ThenResult(&tg.ContactsResolvedPeer{
		Peer:  &tg.PeerChannel{ChannelID: newsChanID},
		Chats: []tg.ChatClass{newsChannel()},
	})
}

func allowedRefs() []domain.ChatRef {
	return []domain.ChatRef{{Username: "mama_tg"}, {Username: "gonews"}}
}
