package mcp

import (
	"time"

	"github.com/meowkey-dev/analog/client"
)

func (s *Server) register() {
	s.tools = []Tool{
		{
			Name:        "list_spaces",
			Description: "List every space, with card / link / open-annotation counts.",
			InputSchema: object(nil, map[string]any{}),
			handler: func(a Args) (any, error) {
				return s.api.ListSpaces()
			},
		},
		{
			Name:        "create_space",
			Description: "Create a space. `branch` keeps superseded cards visible.",
			InputSchema: object([]string{"slug", "title"}, map[string]any{
				"slug":          str("lowercase, digits and dashes"),
				"title":         str("human-readable name"),
				"revision_mode": enum("replace | branch", "replace", "branch"),
			}),
			handler: func(a Args) (any, error) {
				return s.api.CreateSpace(a.String("slug"), a.String("title"),
					a.StringOr("revision_mode", "replace"))
			},
		},
		{
			Name:        "read_space",
			Description: "The whole space: nodes, edges, and the open annotations on it.",
			InputSchema: object([]string{"slug"}, map[string]any{"slug": str("space slug")}),
			handler: func(a Args) (any, error) {
				slug := a.String("slug")
				canvas, err := s.api.GetCanvas(slug, false)
				if err != nil {
					return nil, err
				}
				space, err := s.api.GetSpace(slug)
				if err != nil {
					return nil, err
				}
				open := false
				annotations, err := s.api.ListAnnotations(slug, &open, "")
				if err != nil {
					return nil, err
				}
				return map[string]any{"space": space, "nodes": canvas.Nodes,
					"edges": canvas.Edges, "annotations": annotations}, nil
			},
		},
		{
			Name:        "add_cards",
			Description: "Post cards. One idea per card, or the human cannot annotate them.",
			InputSchema: object([]string{"slug", "cards"}, map[string]any{
				"slug": str("space slug"),
				"cards": map[string]any{
					"type": "array",
					"description": "{title, content, kind?: md|html|svg|plain, x?, y?}. " +
						"Omit x/y and the server places the card for you.",
					"items": object([]string{"title", "content"}, map[string]any{
						"title":   str(""),
						"content": str(""),
						"kind":    enum("md | html | svg | plain", "md", "html", "svg", "plain"),
						"x":       map[string]any{"type": "number"},
						"y":       map[string]any{"type": "number"},
						"width":   map[string]any{"type": "number"},
						"height":  map[string]any{"type": "number"},
						"color":   str(""),
						"meta":    map[string]any{"type": "object"},
					}),
				},
			}),
			handler: func(a Args) (any, error) {
				drafts := []client.CardDraft{}
				for _, raw := range a.Slice("cards") {
					fields, ok := raw.(map[string]any)
					if !ok {
						continue
					}
					card := Args(fields)
					draft := client.CardDraft{
						Title:   card.String("title"),
						Content: card.String("content"),
						Kind:    card.StringOr("kind", "md"),
						Color:   card.String("color"),
						Meta:    card.Map("meta"),
					}
					// Only forward geometry the caller actually gave: a zero here
					// would pin the card at the origin instead of auto-placing it.
					for _, geometry := range []struct {
						key  string
						into **float64
					}{
						{"x", &draft.X}, {"y", &draft.Y},
						{"width", &draft.Width}, {"height", &draft.Height},
					} {
						if _, given := fields[geometry.key]; given {
							value := card.Float(geometry.key, 0)
							*geometry.into = &value
						}
					}
					drafts = append(drafts, draft)
				}
				return s.api.CreateCards(a.String("slug"), drafts)
			},
		},
		{
			Name:        "update_card",
			Description: "Rewrite a card. In branch mode this returns the NEW card.",
			InputSchema: object([]string{"slug", "card_id", "patch"}, map[string]any{
				"slug":    str("space slug"),
				"card_id": str("the card to rewrite"),
				"patch": map[string]any{"type": "object",
					"description": "Any subset of a JSON Canvas node, e.g. {'text': '...'}"},
				"mode": enum("replace | branch", "replace", "branch"),
				"if_match": map[string]any{"type": "integer",
					"description": "The sp_rev you read. Returns a conflict if it moved on."},
			}),
			handler: func(a Args) (any, error) {
				return s.api.UpdateCard(a.String("slug"), a.String("card_id"),
					a.Map("patch"), a.String("mode"), a.Int("if_match"))
			},
		},
		{
			Name:        "delete_card",
			Description: "Remove a card. Don't delete cards the human created — annotate instead.",
			InputSchema: object([]string{"slug", "card_id"}, map[string]any{
				"slug": str("space slug"), "card_id": str("the card to remove"),
			}),
			handler: func(a Args) (any, error) {
				cardID := a.String("card_id")
				if err := s.api.DeleteCard(a.String("slug"), cardID); err != nil {
					return nil, err
				}
				return map[string]any{"deleted": cardID}, nil
			},
		},
		{
			Name:        "link_cards",
			Description: "Draw a labelled edge between two cards.",
			InputSchema: object([]string{"slug", "from_card", "to_card"}, map[string]any{
				"slug":      str("space slug"),
				"from_card": str("source card id"),
				"to_card":   str("target card id"),
				"label":     str("Always label. Unlabelled edges are noise."),
			}),
			handler: func(a Args) (any, error) {
				return s.api.LinkCards(a.String("slug"), a.String("from_card"),
					a.String("to_card"), a.String("label"))
			},
		},
		{
			Name: "get_feedback",
			Description: `What the human changed since you last looked.

All unresolved annotations come back every call — they ignore the cursor —
while card and link deltas are cursor-governed and exclude your own writes.
` + "`motivation: editing`" + ` is an instruction, ` + "`assessing`" + ` is a verdict,
` + "`commenting`" + ` is context. Deleted cards mean the human rejected that idea.`,
			InputSchema: object([]string{"slug"}, map[string]any{
				"slug": str("space slug"),
				"since": map[string]any{"type": "integer",
					"description": "Replay from this seq. Normally omit it: the server " +
						"keeps a cursor per actor, so you stay stateless."},
			}),
			handler: func(a Args) (any, error) {
				return s.api.GetFeedback(a.String("slug"), a.Int("since"), true)
			},
		},
		{
			Name:        "resolve_annotation",
			Description: "Mark one annotation done. Don't resolve what you haven't acted on.",
			InputSchema: object([]string{"annotation_id"}, map[string]any{
				"annotation_id": str("the annotation to close"),
				"reply":         str("What you did about it"),
				"slug":          str("Optional; without it the annotation is looked up by id"),
			}),
			handler: func(a Args) (any, error) {
				target := a.String("slug")
				if target == "" {
					found, _, err := s.api.FindAnnotation(a.String("annotation_id"))
					if err != nil {
						return nil, err
					}
					target = found
				}
				var reply *string
				if v, ok := a["reply"].(string); ok {
					reply = &v
				}
				return s.api.ResolveAnnotation(target, a.String("annotation_id"), reply, true)
			},
		},
		{
			Name: "await_feedback",
			Description: `Block until the human does something, then return it. For resident agents.

Returns the same shape as get_feedback; ` + "`summary`" + ` is empty if it timed out.`,
			InputSchema: object([]string{"slug"}, map[string]any{
				"slug": str("space slug"),
				"since": map[string]any{"type": "integer",
					"description": "Defaults to your cursor"},
				"timeout_s": map[string]any{"type": "number",
					"description": "Give up after this long"},
				"poll_s": map[string]any{"type": "number", "description": "Seconds between polls"},
			}),
			handler: func(a Args) (any, error) {
				return s.awaitFeedback(a.String("slug"), a.Int("since"),
					a.Float("timeout_s", 300), a.Float("poll_s", 2))
			},
		},
	}
}

func (s *Server) awaitFeedback(slug string, since *int64, timeoutS, pollS float64) (any, error) {
	deadline := s.now().Add(seconds(timeoutS))
	poll := seconds(pollS)

	// Waking on a non-empty `summary` does not work: unresolved annotations are
	// returned regardless of the cursor -- deliberately, so an open comment cannot
	// be missed -- so one open comment makes `summary` non-empty forever and this
	// returns instantly every time. The card and link deltas are cursor-governed
	// and clear when consumed, so any of those is news the moment it appears;
	// annotations need a baseline taken on entry, and only one that shows up while
	// we wait counts.
	var baseline map[string]bool
	for {
		peek, err := s.api.GetFeedback(slug, since, false)
		if err != nil {
			return nil, err
		}
		if baseline == nil {
			baseline = make(map[string]bool, len(peek.Annotations))
			for _, a := range peek.Annotations {
				baseline[a.ID] = true
			}
		}
		if hasDeltas(peek) || hasNewAnnotation(peek, baseline) {
			// Consume: the caller is about to act on it.
			return s.api.GetFeedback(slug, since, true)
		}
		left := deadline.Sub(s.now())
		if left <= 0 {
			return peek, nil
		}
		if poll < left {
			left = poll
		}
		s.sleep(left)
	}
}

// hasDeltas reports whether anything cursor-governed is waiting.
func hasDeltas(f client.Feedback) bool {
	return len(f.CardsEdited) > 0 || len(f.CardsDeleted) > 0 || len(f.CardsMoved) > 0 ||
		len(f.LinksAdded) > 0 || len(f.LinksRemoved) > 0
}

func hasNewAnnotation(f client.Feedback, baseline map[string]bool) bool {
	for _, a := range f.Annotations {
		if !baseline[a.ID] {
			return true
		}
	}
	return false
}

func seconds(v float64) time.Duration { return time.Duration(v * float64(time.Second)) }
