// Package apierr is the Error schema in contracts/openapi.json, and nothing else.
//
// Whatever net/http produces natively for a malformed body does not match the
// contract, so decode failures are remapped to a 400 `validation_failed`.
package apierr

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// Detail is the free-form map an error can carry. Nil means no detail key.
type Detail map[string]any

type Error struct {
	Status  int
	Code    string
	Message string
	Detail  Detail
}

func (e *Error) Error() string { return fmt.Sprintf("%d %s: %s", e.Status, e.Code, e.Message) }

// promoted keys sit at the top level of the body rather than under `detail`:
// openapi.json's 409 for updateCard puts the current node there.
var promoted = []string{"current"}

// Body is the wire form. Key order is irrelevant — clients parse it — but the
// shape is exactly {error, message, detail?} plus any promoted key.
func (e *Error) Body() map[string]any {
	out := map[string]any{"error": e.Code, "message": e.Message}
	detail := make(Detail, len(e.Detail))
	for k, v := range e.Detail {
		detail[k] = v
	}
	for _, key := range promoted {
		if v, ok := detail[key]; ok {
			out[key] = v
			delete(detail, key)
		}
	}
	if len(detail) > 0 {
		out["detail"] = detail
	}
	return out
}

// Write renders the error as the contract's Error body.
func (e *Error) Write(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(e.Status)
	_ = json.NewEncoder(w).Encode(e.Body())
}

func mk(status int, code, message string, detail Detail) *Error {
	return &Error{Status: status, Code: code, Message: message, Detail: detail}
}

func NotFound(message string, detail ...Detail) *Error {
	return mk(http.StatusNotFound, "not_found", message, first(detail))
}

func Conflict(message string, detail ...Detail) *Error {
	return mk(http.StatusConflict, "conflict", message, first(detail))
}

func ActorRequired(message string, detail ...Detail) *Error {
	return mk(http.StatusBadRequest, "actor_required", message, first(detail))
}

func Unauthorized(message string, detail ...Detail) *Error {
	return mk(http.StatusUnauthorized, "unauthorized", message, first(detail))
}

func Forbidden(message string, detail ...Detail) *Error {
	return mk(http.StatusForbidden, "forbidden", message, first(detail))
}

func ValidationFailed(message string, detail ...Detail) *Error {
	return mk(http.StatusBadRequest, "validation_failed", message, first(detail))
}

func UnsupportedKind(message string, detail ...Detail) *Error {
	return mk(http.StatusBadRequest, "unsupported_kind", message, first(detail))
}

func first(d []Detail) Detail {
	if len(d) == 0 || len(d[0]) == 0 {
		return nil
	}
	return d[0]
}
