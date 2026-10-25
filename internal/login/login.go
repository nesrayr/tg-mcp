package login

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/auth/qrlogin"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

const maxPasswordAttempts = 3

var ErrSignUpNotSupported = errors.New("this phone number has no Telegram account; sign up is not supported")

// WithQR logs in with a QR code: the code is printed to out and refreshed until it is scanned.
func WithQR(ctx context.Context, client *telegram.Client, loggedIn qrlogin.LoggedIn, prompt Prompter, out io.Writer) (*tg.User, error) {
	show := func(_ context.Context, token qrlogin.Token) error {
		fmt.Fprint(out, "\x1b[2J\x1b[H")
		fmt.Fprintln(out, "Scan the code with Telegram on your phone:")
		fmt.Fprintln(out, "Settings → Devices → Link Desktop Device")
		fmt.Fprintln(out)
		if err := RenderQR(out, token.URL()); err != nil {
			return err
		}
		fmt.Fprintf(out, "\nThe code refreshes at %s. Press Ctrl+C to cancel.\n", token.Expires().Local().Format("15:04:05"))
		return nil
	}

	_, err := client.QR().Auth(ctx, loggedIn, show)
	if tgerr.Is(err, "SESSION_PASSWORD_NEEDED") {
		fmt.Fprintln(out, "\nCode accepted. Two-step verification is enabled.")
		err = checkPassword(ctx, client.Auth(), prompt)
	}
	if err != nil {
		return nil, fmt.Errorf("qr login: %w", err)
	}
	return self(ctx, client)
}

// WithCode logs in with a phone number and the code Telegram sends to the app.
func WithCode(ctx context.Context, client *telegram.Client, prompt Prompter) (*tg.User, error) {
	flow := auth.NewFlow(terminalAuth{prompt: prompt}, auth.SendCodeOptions{})
	if err := flow.Run(ctx, client.Auth()); err != nil {
		return nil, fmt.Errorf("code login: %w", err)
	}
	return self(ctx, client)
}

// Status returns the current user, or nil if the session is not authorized.
func Status(ctx context.Context, client *telegram.Client) (*tg.User, error) {
	status, err := client.Auth().Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth status: %w", err)
	}
	if !status.Authorized {
		return nil, nil
	}
	return status.User, nil
}

func self(ctx context.Context, client *telegram.Client) (*tg.User, error) {
	user, err := Status(ctx, client)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("login finished but session is not authorized")
	}
	return user, nil
}

func checkPassword(ctx context.Context, client *auth.Client, prompt Prompter) error {
	for attempt := 1; ; attempt++ {
		password, err := prompt.Secret(ctx, "Cloud password (2FA): ")
		if err != nil {
			return err
		}
		_, err = client.Password(ctx, password)
		if errors.Is(err, auth.ErrPasswordInvalid) && attempt < maxPasswordAttempts {
			fmt.Fprintln(os.Stderr, "Wrong password, try again.")
			continue
		}
		return err
	}
}

type terminalAuth struct {
	prompt Prompter
}

func (a terminalAuth) Phone(ctx context.Context) (string, error) {
	phone, err := a.prompt.Line(ctx, "Phone number in international format (+1…): ")
	if err != nil {
		return "", err
	}
	return NormalizePhone(phone)
}

func (a terminalAuth) Code(ctx context.Context, _ *tg.AuthSentCode) (string, error) {
	return a.prompt.Line(ctx, "Code from Telegram (sent to your Telegram app): ")
}

func (a terminalAuth) Password(ctx context.Context) (string, error) {
	return a.prompt.Secret(ctx, "Cloud password (2FA): ")
}

func (terminalAuth) SignUp(context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, ErrSignUpNotSupported
}

func (terminalAuth) AcceptTermsOfService(context.Context, tg.HelpTermsOfService) error {
	return ErrSignUpNotSupported
}

var ErrInvalidPhone = errors.New("phone must be in international format, e.g. +79991234567")

// NormalizePhone strips spaces, dashes and parentheses and checks the international format.
func NormalizePhone(raw string) (string, error) {
	phone := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '-', '(', ')':
			return -1
		}
		return r
	}, strings.TrimSpace(raw))

	digits, ok := strings.CutPrefix(phone, "+")
	if !ok || len(digits) < 7 || len(digits) > 15 {
		return "", ErrInvalidPhone
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return "", ErrInvalidPhone
		}
	}
	return phone, nil
}
