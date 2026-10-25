package telegram

import (
	"strings"
	"time"

	"github.com/gotd/td/constant"
	"github.com/gotd/td/tg"

	"github.com/nesrayr/tg-mcp/internal/domain"
)

type entities struct {
	users    map[int64]*tg.User
	chats    map[int64]*tg.Chat
	channels map[int64]*tg.Channel
}

func newEntities(users []tg.UserClass, chats []tg.ChatClass) entities {
	return entities{
		users:    tg.UserClassArray(users).UserToMap(),
		chats:    tg.ChatClassArray(chats).ChatToMap(),
		channels: tg.ChatClassArray(chats).ChannelToMap(),
	}
}

type resolvedChat struct {
	chat domain.Chat
	peer tg.InputPeerClass
}

func userChatID(id int64) int64 {
	var peerID constant.TDLibPeerID
	peerID.User(id)
	return int64(peerID)
}

func groupChatID(id int64) int64 {
	var peerID constant.TDLibPeerID
	peerID.Chat(id)
	return int64(peerID)
}

func channelChatID(id int64) int64 {
	var peerID constant.TDLibPeerID
	peerID.Channel(id)
	return int64(peerID)
}

func peerChatID(peer tg.PeerClass) (int64, bool) {
	switch p := peer.(type) {
	case *tg.PeerUser:
		return userChatID(p.UserID), true
	case *tg.PeerChat:
		return groupChatID(p.ChatID), true
	case *tg.PeerChannel:
		return channelChatID(p.ChannelID), true
	default:
		return 0, false
	}
}

func (e entities) resolvePeer(peer tg.PeerClass) (resolvedChat, bool) {
	switch p := peer.(type) {
	case *tg.PeerUser:
		user, ok := e.users[p.UserID]
		if !ok {
			return resolvedChat{}, false
		}
		kind := domain.ChatKindPrivate
		if user.Bot {
			kind = domain.ChatKindBot
		}
		return resolvedChat{
			chat: domain.Chat{
				ID:       userChatID(user.ID),
				Kind:     kind,
				Title:    userName(user),
				Username: user.Username,
			},
			peer: user.AsInputPeer(),
		}, true
	case *tg.PeerChat:
		chat, ok := e.chats[p.ChatID]
		if !ok {
			return resolvedChat{}, false
		}
		return resolvedChat{
			chat: domain.Chat{
				ID:    groupChatID(chat.ID),
				Kind:  domain.ChatKindGroup,
				Title: chat.Title,
			},
			peer: &tg.InputPeerChat{ChatID: chat.ID},
		}, true
	case *tg.PeerChannel:
		channel, ok := e.channels[p.ChannelID]
		if !ok {
			return resolvedChat{}, false
		}
		kind := domain.ChatKindGroup
		if channel.Broadcast {
			kind = domain.ChatKindChannel
		}
		return resolvedChat{
			chat: domain.Chat{
				ID:       channelChatID(channel.ID),
				Kind:     kind,
				Title:    channel.Title,
				Username: channel.Username,
			},
			peer: channel.AsInputPeer(),
		}, true
	default:
		return resolvedChat{}, false
	}
}

func (e entities) peerName(peer tg.PeerClass) string {
	switch p := peer.(type) {
	case *tg.PeerUser:
		if user, ok := e.users[p.UserID]; ok {
			return userName(user)
		}
	case *tg.PeerChat:
		if chat, ok := e.chats[p.ChatID]; ok {
			return chat.Title
		}
	case *tg.PeerChannel:
		if channel, ok := e.channels[p.ChannelID]; ok {
			return channel.Title
		}
	}
	return ""
}

func userName(user *tg.User) string {
	name := strings.TrimSpace(user.FirstName + " " + user.LastName)
	if name == "" && user.Username != "" {
		return "@" + user.Username
	}
	if name == "" {
		return "Deleted Account"
	}
	return name
}

func (e entities) mapMessage(chat domain.Chat, raw tg.MessageClass, maxMediaSize int64) (domain.Message, bool) {
	// Service and empty messages are other MessageClass types and are skipped.
	msg, ok := raw.(*tg.Message)
	if !ok {
		return domain.Message{}, false
	}

	result := domain.Message{
		ID:       msg.ID,
		ChatID:   chat.ID,
		Date:     time.Unix(int64(msg.Date), 0).UTC(),
		Outgoing: msg.Out,
		Text:     msg.Message,
		Media:    classifyMedia(msg.Media, maxMediaSize),
	}
	if _, ok := msg.GetFwdFrom(); ok {
		result.Forwarded = true
	}
	if editDate, ok := msg.GetEditDate(); ok {
		result.EditedAt = time.Unix(int64(editDate), 0).UTC()
	}
	if reply, ok := msg.ReplyTo.(*tg.MessageReplyHeader); ok {
		result.ReplyToID = reply.ReplyToMsgID
	}

	sender := msg.FromID
	if sender == nil {
		// In private chats and channels the author is not explicit: it is the chat itself.
		sender = msg.PeerID
	}
	if senderID, ok := peerChatID(sender); ok {
		result.SenderID = senderID
	}
	result.Sender = e.peerName(sender)

	return result, true
}
