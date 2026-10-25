package telegram

import (
	"context"
	"time"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

// WaitFunc waits for the given duration or until the context is canceled.
type WaitFunc func(ctx context.Context, d time.Duration) error

// SleepContext is a WaitFunc backed by a real timer.
func SleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// FloodWait retries a request after FLOOD_WAIT if Telegram asks to wait no longer than maxWait.
func FloodWait(maxWait time.Duration, maxRetries int, wait WaitFunc) telegram.Middleware {
	return telegram.MiddlewareFunc(func(next tg.Invoker) telegram.InvokeFunc {
		return func(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
			for attempt := 0; ; attempt++ {
				err := next.Invoke(ctx, input, output)
				delay, ok := tgerr.AsFloodWait(err)
				// A longer wait is returned as an error: reporting the limit beats hanging the tool call.
				if !ok || attempt >= maxRetries || delay > maxWait {
					return err
				}
				if err := wait(ctx, delay); err != nil {
					return err
				}
			}
		}
	})
}

// Timeout bounds the duration of each individual request.
func Timeout(d time.Duration) telegram.Middleware {
	return telegram.MiddlewareFunc(func(next tg.Invoker) telegram.InvokeFunc {
		return func(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
			ctx, cancel := context.WithTimeout(ctx, d)
			defer cancel()
			return next.Invoke(ctx, input, output)
		}
	})
}
