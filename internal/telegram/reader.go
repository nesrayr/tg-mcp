package telegram

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/tg"

	"github.com/nesrayr/tg-mcp/internal/domain"
)

const (
	MaxMessagesLimit = 100
	// MaxSearchChats limits search without a chat: it costs one request per chat.
	MaxSearchChats       = 50
	peerDialogsBatchSize = 100
)

var (
	ErrMessageNotFound      = errors.New("message not found")
	ErrMediaNotDownloadable = errors.New("media is not downloadable")
	ErrMediaTooLarge        = errors.New("media exceeds size limit")
)

// ReaderOptions are the Reader limits.
type ReaderOptions struct {
	MaxMediaSize int64
	// DownloadTimeout bounds the whole download. Without it the gotd downloader
	// retries file parts forever on timeouts and FLOOD_WAIT.
	DownloadTimeout time.Duration
}

// Reader provides read operations on top of Directory. It works only with allowed chats.
type Reader struct {
	api       *tg.Client
	directory *Directory
	opts      ReaderOptions
}

func NewReader(api *tg.Client, directory *Directory, opts ReaderOptions) *Reader {
	return &Reader{
		api:       api,
		directory: directory,
		opts:      opts,
	}
}

// ListChats returns the allowed chats with unread counters, most recent first.
func (r *Reader) ListChats(ctx context.Context, unreadOnly bool) ([]domain.Chat, error) {
	allowed, err := r.directory.Allowed(ctx)
	if err != nil {
		return nil, err
	}
	if len(allowed) == 0 {
		return []domain.Chat{}, nil
	}

	byID := make(map[int64]domain.Chat, len(allowed))
	for _, chat := range allowed {
		byID[chat.chat.ID] = chat.chat
	}

	for batch := range slices.Chunk(allowed, peerDialogsBatchSize) {
		peers := make([]tg.InputDialogPeerClass, 0, len(batch))
		for _, chat := range batch {
			peers = append(peers, &tg.InputDialogPeer{Peer: chat.peer})
		}
		result, err := r.api.MessagesGetPeerDialogs(ctx, peers)
		if err != nil {
			return nil, fmt.Errorf("get peer dialogs: %w", err)
		}
		applyDialogs(byID, result)
	}

	chats := make([]domain.Chat, 0, len(byID))
	for _, chat := range byID {
		if unreadOnly && chat.UnreadCount == 0 {
			continue
		}
		chats = append(chats, chat)
	}
	slices.SortFunc(chats, func(a, b domain.Chat) int {
		return cmp.Or(b.LastMessageAt.Compare(a.LastMessageAt), cmp.Compare(a.ID, b.ID))
	})
	return chats, nil
}

func applyDialogs(byID map[int64]domain.Chat, result *tg.MessagesPeerDialogs) {
	lastDates := make(map[int64]map[int]time.Time)
	for _, raw := range result.Messages {
		// The top message can be a service one (pinned message, member joined).
		msg, ok := raw.AsNotEmpty()
		if !ok {
			continue
		}
		chatID, ok := peerChatID(msg.GetPeerID())
		if !ok {
			continue
		}
		if lastDates[chatID] == nil {
			lastDates[chatID] = make(map[int]time.Time)
		}
		lastDates[chatID][msg.GetID()] = time.Unix(int64(msg.GetDate()), 0).UTC()
	}

	for _, raw := range result.Dialogs {
		dialog, ok := raw.(*tg.Dialog)
		if !ok {
			continue
		}
		chatID, ok := peerChatID(dialog.Peer)
		if !ok {
			continue
		}
		chat, ok := byID[chatID]
		if !ok {
			continue
		}
		chat.UnreadCount = dialog.UnreadCount
		chat.LastMessageAt = lastDates[chatID][dialog.TopMessage]
		byID[chatID] = chat
	}
}

// HistoryParams are the parameters for reading history.
type HistoryParams struct {
	Limit    int
	BeforeID int
	AfterID  int
}

// History returns chat messages in chronological order.
func (r *Reader) History(ctx context.Context, ref domain.ChatRef, params HistoryParams) ([]domain.Message, error) {
	chat, err := r.directory.Resolve(ctx, ref)
	if err != nil {
		return nil, err
	}

	request := &tg.MessagesGetHistoryRequest{
		Peer:     chat.peer,
		OffsetID: params.BeforeID,
		MinID:    params.AfterID,
		Limit:    clampLimit(params.Limit),
	}
	if params.AfterID > 0 && params.BeforeID == 0 {
		// Without offset_id Telegram returns the newest messages above min_id and skips
		// the ones right after it, so paging forward shifts the window with add_offset.
		request.OffsetID = params.AfterID
		request.AddOffset = -request.Limit
	}
	result, err := r.api.MessagesGetHistory(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("get history: %w", err)
	}

	messages := r.mapMessages(chat.chat, result)
	slices.Reverse(messages)
	return messages, nil
}

// SearchParams are the search parameters. An empty Chat searches all allowed chats.
type SearchParams struct {
	Query string
	Chat  *domain.ChatRef
	Limit int
}

// SearchResult holds the matches and the number of allowed chats left out by MaxSearchChats.
type SearchResult struct {
	Messages     []domain.Message
	SkippedChats int
}

// Search finds messages and returns the most recent matches first.
func (r *Reader) Search(ctx context.Context, params SearchParams) (SearchResult, error) {
	var (
		targets []resolvedChat
		skipped int
	)
	if params.Chat != nil {
		chat, err := r.directory.Resolve(ctx, *params.Chat)
		if err != nil {
			return SearchResult{}, err
		}
		targets = []resolvedChat{chat}
	} else {
		// Each allowed chat is searched separately instead of the global search,
		// so the request never touches chats outside the allowlist.
		allowed, err := r.directory.Allowed(ctx)
		if err != nil {
			return SearchResult{}, err
		}
		targets = allowed[:min(len(allowed), MaxSearchChats)]
		skipped = len(allowed) - len(targets)
	}

	limit := clampLimit(params.Limit)
	var found []domain.Message
	for _, chat := range targets {
		result, err := r.api.MessagesSearch(ctx, &tg.MessagesSearchRequest{
			Peer:   chat.peer,
			Q:      params.Query,
			Filter: &tg.InputMessagesFilterEmpty{},
			Limit:  limit,
		})
		if err != nil {
			return SearchResult{}, fmt.Errorf("search messages: %w", err)
		}
		found = append(found, r.mapMessages(chat.chat, result)...)
	}

	slices.SortFunc(found, func(a, b domain.Message) int {
		return cmp.Or(b.Date.Compare(a.Date), cmp.Compare(b.ID, a.ID))
	})
	return SearchResult{
		Messages:     found[:min(len(found), limit)],
		SkippedChats: skipped,
	}, nil
}

// DownloadMedia writes a message attachment to w, failing with ErrMediaTooLarge over MaxMediaSize.
func (r *Reader) DownloadMedia(ctx context.Context, ref domain.ChatRef, messageID int, w io.Writer) (domain.Attachment, error) {
	chat, err := r.directory.Resolve(ctx, ref)
	if err != nil {
		return domain.Attachment{}, err
	}
	attachment := domain.Attachment{
		ChatID:    chat.chat.ID,
		MessageID: messageID,
	}

	msg, err := r.message(ctx, chat, messageID)
	if err != nil {
		return attachment, err
	}

	media := classifyMedia(msg.Media, r.opts.MaxMediaSize)
	if media == nil {
		return attachment, fmt.Errorf("%w: message has no media", ErrMediaNotDownloadable)
	}
	attachment.Media = *media
	if !media.Downloadable {
		if isDownloadableKind(media.Kind) && media.Size > r.opts.MaxMediaSize {
			return attachment, fmt.Errorf("%w: %d bytes, limit %d", ErrMediaTooLarge, media.Size, r.opts.MaxMediaSize)
		}
		return attachment, fmt.Errorf("%w: %s", ErrMediaNotDownloadable, media.Kind)
	}

	location, ok := fileLocation(msg.Media)
	if !ok {
		return attachment, fmt.Errorf("%w: no file location", ErrMediaNotDownloadable)
	}

	ctx, cancel := context.WithTimeout(ctx, r.opts.DownloadTimeout)
	defer cancel()

	limited := &limitedWriter{
		w:         w,
		remaining: r.opts.MaxMediaSize,
	}
	if _, err := downloader.NewDownloader().Download(r.api, location).WithVerify(false).Stream(ctx, limited); err != nil {
		return attachment, fmt.Errorf("download media: %w", err)
	}
	return attachment, nil
}

func (r *Reader) message(ctx context.Context, chat resolvedChat, id int) (*tg.Message, error) {
	ids := []tg.InputMessageClass{&tg.InputMessageID{ID: id}}

	var (
		result tg.MessagesMessagesClass
		err    error
	)
	if channel, ok := chat.peer.(*tg.InputPeerChannel); ok {
		result, err = r.api.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{
			Channel: &tg.InputChannel{
				ChannelID:  channel.ChannelID,
				AccessHash: channel.AccessHash,
			},
			ID: ids,
		})
	} else {
		result, err = r.api.MessagesGetMessages(ctx, ids)
	}
	if err != nil {
		return nil, fmt.Errorf("get message: %w", err)
	}

	modified, ok := result.AsModified()
	if !ok {
		return nil, ErrMessageNotFound
	}
	for _, raw := range modified.GetMessages() {
		msg, ok := raw.(*tg.Message)
		if !ok || msg.ID != id {
			continue
		}
		// messages.getMessages searches all private chats and groups at once,
		// so check that the message really comes from the requested chat.
		if chatID, ok := peerChatID(msg.PeerID); !ok || chatID != chat.chat.ID {
			continue
		}
		return msg, nil
	}
	return nil, fmt.Errorf("%w: %d", ErrMessageNotFound, id)
}

func (r *Reader) mapMessages(chat domain.Chat, result tg.MessagesMessagesClass) []domain.Message {
	modified, ok := result.AsModified()
	if !ok {
		return nil
	}

	ent := newEntities(modified.GetUsers(), modified.GetChats())
	messages := make([]domain.Message, 0, len(modified.GetMessages()))
	for _, raw := range modified.GetMessages() {
		if msg, ok := ent.mapMessage(chat, raw, r.opts.MaxMediaSize); ok {
			messages = append(messages, msg)
		}
	}
	return messages
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	return min(limit, MaxMessagesLimit)
}

type limitedWriter struct {
	w         io.Writer
	remaining int64
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > l.remaining {
		return 0, ErrMediaTooLarge
	}
	n, err := l.w.Write(p)
	l.remaining -= int64(n)
	return n, err
}
