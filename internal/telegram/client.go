// Package telegram is a read-only Telegram access layer on top of gotd.
package telegram

import (
	"runtime"
	"time"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
)

const (
	requestTimeout = 30 * time.Second
	maxFloodWait   = 30 * time.Second
	maxFloodRetry  = 2
)

// ClientOptions are the MTProto client parameters.
type ClientOptions struct {
	APIID       int
	APIHash     string
	SessionFile string
	Policy      Policy
	AppVersion  string
	// UpdateHandler is needed only for QR login: Telegram reports the code scan
	// with an updateLoginToken update. If nil, updates are disabled.
	UpdateHandler telegram.UpdateHandler
}

// NewClient creates a gotd client in which every request goes through Guard.
//
// Without an UpdateHandler updates are disabled: the client keeps no event stream and does not
// call updates.getState, so to Telegram it looks inactive.
func NewClient(opts ClientOptions) *telegram.Client {
	return telegram.NewClient(opts.APIID, opts.APIHash, telegram.Options{
		SessionStorage: &session.FileStorage{Path: opts.SessionFile},
		NoUpdates:      opts.UpdateHandler == nil,
		UpdateHandler:  opts.UpdateHandler,
		Middlewares: []telegram.Middleware{
			Guard(opts.Policy),
			FloodWait(maxFloodWait, maxFloodRetry, SleepContext),
			Timeout(requestTimeout),
		},
		Device: telegram.DeviceConfig{
			DeviceModel:    "tg-mcp",
			SystemVersion:  runtime.GOOS,
			AppVersion:     opts.AppVersion,
			SystemLangCode: "ru",
			LangCode:       "ru",
		},
	})
}
