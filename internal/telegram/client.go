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
}

// NewClient creates a gotd client in which every request goes through Guard.
//
// Updates are disabled: the server keeps no persistent event stream and does not
// call updates.getState, so to Telegram it looks like a regular
// inactive client.
func NewClient(opts ClientOptions) *telegram.Client {
	return telegram.NewClient(opts.APIID, opts.APIHash, telegram.Options{
		SessionStorage: &session.FileStorage{Path: opts.SessionFile},
		NoUpdates:      true,
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
