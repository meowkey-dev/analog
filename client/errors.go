package client

import (
	"errors"
	"fmt"
)

// The error codes the contract's Error schema uses.
const (
	CodeNotFound         = "not_found"
	CodeConflict         = "conflict"
	CodeActorRequired    = "actor_required"
	CodeValidationFailed = "validation_failed"
	CodeUnsupportedKind  = "unsupported_kind"
	CodeUnauthorized     = "unauthorized"
	CodeForbidden        = "forbidden"
	// CodeUnreachable is the client's own: the server never answered.
	CodeUnreachable = "unreachable"
)

// Error is any non-2xx answer, plus the case where there was no answer at all.
type Error struct {
	Status  int
	Code    string
	Message string
	// Body is the parsed response, so a caller can reach a key the contract
	// promotes to the top level — `current` on a 409, in particular.
	Body map[string]any
	URL  string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%d %s: %s", e.Status, e.Code, e.Message)
}

// Current is the server's current node on a 409. SPEC §3: a conflict is surfaced,
// never auto-resolved.
func (e *Error) Current() Node {
	if node, ok := e.Body["current"].(map[string]any); ok {
		return node
	}
	return nil
}

// Is reports whether err is an *Error with the given code. Kinds of failure the
// callers actually branch on — a 401 exits 3, a 409 exits 2 — rather than a
// hierarchy of types nobody switches on.
func Is(err error, code string) bool {
	var e *Error
	if !errors.As(err, &e) {
		return false
	}
	if code == CodeValidationFailed {
		// unsupported_kind is a validation failure with a friendlier name; the
		// Python client mapped both to one exception and callers rely on that.
		return e.Code == CodeValidationFailed || e.Code == CodeUnsupportedKind
	}
	return e.Code == code
}

// As unwraps err to an *Error, if it is one.
func As(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)
	return e, ok
}
