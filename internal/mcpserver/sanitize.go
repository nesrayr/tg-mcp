package mcpserver

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxTextRunes  = 4000
	maxNameRunes  = 200
	truncatedMark = "…[truncated]"
)

// sanitize removes characters from untrusted text that can be used to hide
// instructions from a human but not from the model: control characters, bidi
// overrides, zero-width characters and Unicode tags. The length is limited.
func sanitize(text string, maxRunes int) string {
	text = strings.ToValidUTF8(text, "�")

	var b strings.Builder
	b.Grow(len(text))
	count := 0
	for _, r := range text {
		if isHidden(r) {
			continue
		}
		if count == maxRunes {
			b.WriteString(truncatedMark)
			break
		}
		b.WriteRune(r)
		count++
	}
	return b.String()
}

func isHidden(r rune) bool {
	switch {
	case r == '\n' || r == '\t':
		return false
	case unicode.IsControl(r):
		return true
	case r >= 0x200B && r <= 0x200F, // zero-width space/joiners, LRM/RLM
		r >= 0x202A && r <= 0x202E,   // bidi embeddings and overrides
		r >= 0x2060 && r <= 0x2064,   // word joiner, invisible operators
		r >= 0x2066 && r <= 0x2069,   // bidi isolates
		r == 0x061C,                  // arabic letter mark
		r == 0xFEFF,                  // zero-width no-break space
		r >= 0xE0000 && r <= 0xE007F: // tag characters
		return true
	default:
		return false
	}
}

func runeLen(s string) int {
	return utf8.RuneCountInString(s)
}
