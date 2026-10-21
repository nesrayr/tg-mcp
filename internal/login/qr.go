package login

import (
	"fmt"
	"io"
	"strings"

	"rsc.io/qr"
)

const (
	quietZone = 2

	ansiReset = "\x1b[0m"
	// Colors are set explicitly instead of following the terminal theme: the scanner needs a dark code
	// on a light background, and on a dark theme "regular" characters would produce an inverted QR.
	fgBlack = "\x1b[30m"
	fgWhite = "\x1b[97m"
	bgBlack = "\x1b[40m"
	bgWhite = "\x1b[107m"
)

// RenderQR draws a QR code in the terminal with half blocks: one character is two vertical modules.
func RenderQR(w io.Writer, text string) error {
	code, err := qr.Encode(text, qr.L)
	if err != nil {
		return fmt.Errorf("encode qr: %w", err)
	}

	dark := func(x, y int) bool {
		x, y = x-quietZone, y-quietZone
		return x >= 0 && y >= 0 && x < code.Size && y < code.Size && code.Black(x, y)
	}

	side := code.Size + 2*quietZone
	var b strings.Builder
	for y := 0; y < side; y += 2 {
		for x := 0; x < side; x++ {
			fg, bg := fgWhite, bgWhite
			if dark(x, y) {
				fg = fgBlack
			}
			if dark(x, y+1) {
				bg = bgBlack
			}
			b.WriteString(fg + bg + "▀")
		}
		b.WriteString(ansiReset + "\n")
	}

	_, err = io.WriteString(w, b.String())
	return err
}
