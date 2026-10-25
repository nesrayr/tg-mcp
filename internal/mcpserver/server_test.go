package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/nesrayr/tg-mcp/internal/domain"
	"github.com/nesrayr/tg-mcp/internal/mcpserver/mocks"
	"github.com/nesrayr/tg-mcp/internal/telegram"
	"github.com/nesrayr/tg-mcp/internal/textsafe"
)

var testTime = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

type fixture struct {
	reader   *mocks.MockReader
	session  *mcp.ClientSession
	mediaDir string
}

func newFixture(t *testing.T) fixture {
	t.Helper()

	ctx := context.Background()
	reader := mocks.NewMockReader(gomock.NewController(t))
	mediaDir := filepath.Join(t.TempDir(), "media")
	server := New(Options{
		Reader:   reader,
		Media:    NewMediaStore(mediaDir, 7*24*time.Hour, func() time.Time { return testTime }),
		Location: time.UTC,
		Logger:   slog.New(slog.DiscardHandler),
		Version:  "test",
	})

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	return fixture{reader: reader, session: session, mediaDir: mediaDir}
}

func (f fixture) call(t *testing.T, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()

	result, err := f.session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	require.NoError(t, err)
	return result
}

func decodeText[T any](t *testing.T, result *mcp.CallToolResult, index int) T {
	t.Helper()

	text, ok := result.Content[index].(*mcp.TextContent)
	require.True(t, ok)
	var out T
	require.NoError(t, json.Unmarshal([]byte(text.Text), &out))
	return out
}

func errorText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()

	require.True(t, result.IsError)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	return text.Text
}

func Test_ListTools(t *testing.T) {
	t.Parallel()

	f := newFixture(t)

	result, err := f.session.ListTools(context.Background(), nil)
	require.NoError(t, err)

	names := make([]string, 0, len(result.Tools))
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
		require.True(t, tool.Annotations.ReadOnlyHint)
		require.True(t, *tool.Annotations.OpenWorldHint)
	}
	require.ElementsMatch(t, []string{"list_chats", "read_messages", "search_messages", "get_media"}, names)
}

func Test_ListChats(t *testing.T) {
	t.Parallel()

	t.Run("maps and sanitizes chats", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		f.reader.EXPECT().ListChats(gomock.Any(), true).Return([]domain.Chat{
			{ID: -1001, Kind: domain.ChatKindChannel, Title: "Go\u202eNews", Username: "gonews", UnreadCount: 4, LastMessageAt: testTime},
			{ID: 42, Kind: domain.ChatKindPrivate, Title: "Mom"},
		}, nil)

		out := decodeText[ListChatsOutput](t, f.call(t, "list_chats", map[string]any{"unread_only": true}), 0)
		require.Equal(t, untrustedNotice, out.Notice)
		require.Equal(t, []ChatDTO{
			{ID: -1001, Kind: "channel", Title: "GoNews", Username: "gonews", UnreadCount: 4, LastMessageAt: "2026-10-21T12:00:00Z"},
			{ID: 42, Kind: "private", Title: "Mom"},
		}, out.Chats)
		require.Empty(t, out.Hint)
	})

	t.Run("hint when allowlist is empty", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		f.reader.EXPECT().ListChats(gomock.Any(), false).Return([]domain.Chat{}, nil)

		out := decodeText[ListChatsOutput](t, f.call(t, "list_chats", nil), 0)
		require.Empty(t, out.Chats)
		require.Contains(t, out.Hint, "tg-mcp allow")
	})

	t.Run("reader error is sanitized and limited", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		f.reader.EXPECT().ListChats(gomock.Any(), false).Return(nil, errors.New("flood\u202e wait "+strings.Repeat("x", 1000)))

		want := "flood wait " + strings.Repeat("x", maxErrorRunes-len("flood wait ")) + textsafe.TruncatedMark
		require.Equal(t, want, errorText(t, f.call(t, "list_chats", nil)))
	})
}

func Test_ReadMessages(t *testing.T) {
	t.Parallel()

	t.Run("passes params and maps messages", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		f.reader.EXPECT().History(gomock.Any(), domain.ChatRef{Username: "mama"}, telegram.HistoryParams{Limit: 5, BeforeID: 100, AfterID: 10}).
			Return([]domain.Message{
				{ID: 11, ChatID: 42, Date: testTime, SenderID: 42, Sender: "Mom", Text: "Hi\u200b, check out\x07 the file", Forwarded: true},
				{
					ID: 12, ChatID: 42, Date: testTime.Add(time.Minute), EditedAt: testTime.Add(2 * time.Minute), SenderID: 1, Sender: "Me",
					Outgoing: true, Text: "ok", ReplyToID: 11,
					Media: &domain.Media{Kind: domain.MediaKindPDF, FileName: "report\u202e.pdf", MimeType: "application/pdf\u202e", Size: 3, Downloadable: true},
				},
			}, nil)

		out := decodeText[MessagesOutput](t, f.call(t, "read_messages", map[string]any{"chat": "@mama", "limit": 5, "before_id": 100, "after_id": 10}), 0)
		require.Equal(t, MessagesOutput{
			Notice: untrustedNotice,
			Messages: []MessageDTO{
				{ID: 11, ChatID: 42, Date: "2026-10-21T12:00:00Z", From: "Mom", FromID: 42, Forwarded: true, Text: "Hi, check out the file"},
				{
					ID: 12, ChatID: 42, Date: "2026-10-21T12:01:00Z", Edited: true, From: "me (account owner)", FromID: 1,
					Outgoing: true, ReplyTo: 11, Text: "ok",
					Media: &MediaDTO{Kind: "pdf", FileName: "report.pdf", MimeType: "application/pdf", Size: 3, Downloadable: true},
				},
			},
		}, out)
	})

	t.Run("keeps newest messages when response is too large", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		messages := make([]domain.Message, 20)
		for i := range messages {
			messages[i] = domain.Message{ID: i + 1, ChatID: 42, Date: testTime, Text: strings.Repeat("é", maxTextRunes)}
		}
		f.reader.EXPECT().History(gomock.Any(), domain.ChatRef{ID: 42}, telegram.HistoryParams{}).Return(messages, nil)

		out := decodeText[MessagesOutput](t, f.call(t, "read_messages", map[string]any{"chat": "42"}), 0)
		require.True(t, out.Truncated)
		require.Len(t, out.Messages, maxResponseRunes/maxTextRunes)
		require.Equal(t, 6, out.Messages[0].ID)
		require.Equal(t, 20, out.Messages[len(out.Messages)-1].ID)
		require.Contains(t, out.Hint, "before_id=6")
	})

	t.Run("invalid chat reference", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)

		require.Contains(t, errorText(t, f.call(t, "read_messages", map[string]any{"chat": "mom"})), "invalid chat reference")
	})

	t.Run("chat is required", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)

		require.True(t, f.call(t, "read_messages", map[string]any{}).IsError)
	})

	t.Run("chat outside allowlist", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		f.reader.EXPECT().History(gomock.Any(), domain.ChatRef{ID: 777}, telegram.HistoryParams{}).Return(nil, telegram.ErrChatUnavailable)

		require.Contains(t, errorText(t, f.call(t, "read_messages", map[string]any{"chat": "777"})), "not in allowed_chats")
	})
}

func Test_SearchMessages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		args     map[string]any
		params   telegram.SearchParams
		skipped  int
		wantHint string
	}{
		{
			name:   "all allowed chats",
			args:   map[string]any{"query": "report"},
			params: telegram.SearchParams{Query: "report"},
		},
		{
			name: "single chat",
			args: map[string]any{
				"query": "report",
				"chat":  "-1001",
				"limit": 3,
			},
			params: telegram.SearchParams{
				Query: "report",
				Chat:  &domain.ChatRef{ID: -1001},
				Limit: 3,
			},
		},
		{
			name:     "skipped chats are reported",
			args:     map[string]any{"query": "report"},
			params:   telegram.SearchParams{Query: "report"},
			skipped:  7,
			wantHint: "7 allowed chats were not searched",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t)
			f.reader.EXPECT().Search(gomock.Any(), tt.params).Return(telegram.SearchResult{
				Messages: []domain.Message{
					{
						ID:     5,
						ChatID: -1001,
						Date:   testTime,
						Sender: "Go News",
						Text:   "report is ready",
					},
				},
				SkippedChats: tt.skipped,
			}, nil)

			out := decodeText[MessagesOutput](t, f.call(t, "search_messages", tt.args), 0)
			require.Equal(t, []MessageDTO{
				{
					ID:     5,
					ChatID: -1001,
					Date:   "2026-10-21T12:00:00Z",
					From:   "Go News",
					Text:   "report is ready",
				},
			}, out.Messages)
			if tt.wantHint == "" {
				require.Empty(t, out.Hint)
				return
			}
			require.Contains(t, out.Hint, tt.wantHint)
		})
	}
}

func Test_GetMedia(t *testing.T) {
	t.Parallel()

	expectDownload := func(f fixture, ref domain.ChatRef, messageID int, attachment domain.Attachment, data []byte) {
		f.reader.EXPECT().DownloadMedia(gomock.Any(), ref, messageID, gomock.Any()).
			Do(func(_ context.Context, _ domain.ChatRef, _ int, w io.Writer) {
				_, _ = w.Write(data)
			}).
			Return(attachment, nil)
	}

	t.Run("photo is returned inline", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		photo := domain.Media{Kind: domain.MediaKindPhoto, MimeType: "image/jpeg", Size: 4, Downloadable: true}
		expectDownload(f, domain.ChatRef{ID: 42}, 12, domain.Attachment{ChatID: 42, MessageID: 12, Media: photo}, []byte("jpeg"))

		result := f.call(t, "get_media", map[string]any{"chat": "42", "message_id": 12})
		require.False(t, result.IsError)
		require.Len(t, result.Content, 2)

		image, ok := result.Content[0].(*mcp.ImageContent)
		require.True(t, ok)
		require.Equal(t, "image/jpeg", image.MIMEType)
		require.Equal(t, []byte("jpeg"), image.Data)

		out := decodeText[MediaOutput](t, result, 1)
		require.True(t, out.Inline)
		require.Empty(t, out.Path)
		require.NoDirExists(t, f.mediaDir)
	})

	t.Run("pdf is saved to private file", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		pdf := domain.Media{Kind: domain.MediaKindPDF, FileName: "../../etc/passwd.pdf", MimeType: "application/pdf", Size: 3, Downloadable: true}
		expectDownload(f, domain.ChatRef{Username: "gonews"}, 7, domain.Attachment{ChatID: -1001, MessageID: 7, Media: pdf}, []byte("pdf"))

		result := f.call(t, "get_media", map[string]any{"chat": "@gonews", "message_id": 7})
		require.False(t, result.IsError)

		out := decodeText[MediaOutput](t, result, 0)
		require.Equal(t, filepath.Join(f.mediaDir, "-1001_7.pdf"), out.Path)
		require.Equal(t, int64(-1001), out.ChatID)
		require.Equal(t, 3, out.Size)

		data, err := os.ReadFile(out.Path)
		require.NoError(t, err)
		require.Equal(t, []byte("pdf"), data)

		fileInfo, err := os.Stat(out.Path)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), fileInfo.Mode().Perm())

		dirInfo, err := os.Stat(f.mediaDir)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm())
	})

	t.Run("large image goes to file", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		image := domain.Media{Kind: domain.MediaKindImage, MimeType: "image/png", Size: maxInlineImage + 1, Downloadable: true}
		expectDownload(f, domain.ChatRef{ID: 42}, 3, domain.Attachment{ChatID: 42, MessageID: 3, Media: image}, make([]byte, maxInlineImage+1))

		out := decodeText[MediaOutput](t, f.call(t, "get_media", map[string]any{"chat": "42", "message_id": 3}), 0)
		require.False(t, out.Inline)
		require.Equal(t, filepath.Join(f.mediaDir, "42_3.png"), out.Path)
	})

	t.Run("pdf sent as octet-stream keeps pdf extension", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		pdf := domain.Media{Kind: domain.MediaKindPDF, FileName: "scan.pdf", MimeType: "application/octet-stream", Size: 3, Downloadable: true}
		expectDownload(f, domain.ChatRef{ID: 42}, 8, domain.Attachment{ChatID: 42, MessageID: 8, Media: pdf}, []byte("pdf"))

		out := decodeText[MediaOutput](t, f.call(t, "get_media", map[string]any{"chat": "42", "message_id": 8}), 0)
		require.Equal(t, filepath.Join(f.mediaDir, "42_8.pdf"), out.Path)
	})

	t.Run("docx text is extracted and sanitized", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		document := buildDocx(t, `<w:p><w:r><w:t>Classes on Tuesday</w:t></w:r></w:p>`+
			`<w:tbl><w:tr><w:tc><w:p><w:r><w:t>16:30</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>Smith`+"\u202e"+`</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`)
		media := domain.Media{Kind: domain.MediaKindDOCX, FileName: "Classes.docx", Size: int64(len(document)), Downloadable: true}
		expectDownload(f, domain.ChatRef{ID: 42}, 4, domain.Attachment{ChatID: 42, MessageID: 4, Media: media}, document)

		out := decodeText[DocumentOutput](t, f.call(t, "get_media", map[string]any{"chat": "42", "message_id": 4}), 0)
		require.Equal(t, "Classes on Tuesday\n| 16:30 | Smith |", out.Text)
		require.False(t, out.Truncated)
		require.Empty(t, out.Path)
		require.Equal(t, untrustedNotice, out.Notice)
		require.NoDirExists(t, f.mediaDir)
	})

	t.Run("xlsx text is extracted", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		book := buildXlsx(t, `<row r="1"><c r="A1" t="inlineStr"><is><t>Fri</t></is></c><c r="B1" t="inlineStr"><is><t>Philosophy</t></is></c></row>`)
		media := domain.Media{Kind: domain.MediaKindXLSX, FileName: "Classes.xlsx", Size: int64(len(book)), Downloadable: true}
		expectDownload(f, domain.ChatRef{ID: 42}, 5, domain.Attachment{ChatID: 42, MessageID: 5, Media: media}, book)

		out := decodeText[DocumentOutput](t, f.call(t, "get_media", map[string]any{"chat": "42", "message_id": 5}), 0)
		require.Equal(t, "## Sheet: Sheet1\n1 | Fri | Philosophy |", out.Text)
		require.Contains(t, out.Hint, "merged cell")
	})

	t.Run("broken docx", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		media := domain.Media{Kind: domain.MediaKindDOCX, FileName: "broken.docx", Size: 4, Downloadable: true}
		expectDownload(f, domain.ChatRef{ID: 42}, 6, domain.Attachment{ChatID: 42, MessageID: 6, Media: media}, []byte("nope"))

		require.Contains(t, errorText(t, f.call(t, "get_media", map[string]any{"chat": "42", "message_id": 6})), "invalid office document")
	})

	t.Run("download error", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		f.reader.EXPECT().DownloadMedia(gomock.Any(), domain.ChatRef{ID: 42}, 9, gomock.Any()).
			Return(domain.Attachment{ChatID: 42, MessageID: 9}, telegram.ErrMediaNotDownloadable)

		require.Contains(t, errorText(t, f.call(t, "get_media", map[string]any{"chat": "42", "message_id": 9})), "not downloadable")
	})
}
