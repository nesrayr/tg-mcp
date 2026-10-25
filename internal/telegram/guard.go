package telegram

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
)

// ErrMethodNotAllowed is returned for a request outside the policy; such a request never reaches Telegram.
var ErrMethodNotAllowed = errors.New("telegram method is not allowed")

// MethodNotAllowedError specifies which method was blocked.
type MethodNotAllowedError struct {
	Method string
}

func (e *MethodNotAllowedError) Error() string {
	return fmt.Sprintf("%s: %s", ErrMethodNotAllowed, e.Method)
}

func (e *MethodNotAllowedError) Is(target error) bool {
	return target == ErrMethodNotAllowed
}

type typedRequest interface {
	TypeID() uint32
}

// Policy is an allowlist of RPC methods the client is allowed to call.
type Policy struct {
	allowed map[uint32]struct{}
}

// NewPolicy builds a policy from zero values of the allowed requests, e.g. &tg.MessagesGetHistoryRequest{}.
func NewPolicy(requests ...typedRequest) Policy {
	allowed := make(map[uint32]struct{}, len(requests))
	for _, request := range requests {
		allowed[request.TypeID()] = struct{}{}
	}
	return Policy{allowed: allowed}
}

// With returns a new policy extended with additional methods.
func (p Policy) With(requests ...typedRequest) Policy {
	allowed := make(map[uint32]struct{}, len(p.allowed)+len(requests))
	for id := range p.allowed {
		allowed[id] = struct{}{}
	}
	for _, request := range requests {
		allowed[request.TypeID()] = struct{}{}
	}
	return Policy{allowed: allowed}
}

// Methods returns the names of the allowed methods in alphabetical order.
func (p Policy) Methods() []string {
	names := make([]string, 0, len(p.allowed))
	for id := range p.allowed {
		names = append(names, tlName(id))
	}
	slices.Sort(names)
	return names
}

// Allows reports whether the request is allowed. Requests without a TypeID are always denied.
func (p Policy) Allows(input bin.Encoder) bool {
	request, ok := input.(typedRequest)
	if !ok {
		return false
	}
	_, ok = p.allowed[request.TypeID()]
	return ok
}

// ReadOnlyPolicy lists the read methods the server needs; none of them changes the account state.
var ReadOnlyPolicy = NewPolicy(
	// None of these sends messages, marks anything as read or changes the "online" status.
	// Session check on client start (users.getUsers for self).
	&tg.UsersGetUsersRequest{},
	// Allowlisted chat lookup and unread counters.
	&tg.MessagesGetDialogsRequest{},
	&tg.MessagesGetPeerDialogsRequest{},
	&tg.ContactsResolveUsernameRequest{},
	// Reading and searching messages.
	&tg.MessagesGetHistoryRequest{},
	&tg.MessagesSearchRequest{},
	&tg.MessagesGetMessagesRequest{},
	&tg.ChannelsGetMessagesRequest{},
	// Attachment downloads. gotd needs auth.exportAuthorization when the file is stored
	// in another data center: the authorization is transferred to the connection with it.
	&tg.UploadGetFileRequest{},
	&tg.AuthExportAuthorizationRequest{},
)

// LoginPolicy adds QR, code and 2FA login methods to ReadOnlyPolicy for the login command only.
var LoginPolicy = ReadOnlyPolicy.With(
	// Signing up a new account is deliberately not allowed.
	&tg.AuthExportLoginTokenRequest{},
	&tg.AuthImportLoginTokenRequest{},
	&tg.AuthSendCodeRequest{},
	&tg.AuthSignInRequest{},
	&tg.AccountGetPasswordRequest{},
	&tg.AuthCheckPasswordRequest{},
)

// Guard is a middleware that rejects every request outside the policy before it reaches the network.
func Guard(policy Policy) telegram.Middleware {
	return telegram.MiddlewareFunc(func(next tg.Invoker) telegram.InvokeFunc {
		return func(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
			if !policy.Allows(input) {
				return &MethodNotAllowedError{Method: methodName(input)}
			}
			return next.Invoke(ctx, input, output)
		}
	})
}

var typeNames = sync.OnceValue(tg.TypesMap)

func methodName(input bin.Encoder) string {
	request, ok := input.(typedRequest)
	if !ok {
		return fmt.Sprintf("%T", input)
	}
	return tlName(request.TypeID())
}

func tlName(id uint32) string {
	name, ok := typeNames()[id]
	if !ok {
		return fmt.Sprintf("0x%08x", id)
	}
	name, _, _ = strings.Cut(name, "#")
	return name
}
