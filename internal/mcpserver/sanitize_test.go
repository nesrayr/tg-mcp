package mcpserver

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_sanitize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		text     string
		maxRunes int
		want     string
	}{
		{name: "plain text with newlines and tabs", text: "line 1\n\tline 2", maxRunes: 100, want: "line 1\n\tline 2"},
		{name: "control characters", text: "a\x00b\x1bc\x7fd\re", maxRunes: 100, want: "abcde"},
		{name: "zero width characters", text: "ig\u200bno\u200dr\ufeffe", maxRunes: 100, want: "ignore"},
		{name: "bidi overrides and isolates", text: "file\u202egpj.exe\u2066x\u2069", maxRunes: 100, want: "filegpj.exex"},
		{name: "unicode tag characters", text: "hi\U000E0049\U000E0047\U000E004E", maxRunes: 100, want: "hi"},
		{name: "invalid utf8", text: "ok\xffok", maxRunes: 100, want: "ok�ok"},
		{name: "emoji kept", text: "👍🏽 ok", maxRunes: 100, want: "👍🏽 ok"},
		{name: "truncated by runes", text: strings.Repeat("é", 10), maxRunes: 4, want: "éééé" + truncatedMark},
		{name: "hidden runes do not count", text: "\u200bab\u200bc", maxRunes: 3, want: "abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, sanitize(tt.text, tt.maxRunes))
		})
	}
}
