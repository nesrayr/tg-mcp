package domain

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_ParseChatRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    ChatRef
		wantErr bool
	}{
		{name: "user id", raw: "123456", want: ChatRef{ID: 123456}},
		{name: "group id", raw: "-4242", want: ChatRef{ID: -4242}},
		{name: "channel id", raw: "-1001234567890", want: ChatRef{ID: -1001234567890}},
		{name: "username lowercased", raw: "@Durov_Team", want: ChatRef{Username: "durov_team"}},
		{name: "surrounding spaces", raw: "  @durov  ", want: ChatRef{Username: "durov"}},
		{name: "zero id", raw: "0", wantErr: true},
		{name: "empty", raw: "", wantErr: true},
		{name: "bare word", raw: "durov", wantErr: true},
		{name: "username too short", raw: "@abc", wantErr: true},
		{name: "username starts with digit", raw: "@1abcd", wantErr: true},
		{name: "username with dash", raw: "@ab-cd", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseChatRef(tt.raw)
			if tt.wantErr {
				require.ErrorIs(t, err, ErrInvalidChatRef)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func Test_ChatRef_Matches(t *testing.T) {
	t.Parallel()

	chat := Chat{ID: -1001234567890, Username: "GoNews"}

	tests := []struct {
		name string
		ref  ChatRef
		want bool
	}{
		{name: "same id", ref: ChatRef{ID: -1001234567890}, want: true},
		{name: "other id", ref: ChatRef{ID: 1234567890}, want: false},
		{name: "username case insensitive", ref: ChatRef{Username: "gonews"}, want: true},
		{name: "other username", ref: ChatRef{Username: "gonewsx"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, tt.ref.Matches(chat))
		})
	}
}

func Test_ChatRef_String(t *testing.T) {
	t.Parallel()

	require.Equal(t, "@durov", ChatRef{Username: "durov"}.String())
	require.Equal(t, "-1001", ChatRef{ID: -1001}.String())
}
