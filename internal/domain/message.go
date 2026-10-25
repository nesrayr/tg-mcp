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
	MediaKindDOCX     MediaKind = "docx"
	MediaKindXLSX     MediaKind = "xlsx"
	MediaKindDocument MediaKind = "document"
	MediaKindVoice    MediaKind = "voice"
	MediaKindAudio    MediaKind = "audio"
	MediaKindVideo    MediaKind = "video"
	MediaKindSticker  MediaKind = "sticker"
	MediaKindOther    MediaKind = "other"
)

// Media describes an attachment; Downloadable marks ones the server can fetch.
type Media struct {
	Kind         MediaKind
	FileName     string
	MimeType     string
	Size         int64
	Downloadable bool
	Description  string
}

// Attachment is a downloaded attachment together with the message it belongs to.
type Attachment struct {
	ChatID    int64
	MessageID int
	Media     Media
}
