package login

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gotd/td/tg"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"rsc.io/qr"

	"github.com/nesrayr/tg-mcp/internal/login/mocks"
)

func Test_NormalizePhone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "plain", raw: "+79991234567", want: "+79991234567"},
		{name: "with spaces and dashes", raw: " +7 (999) 123-45-67 ", want: "+79991234567"},
		{name: "without plus", raw: "79991234567", wantErr: true},
		{name: "local format", raw: "89991234567", wantErr: true},
		{name: "letters", raw: "+7999abc4567", wantErr: true},
		{name: "too short", raw: "+12345", wantErr: true},
		{name: "too long", raw: "+1234567890123456", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := NormalizePhone(tt.raw)
			if tt.wantErr {
				require.ErrorIs(t, err, ErrInvalidPhone)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func Test_terminalAuth(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("phone is normalized", func(t *testing.T) {
		t.Parallel()

		prompt := mocks.NewMockPrompter(gomock.NewController(t))
		prompt.EXPECT().Line(ctx, "Phone number in international format (+1…): ").Return("+7 999 123-45-67", nil)

		phone, err := terminalAuth{prompt: prompt}.Phone(ctx)
		require.NoError(t, err)
		require.Equal(t, "+79991234567", phone)
	})

	t.Run("invalid phone", func(t *testing.T) {
		t.Parallel()

		prompt := mocks.NewMockPrompter(gomock.NewController(t))
		prompt.EXPECT().Line(ctx, "Phone number in international format (+1…): ").Return("8 999 123 45 67", nil)

		_, err := terminalAuth{prompt: prompt}.Phone(ctx)
		require.ErrorIs(t, err, ErrInvalidPhone)
	})

	t.Run("prompt error", func(t *testing.T) {
		t.Parallel()

		readErr := errors.New("eof")
		prompt := mocks.NewMockPrompter(gomock.NewController(t))
		prompt.EXPECT().Line(ctx, "Phone number in international format (+1…): ").Return("", readErr)

		_, err := terminalAuth{prompt: prompt}.Phone(ctx)
		require.ErrorIs(t, err, readErr)
	})

	t.Run("password is read hidden", func(t *testing.T) {
		t.Parallel()

		prompt := mocks.NewMockPrompter(gomock.NewController(t))
		prompt.EXPECT().Secret(ctx, "Cloud password (2FA): ").Return("secret", nil)

		password, err := terminalAuth{prompt: prompt}.Password(ctx)
		require.NoError(t, err)
		require.Equal(t, "secret", password)
	})

	t.Run("code", func(t *testing.T) {
		t.Parallel()

		prompt := mocks.NewMockPrompter(gomock.NewController(t))
		prompt.EXPECT().Line(ctx, "Code from Telegram (sent to your Telegram app): ").Return("12345", nil)

		code, err := terminalAuth{prompt: prompt}.Code(ctx, &tg.AuthSentCode{})
		require.NoError(t, err)
		require.Equal(t, "12345", code)
	})

	t.Run("sign up is refused", func(t *testing.T) {
		t.Parallel()

		_, err := terminalAuth{}.SignUp(ctx)
		require.ErrorIs(t, err, ErrSignUpNotSupported)
		require.ErrorIs(t, terminalAuth{}.AcceptTermsOfService(ctx, tg.HelpTermsOfService{}), ErrSignUpNotSupported)
	})
}

func Test_RenderQR(t *testing.T) {
	t.Parallel()

	text := "tg://login?token=AQIDBAUGBwgJCgsMDQ4PEA=="
	code, err := qr.Encode(text, qr.L)
	require.NoError(t, err)

	var out strings.Builder
	require.NoError(t, RenderQR(&out, text))

	side := code.Size + 2*quietZone
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	require.Len(t, lines, (side+1)/2)

	for row, line := range lines {
		cells := strings.Split(strings.TrimSuffix(line, ansiReset), "▀")
		cells = cells[:len(cells)-1]
		require.Len(t, cells, side)

		for x, cell := range cells {
			top, bottom := row*2-quietZone, row*2+1-quietZone
			xm := x - quietZone
			wantTop := xm >= 0 && xm < code.Size && top >= 0 && top < code.Size && code.Black(xm, top)
			wantBottom := xm >= 0 && xm < code.Size && bottom >= 0 && bottom < code.Size && code.Black(xm, bottom)

			require.Equal(t, wantTop, strings.HasPrefix(cell, fgBlack))
			require.Equal(t, wantBottom, strings.HasSuffix(cell, bgBlack))
		}
	}
}
