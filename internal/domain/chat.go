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

// Chat is a chat available for reading, with ID in the TDLib/Bot API format.
type Chat struct {
	ID            int64
	Kind          ChatKind
	Title         string
	Username      string
	UnreadCount   int
	LastMessageAt time.Time
}

// ChatRef references a chat by ID, by @username, or by @username pinned to the ID it had when allowed.
type ChatRef struct {
	ID       int64
	Username string
}

var ErrInvalidChatRef = errors.New("invalid chat reference")

var usernamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{3,31}$`)

// ParseChatRef parses a chat reference: "123", "-100123", "@username" or "@username:-100123".
func ParseChatRef(raw string) (ChatRef, error) {
	value := strings.TrimSpace(raw)
	if username, ok := strings.CutPrefix(value, "@"); ok {
		name, pin, pinned := strings.Cut(username, ":")
		if !usernamePattern.MatchString(name) {
			return ChatRef{}, fmt.Errorf("%w: %q", ErrInvalidChatRef, raw)
		}
		// Telegram usernames are case-insensitive.
		ref := ChatRef{Username: strings.ToLower(name)}
		if pinned {
			id, err := parseID(pin)
			if err != nil {
				return ChatRef{}, fmt.Errorf("%w: %q", ErrInvalidChatRef, raw)
			}
			ref.ID = id
		}
		return ref, nil
	}

	id, err := parseID(value)
	if err != nil {
		return ChatRef{}, fmt.Errorf("%w: %q", ErrInvalidChatRef, raw)
	}
	return ChatRef{ID: id}, nil
}

func parseID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, err
	}
	if id == 0 {
		return 0, ErrInvalidChatRef
	}
	return id, nil
}

func (r ChatRef) String() string {
	id := strconv.FormatInt(r.ID, 10)
	switch {
	case r.Username != "" && r.ID != 0:
		return "@" + r.Username + ":" + id
	case r.Username != "":
		return "@" + r.Username
	default:
		return id
	}
}

// Pinned reports whether the reference is an @username pinned to a chat ID.
func (r ChatRef) Pinned() bool {
	return r.Username != "" && r.ID != 0
}

// Matches reports whether the reference points to the given chat.
func (r ChatRef) Matches(chat Chat) bool {
	if r.Username != "" && !strings.EqualFold(r.Username, chat.Username) {
		return false
	}
	return r.ID == 0 || r.ID == chat.ID
}
