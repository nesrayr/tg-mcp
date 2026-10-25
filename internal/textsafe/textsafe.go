// Package textsafe cleans untrusted text before it reaches the model or the terminal.
package textsafe

import (
	"strings"
	"unicode"
)

// TruncatedMark ends text cut by the length limit.
const TruncatedMark = "…[truncated]"

// Clean removes characters that hide text from a human but not from the model and limits the length in runes.
func Clean(text string, maxRunes int) string {
	clean, _ := CleanTruncated(text, maxRunes)
	return clean
}

// CleanTruncated is Clean that also reports whether the text was truncated.
func CleanTruncated(text string, maxRunes int) (string, bool) {
	text = strings.ToValidUTF8(text, "�")

	var b strings.Builder
	b.Grow(len(text))
	count := 0
	for _, r := range text {
		if isHidden(r) {
			continue
		}
		if count == maxRunes {
			b.WriteString(TruncatedMark)
			return b.String(), true
		}
		b.WriteRune(r)
		count++
	}
	return b.String(), false
}

func isHidden(r rune) bool {
	switch {
	case r == '\n' || r == '\t':
		return false
	case unicode.IsControl(r),
		// Format characters: zero-width, bidi controls, Unicode tags, soft hyphen.
		unicode.Is(unicode.Cf, r),
		// Variation selectors can smuggle a byte each after an emoji.
		unicode.Is(unicode.Variation_Selector, r),
		// Combining grapheme joiner and Hangul fillers render as nothing.
		r == 0x034F, r == 0x115F, r == 0x1160, r == 0x3164, r == 0xFFA0:
		return true
	default:
		return false
	}
}
