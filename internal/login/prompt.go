// Package login implements Telegram login for the `tg-mcp login` command.
package login

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// ErrNotTerminal means there is no interactive terminal for hidden input and output kept out of logs.
var ErrNotTerminal = errors.New("interactive terminal required: run this command in a separate terminal tab, not through an AI agent or a pipe")

//go:generate go tool mockgen -source=prompt.go -destination=mocks/prompter_mock.go -package=mocks

// Prompter asks the user for input.
type Prompter interface {
	Line(ctx context.Context, prompt string) (string, error)
	Secret(ctx context.Context, prompt string) (string, error)
}

// Terminal is a Prompter on top of stdin/stderr.
type Terminal struct {
	in     *os.File
	out    io.Writer
	reader *bufio.Reader
}

// NewTerminal returns a Terminal if stdin and stdout are an interactive terminal.
func NewTerminal() (*Terminal, error) {
	if !IsTerminal(os.Stdin) || !IsTerminal(os.Stdout) {
		return nil, ErrNotTerminal
	}
	// Prompts go to stderr so they do not mix with the command output.
	return &Terminal{
		in:     os.Stdin,
		out:    os.Stderr,
		reader: bufio.NewReader(os.Stdin),
	}, nil
}

// IsTerminal reports whether the file is attached to a terminal.
func IsTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

func (t *Terminal) Line(ctx context.Context, prompt string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	fmt.Fprint(t.out, prompt)
	line, err := t.reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read input: %w", err)
	}
	return strings.TrimSpace(line), nil
}

func (t *Terminal) Secret(ctx context.Context, prompt string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	fmt.Fprint(t.out, prompt)
	secret, err := term.ReadPassword(int(t.in.Fd()))
	fmt.Fprintln(t.out)
	if err != nil {
		return "", fmt.Errorf("read secret: %w", err)
	}
	return strings.TrimSpace(string(secret)), nil
}
