package telegram

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/stretchr/testify/require"
)

type scriptedInvoker struct {
	errs  []error
	calls int
}

func (s *scriptedInvoker) Invoke(_ context.Context, _ bin.Encoder, _ bin.Decoder) error {
	s.calls++
	if len(s.errs) == 0 {
		return nil
	}
	err := s.errs[0]
	s.errs = s.errs[1:]
	return err
}

func Test_FloodWait(t *testing.T) {
	t.Parallel()

	floodErr := func(seconds string) error { return tgerr.New(420, "FLOOD_WAIT_"+seconds) }
	otherErr := errors.New("network down")

	tests := []struct {
		name      string
		errs      []error
		waitErr   error
		wantErr   error
		wantFlood bool
		wantCalls int
		wantWaits []time.Duration
	}{
		{name: "success without wait", wantCalls: 1},
		{name: "retries after short flood wait", errs: []error{floodErr("3")}, wantCalls: 2, wantWaits: []time.Duration{3 * time.Second}},
		{name: "long flood wait is returned", errs: []error{floodErr("120")}, wantFlood: true, wantCalls: 1},
		{name: "retries are limited", errs: []error{floodErr("1"), floodErr("1"), floodErr("1")}, wantFlood: true, wantCalls: 3, wantWaits: []time.Duration{time.Second, time.Second}},
		{name: "other error is not retried", errs: []error{otherErr}, wantErr: otherErr, wantCalls: 1},
		{name: "cancelled wait stops retry", errs: []error{floodErr("5")}, waitErr: context.Canceled, wantErr: context.Canceled, wantCalls: 1, wantWaits: []time.Duration{5 * time.Second}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			next := &scriptedInvoker{errs: tt.errs}
			var waits []time.Duration
			wait := func(_ context.Context, d time.Duration) error {
				waits = append(waits, d)
				return tt.waitErr
			}
			invoker := FloodWait(30*time.Second, 2, wait).Handle(next)

			err := invoker.Invoke(context.Background(), &tg.MessagesGetHistoryRequest{}, &tg.MessagesMessagesBox{})

			switch {
			case tt.wantErr != nil:
				require.ErrorIs(t, err, tt.wantErr)
			case tt.wantFlood:
				_, isFlood := tgerr.AsFloodWait(err)
				require.True(t, isFlood)
			default:
				require.NoError(t, err)
			}
			require.Equal(t, tt.wantCalls, next.calls)
			require.Equal(t, tt.wantWaits, waits)
		})
	}
}

func Test_Timeout(t *testing.T) {
	t.Parallel()

	var deadline time.Time
	next := invokerFunc(func(ctx context.Context, _ bin.Encoder, _ bin.Decoder) error {
		deadline, _ = ctx.Deadline()
		return nil
	})
	before := time.Now()

	err := Timeout(time.Minute).Handle(next).Invoke(context.Background(), &tg.MessagesGetHistoryRequest{}, &tg.MessagesMessagesBox{})

	require.NoError(t, err)
	require.WithinDuration(t, before.Add(time.Minute), deadline, 5*time.Second)
}

func Test_SleepContext(t *testing.T) {
	t.Parallel()

	t.Run("waits", func(t *testing.T) {
		t.Parallel()

		require.NoError(t, SleepContext(context.Background(), time.Millisecond))
	})

	t.Run("cancelled", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		require.ErrorIs(t, SleepContext(ctx, time.Hour), context.Canceled)
	})
}

type invokerFunc func(ctx context.Context, input bin.Encoder, output bin.Decoder) error

func (f invokerFunc) Invoke(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
	return f(ctx, input, output)
}
