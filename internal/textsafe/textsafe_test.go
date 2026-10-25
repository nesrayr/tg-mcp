package textsafe

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_CleanTruncated(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		text          string
		maxRunes      int
		want          string
		wantTruncated bool
	}{
		{
			name:     "plain text with newlines and tabs",
			text:     "line 1\n\tline 2",
			maxRunes: 100,
			want:     "line 1\n\tline 2",
		},
		{
			name:     "control characters",
			text:     "a\x00b\x1bc\x7fd\re",
			maxRunes: 100,
			want:     "abcde",
		},
		{
			name:     "zero width characters",
			text:     "ig\u200bno\u200dr\ufeffe",
			maxRunes: 100,
			want:     "ignore",
		},
		{
			name:     "bidi overrides and isolates",
			text:     "file\u202egpj.exe\u2066x\u2069",
			maxRunes: 100,
			want:     "filegpj.exex",
		},
		{
			name:     "unicode tag characters",
			text:     "hi\U000E0049\U000E0047\U000E004E",
			maxRunes: 100,
			want:     "hi",
		},
		{
			name:     "variation selectors",
			text:     "a\ufe01\U000E0100b",
			maxRunes: 100,
			want:     "ab",
		},
		{
			name:     "soft hyphen and invisible fillers",
			text:     "soft\u00adhy\u034fphen\u3164\u115f",
			maxRunes: 100,
			want:     "softhyphen",
		},
		{
			name:     "invalid utf8",
			text:     "ok\xffok",
			maxRunes: 100,
			want:     "ok�ok",
		},
		{
			name:     "emoji with skin tone kept",
			text:     "👍🏽 ok",
			maxRunes: 100,
			want:     "👍🏽 ok",
		},
		{
			name:          "truncated by runes",
			text:          strings.Repeat("é", 10),
			maxRunes:      4,
			want:          "éééé" + TruncatedMark,
			wantTruncated: true,
		},
		{
			name:     "hidden runes do not count",
			text:     "\u200bab\u200bc",
			maxRunes: 3,
			want:     "abc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, truncated := CleanTruncated(tt.text, tt.maxRunes)
			require.Equal(t, tt.want, got)
			require.Equal(t, tt.wantTruncated, truncated)
		})
	}
}
