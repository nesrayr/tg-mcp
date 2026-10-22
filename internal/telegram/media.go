package telegram

import (
	"path"
	"strings"

	"github.com/gotd/td/tg"

	"github.com/nesrayr/tg-mcp/internal/domain"
)

// maxPhotoSide is the side used to choose a photo size: the model does not need anything larger,
// and extra pixels cost tokens.
const maxPhotoSide = 1280

// Images the model can process. HEIC and similar formats are not supported.
var supportedImageMIME = map[string]struct{}{
	"image/jpeg": {},
	"image/png":  {},
	"image/webp": {},
	"image/gif":  {},
}

// classifyMedia describes an attachment. Uninteresting attachments (link previews) are nil.
func classifyMedia(raw tg.MessageMediaClass, maxSize int64) *domain.Media {
	switch media := raw.(type) {
	case nil, *tg.MessageMediaEmpty, *tg.MessageMediaWebPage:
		return nil
	case *tg.MessageMediaPhoto:
		photo, ok := media.Photo.(*tg.Photo)
		if !ok {
			return &domain.Media{Kind: domain.MediaKindPhoto, MimeType: "image/jpeg"}
		}
		size, _ := choosePhotoSize(photo)
		return &domain.Media{
			Kind:         domain.MediaKindPhoto,
			MimeType:     "image/jpeg",
			Size:         size.bytes,
			Downloadable: size.typ != "" && size.bytes <= maxSize,
		}
	case *tg.MessageMediaDocument:
		document, ok := media.Document.(*tg.Document)
		if !ok {
			return &domain.Media{Kind: domain.MediaKindDocument}
		}
		return classifyDocument(document, maxSize)
	default:
		return &domain.Media{
			Kind:        domain.MediaKindOther,
			Description: strings.TrimPrefix(raw.TypeName(), "messageMedia"),
		}
	}
}

func classifyDocument(document *tg.Document, maxSize int64) *domain.Media {
	result := &domain.Media{
		Kind:     domain.MediaKindDocument,
		MimeType: document.MimeType,
		Size:     document.Size,
	}

	isSticker := false
	for _, attr := range document.Attributes {
		switch a := attr.(type) {
		case *tg.DocumentAttributeFilename:
			result.FileName = a.FileName
		case *tg.DocumentAttributeSticker:
			isSticker = true
		case *tg.DocumentAttributeAudio:
			result.Kind = domain.MediaKindAudio
			if a.Voice {
				result.Kind = domain.MediaKindVoice
			}
		case *tg.DocumentAttributeVideo, *tg.DocumentAttributeAnimated:
			result.Kind = domain.MediaKindVideo
		}
	}

	if isSticker {
		result.Kind = domain.MediaKindSticker
	}
	if result.Kind == domain.MediaKindDocument {
		result.Kind = documentKind(document.MimeType, result.FileName)
	}

	result.Downloadable = isDownloadableKind(result.Kind) && document.Size <= maxSize
	return result
}

// Some clients send files as application/octet-stream, so
// for these MIME types the kind is determined by the file name extension.
var genericMIME = map[string]struct{}{
	"":                         {},
	"application/octet-stream": {},
	"application/zip":          {},
}

func documentKind(mime, fileName string) domain.MediaKind {
	switch {
	case mime == "application/pdf":
		return domain.MediaKindPDF
	case mime == "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return domain.MediaKindDOCX
	case mime == "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return domain.MediaKindXLSX
	case isSupportedImage(mime):
		return domain.MediaKindImage
	}

	if _, generic := genericMIME[mime]; !generic {
		return domain.MediaKindDocument
	}
	switch strings.ToLower(path.Ext(fileName)) {
	case ".pdf":
		return domain.MediaKindPDF
	case ".docx":
		return domain.MediaKindDOCX
	case ".xlsx":
		return domain.MediaKindXLSX
	default:
		return domain.MediaKindDocument
	}
}

// isDownloadableKind reports the attachment kinds the model can process.
func isDownloadableKind(kind domain.MediaKind) bool {
	switch kind {
	case domain.MediaKindPhoto, domain.MediaKindImage, domain.MediaKindPDF, domain.MediaKindDOCX, domain.MediaKindXLSX:
		return true
	default:
		return false
	}
}

func isSupportedImage(mime string) bool {
	_, ok := supportedImageMIME[mime]
	return ok
}

type photoSize struct {
	typ   string
	bytes int64
}

// choosePhotoSize picks the largest size whose side is at most maxPhotoSide,
// or, if there is none, the smallest one available.
func choosePhotoSize(photo *tg.Photo) (photoSize, bool) {
	var best, smallest photoSize
	bestSide, smallestSide := 0, 0

	for _, raw := range photo.Sizes {
		var (
			size photoSize
			side int
		)
		switch s := raw.(type) {
		case *tg.PhotoSize:
			size, side = photoSize{typ: s.Type, bytes: int64(s.Size)}, max(s.W, s.H)
		case *tg.PhotoSizeProgressive:
			if len(s.Sizes) == 0 {
				continue
			}
			size, side = photoSize{typ: s.Type, bytes: int64(s.Sizes[len(s.Sizes)-1])}, max(s.W, s.H)
		default:
			continue
		}

		if side <= maxPhotoSide && side > bestSide {
			best, bestSide = size, side
		}
		if smallestSide == 0 || side < smallestSide {
			smallest, smallestSide = size, side
		}
	}

	if bestSide > 0 {
		return best, true
	}
	return smallest, smallestSide > 0
}

// fileLocation builds the location of a file to download.
func fileLocation(raw tg.MessageMediaClass) (tg.InputFileLocationClass, bool) {
	switch media := raw.(type) {
	case *tg.MessageMediaPhoto:
		photo, ok := media.Photo.(*tg.Photo)
		if !ok {
			return nil, false
		}
		size, ok := choosePhotoSize(photo)
		if !ok {
			return nil, false
		}
		return &tg.InputPhotoFileLocation{
			ID:            photo.ID,
			AccessHash:    photo.AccessHash,
			FileReference: photo.FileReference,
			ThumbSize:     size.typ,
		}, true
	case *tg.MessageMediaDocument:
		document, ok := media.Document.(*tg.Document)
		if !ok {
			return nil, false
		}
		return document.AsInputDocumentFileLocation(""), true
	default:
		return nil, false
	}
}
