// Package client is a typed HTTP client for the Analog API.
//
// Shapes follow contracts/openapi.json. This is the only place the MCP server and
// the CLI talk to the API: SPEC §4 says neither may contain business logic, so this
// package does transport, config and error mapping and nothing else.
//
// It is deliberately not under internal/: third parties import it.
package client

// DefaultURL is the address contracts/openapi.json advertises.
const DefaultURL = "http://127.0.0.1:8787"

// Node and Edge stay free-form maps. A JSON Canvas node carries arbitrary sp_* keys
// and anything else the canvas format grows, and a client that dropped them on the
// way through would be worse than useless.
type (
	Node = map[string]any
	Edge = map[string]any
)

type Canvas struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

type Counts struct {
	Cards           int `json:"cards"`
	Links           int `json:"links"`
	OpenAnnotations int `json:"open_annotations"`
}

type Space struct {
	ID           string `json:"id"`
	Slug         string `json:"slug"`
	Title        string `json:"title"`
	RevisionMode string `json:"revision_mode"`
	Seq          int64  `json:"seq"`
	CreatedAt    string `json:"created_at"`
	Counts       Counts `json:"counts"`
}

type Annotation struct {
	ID        string `json:"id"`
	CardID    string `json:"card_id"`
	CardTitle string `json:"card_title"`
	// Branch mode only; absent while the card is current.
	CardSupersededBy string `json:"card_superseded_by,omitempty"`
	CardRev          int64  `json:"card_rev"`
	Selector         any    `json:"selector"`
	Body             string `json:"body"`
	Motivation       string `json:"motivation"`
	Creator          string `json:"creator"`
	CreatorKind      string `json:"creator_kind"`
	Resolved         bool   `json:"resolved"`
	ResolvedReply    any    `json:"resolved_reply"`
	Stale            bool   `json:"stale"`
	CreatedAt        string `json:"created_at"`
}

type Event struct {
	Seq       int64          `json:"seq"`
	TS        string         `json:"ts"`
	Type      string         `json:"type"`
	SubjectID string         `json:"subject_id"`
	Actor     string         `json:"actor"`
	ActorKind string         `json:"actor_kind"`
	Payload   map[string]any `json:"payload,omitempty"`
}

type EventPage struct {
	Events []Event `json:"events"`
	Cursor int64   `json:"cursor"`
}

type Feedback struct {
	Cursor       int64            `json:"cursor"`
	Annotations  []Annotation     `json:"annotations"`
	Replies      []map[string]any `json:"replies"`
	CardsEdited  []map[string]any `json:"cards_edited"`
	CardsDeleted []map[string]any `json:"cards_deleted"`
	CardsMoved   []map[string]any `json:"cards_moved"`
	LinksAdded   []map[string]any `json:"links_added"`
	LinksRemoved []map[string]any `json:"links_removed"`
	Summary      string           `json:"summary"`
}

// CardDraft is the friendly card body. Omitted x/y mean "the server places it".
type CardDraft struct {
	Title   string         `json:"title"`
	Content string         `json:"content"`
	Kind    string         `json:"kind,omitempty"`
	X       *float64       `json:"x,omitempty"`
	Y       *float64       `json:"y,omitempty"`
	Width   *float64       `json:"width,omitempty"`
	Height  *float64       `json:"height,omitempty"`
	Color   string         `json:"color,omitempty"`
	Meta    map[string]any `json:"meta,omitempty"`
}

type ImportResult struct {
	IDMap  map[string]string `json:"id_map"`
	Canvas Canvas            `json:"canvas"`
}

type Media struct {
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
	Bytes       int    `json:"bytes"`
}

type Health struct {
	OK           bool   `json:"ok"`
	Service      string `json:"service"`
	Version      string `json:"version"`
	AuthRequired bool   `json:"auth_required"`
}

type Identity struct {
	Authenticated bool   `json:"authenticated"`
	Actor         string `json:"actor"`
	ActorKind     string `json:"actor_kind"`
}
