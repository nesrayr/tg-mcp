package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ChatKind is the chat type from the user's point of view.
type ChatKind string

const (
	ChatKindPrivate ChatKind = "private"
	ChatKindBot     ChatKind = "bot"
	ChatKindGroup   ChatKind = "group"
	ChatKindChannel ChatKind = "channel"
)

// Chat is a chat available for reading.
//
// ID uses the TDLib/Bot API format: users are positive,
// basic groups are negative, channels and supergroups have the -100 prefix.
type Chat struct {
	ID            int64
	Kind          ChatKind
	Title         string
	Username      string
	UnreadCount   int
	LastMessageAt time.Time
}

// ChatRef references a chat: a numeric ID or an @username (exactly one of the fields).
type ChatRef struct {
	ID       int64
	Username string
}

var ErrInvalidChatRef = errors.New("invalid chat reference")

var usernamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{3,31}$`)

// ParseChatRef parses a chat reference: "123", "-100123" or "@username".
// The username is lowercased because Telegram usernames are case-insensitive.
func ParseChatRef(raw string) (ChatRef, error) {
	value := strings.TrimSpace(raw)
	if username, ok := strings.CutPrefix(value, "@"); ok {
		if !usernamePattern.MatchString(username) {
			return ChatRef{}, fmt.Errorf("%w: %q", ErrInvalidChatRef, raw)
		}
		return ChatRef{Username: strings.ToLower(username)}, nil
	}

	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id == 0 {
		return ChatRef{}, fmt.Errorf("%w: %q", ErrInvalidChatRef, raw)
	}
	return ChatRef{ID: id}, nil
}

func (r ChatRef) String() string {
	if r.Username != "" {
		return "@" + r.Username
	}
	return strconv.FormatInt(r.ID, 10)
}

// Matches reports whether the reference points to the given chat.
func (r ChatRef) Matches(chat Chat) bool {
	if r.Username != "" {
		return strings.EqualFold(r.Username, chat.Username)
	}
	return r.ID == chat.ID
}
