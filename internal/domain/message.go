package domain

import "time"

// Message is a chat message. Text and Sender are untrusted content from Telegram.
type Message struct {
	ID        int
	ChatID    int64
	Date      time.Time
	EditedAt  time.Time
	SenderID  int64
	Sender    string
	Outgoing  bool
	Text      string
	ReplyToID int
	Forwarded bool
	Media     *Media
}

// MediaKind is the attachment type.
type MediaKind string

const (
	MediaKindPhoto    MediaKind = "photo"
	MediaKindImage    MediaKind = "image"
	MediaKindPDF      MediaKind = "pdf"
	MediaKindDocument MediaKind = "document"
	MediaKindVoice    MediaKind = "voice"
	MediaKindAudio    MediaKind = "audio"
	MediaKindVideo    MediaKind = "video"
	MediaKindSticker  MediaKind = "sticker"
	MediaKindOther    MediaKind = "other"
)

// Media describes an attachment. Downloadable means the attachment can be fetched
// through the server: currently only photos, images and PDFs within the size limit.
type Media struct {
	Kind         MediaKind
	FileName     string
	MimeType     string
	Size         int64
	Downloadable bool
}
