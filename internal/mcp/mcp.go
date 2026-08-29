// Package mcp is SPEC §4.1's ten tools.
//
// A thin proxy over client/. Every rule, including the get_feedback delta, lives in
// the server; contracts/README.md is explicit that delta computation is an endpoint
// so it cannot drift between the MCP surface and the CLI.
package mcp

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/meowkey-dev/analog/client"
)

// Instructions is what the agent is told about the canvas before it uses it.
const Instructions = `A shared canvas you and a human both write to.

Call get_feedback(slug) at the start of every turn: it returns exactly what the
human changed, already diffed. Nothing back means nothing changed.

One idea per card — a wall of text cannot be annotated usefully. Use kind="html"
or kind="svg" for anything visual; the human can pin comments on regions of it.
Always label links. Don't edit or delete the human's cards, don't rearrange the
canvas, and don't resolve annotations you haven't acted on.
`

// API is the slice of the client these tools use. An interface so the tools can be
// tested without a server.
type API interface {
	ListSpaces() ([]client.Space, error)
	CreateSpace(slug, title, revisionMode string) (client.Space, error)
	GetSpace(slug string) (client.Space, error)
	GetCanvas(slug string, includeDeleted bool) (client.Canvas, error)
	ListAnnotations(slug string, resolved *bool, cardID string) ([]client.Annotation, error)
	CreateCards(slug string, cards []client.CardDraft) ([]client.Node, error)
	UpdateCard(slug, cardID string, patch map[string]any, mode string, ifMatch *int64) (client.Node, error)
	DeleteCard(slug, cardID string) error
	LinkCards(slug, fromID, toID, label string) (client.Edge, error)
	GetFeedback(slug string, since *int64, advance bool) (client.Feedback, error)
	ResolveAnnotation(slug, annotationID string, reply *string, resolved bool) (client.Annotation, error)
	FindAnnotation(annotationID string) (string, client.Annotation, error)
}

var _ API = (*client.Client)(nil)

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`

	handler func(Args) (any, error)
}

type Server struct {
	api   API
	tools []Tool
	index map[string]*Tool
	// now and sleep are injectable so await_feedback is testable without waiting.
	now   func() time.Time
	sleep func(time.Duration)
}

func New(api API) *Server {
	s := &Server{api: api, now: time.Now, sleep: time.Sleep}
	s.register()
	s.index = map[string]*Tool{}
	for i := range s.tools {
		s.index[s.tools[i].Name] = &s.tools[i]
	}
	return s
}

func (s *Server) Tools() []Tool { return s.tools }

// Call runs one tool. An API failure reaches the agent as a readable message, not a
// stack.
func (s *Server) Call(name string, arguments map[string]any) (any, error) {
	tool, ok := s.index[name]
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
	result, err := tool.handler(Args(arguments))
	if err != nil {
		if e, ok := client.As(err); ok {
			return nil, fmt.Errorf("%s", e.Error())
		}
		return nil, err
	}
	return result, nil
}

// --- argument access -------------------------------------------------------------

// Args is one tool call's arguments, decoded from JSON.
type Args map[string]any

func (a Args) String(key string) string {
	s, _ := a[key].(string)
	return s
}

func (a Args) StringOr(key, fallback string) string {
	if s, ok := a[key].(string); ok && s != "" {
		return s
	}
	return fallback
}

func (a Args) Map(key string) map[string]any {
	m, _ := a[key].(map[string]any)
	return m
}

func (a Args) Int(key string) *int64 {
	value, ok := a[key]
	if !ok || value == nil {
		return nil
	}
	switch n := value.(type) {
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return &i
		}
	case float64:
		i := int64(n)
		return &i
	case int64:
		return &n
	}
	return nil
}

func (a Args) Float(key string, fallback float64) float64 {
	value, ok := a[key]
	if !ok || value == nil {
		return fallback
	}
	switch n := value.(type) {
	case json.Number:
		if f, err := n.Float64(); err == nil {
			return f
		}
	case float64:
		return n
	}
	return fallback
}

func (a Args) Slice(key string) []any {
	s, _ := a[key].([]any)
	return s
}

// --- schema helpers ----------------------------------------------------------------

func object(required []string, properties map[string]any) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{
		"type": "object", "properties": properties, "required": required,
	}
}

func str(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func enum(description string, values ...string) map[string]any {
	options := make([]any, 0, len(values))
	for _, v := range values {
		options = append(options, v)
	}
	return map[string]any{"type": "string", "enum": options, "description": description}
}
