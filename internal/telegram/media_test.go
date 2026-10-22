package telegram

import (
	"testing"

	"github.com/gotd/td/tg"
	"github.com/stretchr/testify/require"

	"github.com/nesrayr/tg-mcp/internal/domain"
)

func Test_classifyMedia(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		media tg.MessageMediaClass
		want  *domain.Media
	}{
		{name: "no media", media: nil, want: nil},
		{name: "link preview is ignored", media: &tg.MessageMediaWebPage{Webpage: &tg.WebPageEmpty{}}, want: nil},
		{
			name:  "photo",
			media: &tg.MessageMediaPhoto{Photo: testPhoto()},
			want:  &domain.Media{Kind: domain.MediaKindPhoto, MimeType: "image/jpeg", Size: 500, Downloadable: true},
		},
		{
			name:  "deleted photo",
			media: &tg.MessageMediaPhoto{Photo: &tg.PhotoEmpty{ID: 1}},
			want:  &domain.Media{Kind: domain.MediaKindPhoto, MimeType: "image/jpeg"},
		},
		{
			name: "pdf",
			media: &tg.MessageMediaDocument{Document: testDocument("application/pdf", 900,
				&tg.DocumentAttributeFilename{FileName: "report.pdf"})},
			want: &domain.Media{Kind: domain.MediaKindPDF, FileName: "report.pdf", MimeType: "application/pdf", Size: 900, Downloadable: true},
		},
		{
			name:  "pdf over limit",
			media: &tg.MessageMediaDocument{Document: testDocument("application/pdf", 5000)},
			want:  &domain.Media{Kind: domain.MediaKindPDF, MimeType: "application/pdf", Size: 5000},
		},
		{
			name:  "png sent as file",
			media: &tg.MessageMediaDocument{Document: testDocument("image/png", 10, &tg.DocumentAttributeFilename{FileName: "scheme.png"})},
			want:  &domain.Media{Kind: domain.MediaKindImage, FileName: "scheme.png", MimeType: "image/png", Size: 10, Downloadable: true},
		},
		{
			name:  "heic is not supported",
			media: &tg.MessageMediaDocument{Document: testDocument("image/heic", 10)},
			want:  &domain.Media{Kind: domain.MediaKindDocument, MimeType: "image/heic", Size: 10},
		},
		{
			name:  "webp sticker",
			media: &tg.MessageMediaDocument{Document: testDocument("image/webp", 10, &tg.DocumentAttributeSticker{Stickerset: &tg.InputStickerSetEmpty{}})},
			want:  &domain.Media{Kind: domain.MediaKindSticker, MimeType: "image/webp", Size: 10},
		},
		{
			name:  "voice message",
			media: &tg.MessageMediaDocument{Document: testDocument("audio/ogg", 10, &tg.DocumentAttributeAudio{Voice: true, Duration: 3})},
			want:  &domain.Media{Kind: domain.MediaKindVoice, MimeType: "audio/ogg", Size: 10},
		},
		{
			name:  "music",
			media: &tg.MessageMediaDocument{Document: testDocument("audio/mpeg", 10, &tg.DocumentAttributeAudio{Duration: 180})},
			want:  &domain.Media{Kind: domain.MediaKindAudio, MimeType: "audio/mpeg", Size: 10},
		},
		{
			name:  "video",
			media: &tg.MessageMediaDocument{Document: testDocument("video/mp4", 10, &tg.DocumentAttributeVideo{W: 1, H: 1})},
			want:  &domain.Media{Kind: domain.MediaKindVideo, MimeType: "video/mp4", Size: 10},
		},
		{
			name: "docx",
			media: &tg.MessageMediaDocument{Document: testDocument("application/vnd.openxmlformats-officedocument.wordprocessingml.document", 10,
				&tg.DocumentAttributeFilename{FileName: "Schedule.docx"})},
			want: &domain.Media{Kind: domain.MediaKindDOCX, FileName: "Schedule.docx", MimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", Size: 10, Downloadable: true},
		},
		{
			name: "xlsx",
			media: &tg.MessageMediaDocument{Document: testDocument("application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", 10,
				&tg.DocumentAttributeFilename{FileName: "group.xlsx"})},
			want: &domain.Media{Kind: domain.MediaKindXLSX, FileName: "group.xlsx", MimeType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", Size: 10, Downloadable: true},
		},
		{
			name:  "xlsx sent as octet-stream",
			media: &tg.MessageMediaDocument{Document: testDocument("application/octet-stream", 10, &tg.DocumentAttributeFilename{FileName: "Classes.XLSX"})},
			want:  &domain.Media{Kind: domain.MediaKindXLSX, FileName: "Classes.XLSX", MimeType: "application/octet-stream", Size: 10, Downloadable: true},
		},
		{
			name:  "pdf sent as octet-stream",
			media: &tg.MessageMediaDocument{Document: testDocument("application/octet-stream", 10, &tg.DocumentAttributeFilename{FileName: "scan.pdf"})},
			want:  &domain.Media{Kind: domain.MediaKindPDF, FileName: "scan.pdf", MimeType: "application/octet-stream", Size: 10, Downloadable: true},
		},
		{
			name:  "extension does not override specific mime",
			media: &tg.MessageMediaDocument{Document: testDocument("text/plain", 10, &tg.DocumentAttributeFilename{FileName: "fake.docx"})},
			want:  &domain.Media{Kind: domain.MediaKindDocument, FileName: "fake.docx", MimeType: "text/plain", Size: 10},
		},
		{
			name:  "legacy doc is not supported",
			media: &tg.MessageMediaDocument{Document: testDocument("application/msword", 10, &tg.DocumentAttributeFilename{FileName: "old.doc"})},
			want:  &domain.Media{Kind: domain.MediaKindDocument, FileName: "old.doc", MimeType: "application/msword", Size: 10},
		},
		{
			name:  "zip archive",
			media: &tg.MessageMediaDocument{Document: testDocument("application/zip", 10, &tg.DocumentAttributeFilename{FileName: "a.zip"})},
			want:  &domain.Media{Kind: domain.MediaKindDocument, FileName: "a.zip", MimeType: "application/zip", Size: 10},
		},
		{
			name:  "geo point",
			media: &tg.MessageMediaGeo{Geo: &tg.GeoPointEmpty{}},
			want:  &domain.Media{Kind: domain.MediaKindOther, Description: "Geo"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, classifyMedia(tt.media, testMaxMedia))
		})
	}
}

func Test_choosePhotoSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		sizes  []tg.PhotoSizeClass
		want   photoSize
		wantOK bool
	}{
		{
			name:   "largest within limit",
			sizes:  testPhoto().Sizes,
			want:   photoSize{typ: "y", bytes: 500},
			wantOK: true,
		},
		{
			name: "progressive size uses final byte count",
			sizes: []tg.PhotoSizeClass{
				&tg.PhotoStrippedSize{Type: "i", Bytes: []byte{1}},
				&tg.PhotoSizeProgressive{Type: "x", W: 800, H: 600, Sizes: []int{10, 50, 120}},
			},
			want:   photoSize{typ: "x", bytes: 120},
			wantOK: true,
		},
		{
			name: "smallest when all are too big",
			sizes: []tg.PhotoSizeClass{
				&tg.PhotoSize{Type: "w", W: 2560, H: 1920, Size: 2000},
				&tg.PhotoSize{Type: "z", W: 1600, H: 1600, Size: 1500},
			},
			want:   photoSize{typ: "z", bytes: 1500},
			wantOK: true,
		},
		{
			name:  "only stripped thumbnail",
			sizes: []tg.PhotoSizeClass{&tg.PhotoStrippedSize{Type: "i", Bytes: []byte{1}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := choosePhotoSize(&tg.Photo{Sizes: tt.sizes})
			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func Test_fileLocation(t *testing.T) {
	t.Parallel()

	t.Run("photo", func(t *testing.T) {
		t.Parallel()

		got, ok := fileLocation(&tg.MessageMediaPhoto{Photo: testPhoto()})
		require.True(t, ok)
		require.Equal(t, &tg.InputPhotoFileLocation{ID: 900, AccessHash: 901, FileReference: []byte{9, 9}, ThumbSize: "y"}, got)
	})

	t.Run("document", func(t *testing.T) {
		t.Parallel()

		got, ok := fileLocation(&tg.MessageMediaDocument{Document: testDocument("application/pdf", 1)})
		require.True(t, ok)
		require.Equal(t, &tg.InputDocumentFileLocation{ID: 800, AccessHash: 801, FileReference: []byte{8}}, got)
	})

	t.Run("unsupported media", func(t *testing.T) {
		t.Parallel()

		_, ok := fileLocation(&tg.MessageMediaGeo{Geo: &tg.GeoPointEmpty{}})
		require.False(t, ok)
	})
}
