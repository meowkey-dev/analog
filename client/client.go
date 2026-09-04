package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Options configures a Client. The zero value is usable: it reads ~/.analog.toml
// and the environment.
type Options struct {
	URL       string
	Actor     string
	ActorKind string
	Token     string
	Timeout   time.Duration
	Transport http.RoundTripper
	// Config supplies the settings directly. Nil loads them; a non-nil empty map
	// means "no configuration", which is what the tests want.
	Config map[string]string
	// ConfigPath overrides where LoadConfig looks. Ignored when Config is set.
	ConfigPath string
}

// Client calls every §3 endpoint, one method each.
//
// Actor has no default on purpose (SPEC §10): an unconfigured agent must fail
// loudly rather than write anonymously.
type Client struct {
	Base      string
	Actor     string
	ActorKind string
	Token     string
	WebURL    string
	// ConfigSpace is ANALOG_SPACE: SPEC §4.2 spells `analog resolve a_7f` with no
	// slug, and this is what supplies one.
	ConfigSpace string

	http  *http.Client
	sleep func(time.Duration)
}

const maxMediaRedirects = 10

func New(opts Options) *Client {
	config := opts.Config
	if config == nil {
		config = LoadConfig(opts.ConfigPath)
	}
	pick := func(given, key string) string {
		if given != "" {
			return given
		}
		return config[key]
	}

	base := pick(opts.URL, "url")
	if base == "" {
		base = DefaultURL
	}
	c := &Client{
		Base:        NormalizeBase(base),
		Actor:       pick(opts.Actor, "actor"),
		Token:       pick(opts.Token, "token"),
		ConfigSpace: config["space"],
		sleep:       time.Sleep,
	}
	c.ActorKind = pick(opts.ActorKind, "actor_kind")
	if c.ActorKind == "" {
		c.ActorKind = "agent"
	}
	c.WebURL = strings.TrimRight(pick("", "web_url"), "/")
	if c.WebURL == "" {
		c.WebURL = strings.TrimSuffix(c.Base, "/api")
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	c.http = &http.Client{Timeout: timeout, Transport: opts.Transport}
	return c
}

// --- plumbing ----------------------------------------------------------------

func (c *Client) actorParams(withKind bool) (url.Values, error) {
	if c.Actor == "" {
		return nil, &Error{Status: 400, Code: CodeActorRequired,
			Message: "no actor configured; set ANALOG_ACTOR or pass Actor"}
	}
	params := url.Values{"actor": {c.Actor}}
	if withKind {
		params.Set("actor_kind", c.ActorKind)
	}
	return params, nil
}

type request struct {
	method  string
	path    string
	params  url.Values
	body    any
	raw     []byte
	headers map[string]string
	ctype   string
}

// do issues one request, retrying once on a connection-level failure and never on
// an HTTP status.
func (c *Client) do(req request, into any) error {
	target := c.Base + req.path
	if len(req.params) > 0 {
		target += "?" + req.params.Encode()
	}

	var payload []byte
	if req.raw != nil {
		payload = req.raw
	} else if req.body != nil {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(req.body); err != nil {
			return err
		}
		payload = buf.Bytes()
	}

	var response *http.Response
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		httpReq, err := http.NewRequest(req.method, target, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		if payload != nil {
			ctype := req.ctype
			if ctype == "" {
				ctype = "application/json"
			}
			httpReq.Header.Set("Content-Type", ctype)
		}
		if c.Token != "" {
			httpReq.Header.Set("Authorization", "Bearer "+c.Token)
		}
		for k, v := range req.headers {
			httpReq.Header.Set(k, v)
		}
		response, lastErr = c.http.Do(httpReq)
		if lastErr == nil {
			break
		}
		if attempt == 0 {
			c.sleep(250 * time.Millisecond)
		}
	}
	if lastErr != nil {
		return &Error{Status: 0, Code: CodeUnreachable,
			Message: fmt.Sprintf("cannot reach %s: %v", c.Base, lastErr), URL: req.path}
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	if response.StatusCode >= 400 {
		return errorFrom(response.StatusCode, body, target)
	}
	if response.StatusCode == http.StatusNoContent || len(body) == 0 || into == nil {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	return dec.Decode(into)
}

func errorFrom(status int, body []byte, target string) *Error {
	out := &Error{Status: status, Code: "error", URL: target}
	var parsed map[string]any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&parsed); err != nil || parsed == nil {
		out.Message = string(body)
		return out
	}
	out.Body = parsed
	if code, ok := parsed["error"].(string); ok {
		out.Code = code
	}
	if message, ok := parsed["message"].(string); ok {
		out.Message = message
	} else {
		out.Message = string(body)
	}
	return out
}

func boolParam(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// --- connection ----------------------------------------------------------------

// Health is reachable without a token; it says whether one is needed.
func (c *Client) Health() (Health, error) {
	var out Health
	return out, c.do(request{method: "GET", path: "/health"}, &out)
}

func (c *Client) Whoami() (Identity, error) {
	var out Identity
	return out, c.do(request{method: "GET", path: "/whoami"}, &out)
}

// --- spaces ----------------------------------------------------------------------

func (c *Client) ListSpaces() ([]Space, error) {
	out := []Space{}
	return out, c.do(request{method: "GET", path: "/spaces"}, &out)
}

func (c *Client) CreateSpace(slug, title, revisionMode string) (Space, error) {
	var out Space
	params, err := c.actorParams(true)
	if err != nil {
		return out, err
	}
	if revisionMode == "" {
		revisionMode = "replace"
	}
	return out, c.do(request{method: "POST", path: "/spaces", params: params,
		body: map[string]any{"slug": slug, "title": title,
			"revision_mode": revisionMode}}, &out)
}

func (c *Client) GetSpace(slug string) (Space, error) {
	var out Space
	return out, c.do(request{method: "GET", path: "/spaces/" + slug}, &out)
}

func (c *Client) UpdateSpace(slug string, patch map[string]any) (Space, error) {
	var out Space
	params, err := c.actorParams(true)
	if err != nil {
		return out, err
	}
	return out, c.do(request{method: "PATCH", path: "/spaces/" + slug,
		params: params, body: patch}, &out)
}

func (c *Client) DeleteSpace(slug string) error {
	params, err := c.actorParams(true)
	if err != nil {
		return err
	}
	return c.do(request{method: "DELETE", path: "/spaces/" + slug, params: params}, nil)
}

// --- canvas ------------------------------------------------------------------------

func (c *Client) GetCanvas(slug string, includeDeleted bool) (Canvas, error) {
	out := Canvas{}
	return out, c.do(request{method: "GET", path: "/spaces/" + slug + "/canvas",
		params: url.Values{"include_deleted": {boolParam(includeDeleted)}}}, &out)
}

func (c *Client) ImportCanvas(slug string, canvas Canvas) (ImportResult, error) {
	var out ImportResult
	params, err := c.actorParams(true)
	if err != nil {
		return out, err
	}
	nodes, edges := canvas.Nodes, canvas.Edges
	if nodes == nil {
		nodes = []Node{}
	}
	if edges == nil {
		edges = []Edge{}
	}
	return out, c.do(request{method: "POST", path: "/spaces/" + slug + "/import",
		params: params, body: map[string]any{"nodes": nodes, "edges": edges}}, &out)
}

// --- cards --------------------------------------------------------------------------

func (c *Client) CreateCards(slug string, cards []CardDraft) ([]Node, error) {
	out := []Node{}
	params, err := c.actorParams(true)
	if err != nil {
		return nil, err
	}
	if cards == nil {
		cards = []CardDraft{}
	}
	return out, c.do(request{method: "POST", path: "/spaces/" + slug + "/cards",
		params: params, body: map[string]any{"cards": cards}}, &out)
}

// CreateNodes posts raw JSON Canvas nodes — the only way to create a `file` node.
func (c *Client) CreateNodes(slug string, nodes []Node) ([]Node, error) {
	out := []Node{}
	params, err := c.actorParams(true)
	if err != nil {
		return nil, err
	}
	if nodes == nil {
		nodes = []Node{}
	}
	return out, c.do(request{method: "POST", path: "/spaces/" + slug + "/cards",
		params: params, body: map[string]any{"nodes": nodes}}, &out)
}

// UpdateCard patches a card. mode is "" for the space default; ifMatch is nil for
// an unconditional write.
func (c *Client) UpdateCard(slug, cardID string, patch map[string]any, mode string,
	ifMatch *int64) (Node, error) {
	params, err := c.actorParams(true)
	if err != nil {
		return nil, err
	}
	if mode != "" {
		params.Set("mode", mode)
	}
	headers := map[string]string{}
	if ifMatch != nil {
		headers["If-Match"] = strconv.FormatInt(*ifMatch, 10)
	}
	out := Node{}
	return out, c.do(request{method: "PATCH",
		path: "/spaces/" + slug + "/cards/" + cardID, params: params,
		body: patch, headers: headers}, &out)
}

func (c *Client) DeleteCard(slug, cardID string) error {
	params, err := c.actorParams(true)
	if err != nil {
		return err
	}
	return c.do(request{method: "DELETE",
		path: "/spaces/" + slug + "/cards/" + cardID, params: params}, nil)
}

// --- links ---------------------------------------------------------------------------

func (c *Client) CreateLinks(slug string, edges []Edge) ([]Edge, error) {
	out := []Edge{}
	params, err := c.actorParams(true)
	if err != nil {
		return nil, err
	}
	if edges == nil {
		edges = []Edge{}
	}
	return out, c.do(request{method: "POST", path: "/spaces/" + slug + "/links",
		params: params, body: map[string]any{"edges": edges}}, &out)
}

func (c *Client) LinkCards(slug, fromID, toID, label string) (Edge, error) {
	edge := Edge{"fromNode": fromID, "toNode": toID}
	if label != "" {
		edge["label"] = label
	}
	built, err := c.CreateLinks(slug, []Edge{edge})
	if err != nil {
		return nil, err
	}
	if len(built) == 0 {
		return nil, &Error{Status: 0, Code: "error", Message: "server returned no edge"}
	}
	return built[0], nil
}

func (c *Client) DeleteLink(slug, linkID string) error {
	params, err := c.actorParams(true)
	if err != nil {
		return err
	}
	return c.do(request{method: "DELETE",
		path: "/spaces/" + slug + "/links/" + linkID, params: params}, nil)
}

// --- annotations ----------------------------------------------------------------------

func (c *Client) ListAnnotations(slug string, resolved *bool, cardID string) ([]Annotation, error) {
	out := []Annotation{}
	params := url.Values{}
	if resolved != nil {
		params.Set("resolved", boolParam(*resolved))
	}
	if cardID != "" {
		params.Set("card_id", cardID)
	}
	return out, c.do(request{method: "GET", path: "/spaces/" + slug + "/annotations",
		params: params}, &out)
}

func (c *Client) CreateAnnotation(slug, cardID, body string, selector map[string]any,
	motivation string) (Annotation, error) {
	var out Annotation
	params, err := c.actorParams(true)
	if err != nil {
		return out, err
	}
	if motivation == "" {
		motivation = "commenting"
	}
	return out, c.do(request{method: "POST", path: "/spaces/" + slug + "/annotations",
		params: params, body: map[string]any{"card_id": cardID, "body": body,
			"selector": selector, "motivation": motivation}}, &out)
}

func (c *Client) ResolveAnnotation(slug, annotationID string, reply *string,
	resolved bool) (Annotation, error) {
	var out Annotation
	params, err := c.actorParams(true)
	if err != nil {
		return out, err
	}
	return out, c.do(request{method: "PATCH",
		path: "/spaces/" + slug + "/annotations/" + annotationID, params: params,
		body: map[string]any{"resolved": resolved, "reply": reply}}, &out)
}

// FindAnnotation locates an annotation by id alone.
//
// SPEC §4.1/§4.2 spell `resolve_annotation(id)` and `analog resolve a_7f` without a
// slug, and the API has no cross-space lookup. Scanning spaces is a lookup, not a
// rule, so it stays out of the server.
func (c *Client) FindAnnotation(annotationID string) (string, Annotation, error) {
	spaces, err := c.ListSpaces()
	if err != nil {
		return "", Annotation{}, err
	}
	for _, space := range spaces {
		annotations, err := c.ListAnnotations(space.Slug, nil, "")
		if err != nil {
			return "", Annotation{}, err
		}
		for _, a := range annotations {
			if a.ID == annotationID {
				return space.Slug, a, nil
			}
		}
	}
	return "", Annotation{}, &Error{Status: 404, Code: CodeNotFound,
		Message: fmt.Sprintf("no annotation '%s' in any space", annotationID)}
}

// --- feedback, events -------------------------------------------------------------------

func (c *Client) GetFeedback(slug string, since *int64, advance bool) (Feedback, error) {
	var out Feedback
	params, err := c.actorParams(false)
	if err != nil {
		return out, err
	}
	params.Set("advance", boolParam(advance))
	if since != nil {
		params.Set("since", strconv.FormatInt(*since, 10))
	}
	return out, c.do(request{method: "GET", path: "/spaces/" + slug + "/feedback",
		params: params}, &out)
}

func (c *Client) ListEvents(slug string, since int64, limit int) (EventPage, error) {
	var out EventPage
	if limit == 0 {
		limit = 200
	}
	return out, c.do(request{method: "GET", path: "/spaces/" + slug + "/events",
		params: url.Values{
			"since": {strconv.FormatInt(since, 10)},
			"limit": {strconv.Itoa(limit)},
		}}, &out)
}

// --- media --------------------------------------------------------------------------------

func (c *Client) UploadMedia(slug, path, contentType string) (Media, error) {
	var out Media
	params, err := c.actorParams(true)
	if err != nil {
		return out, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return out, err
	}
	if contentType == "" {
		contentType = mime.TypeByExtension(filepath.Ext(path))
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition",
		fmt.Sprintf(`form-data; name="file"; filename=%q`, filepath.Base(path)))
	header.Set("Content-Type", contentType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return out, err
	}
	if _, err := part.Write(data); err != nil {
		return out, err
	}
	if err := writer.Close(); err != nil {
		return out, err
	}
	return out, c.do(request{method: "POST", path: "/spaces/" + slug + "/media",
		params: params, raw: buf.Bytes(), ctype: writer.FormDataContentType()}, &out)
}

// GetMedia fetches a file node's bytes. `file` is the node's `file` field, usually
// `/api/spaces/<slug>/media/<name>`. Same-origin Analog media gets the bearer;
// external media is fetched anonymously so imported boards stay useful without
// turning an arbitrary URL into a credential sink. This is a GET of an existing
// object, not a JSON round-trip, so it cannot go through do().
func (c *Client) GetMedia(file string) ([]byte, string, error) {
	target := c.resolveURL(file)
	targetURL, err := url.Parse(target)
	if err != nil {
		return nil, "", err
	}
	authenticated := c.Token != "" && isAnalogMediaURL(c.WebURL, targetURL)
	// A media URL can redirect. Keep the bearer token on the narrow media
	// allowlist only; an open redirect must never turn a media fetch into a
	// credential transfer to another route or origin.
	httpClient := &http.Client{
		Transport: c.http.Transport,
		Timeout:   c.http.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxMediaRedirects {
				return fmt.Errorf("media fetch stopped after %d redirects", maxMediaRedirects)
			}
			if authenticated && isAnalogMediaURL(c.WebURL, req.URL) {
				req.Header.Set("Authorization", "Bearer "+c.Token)
			} else {
				req.Header.Del("Authorization")
			}
			return nil
		},
	}
	var response *http.Response
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		httpReq, err := http.NewRequest(http.MethodGet, target, nil)
		if err != nil {
			return nil, "", err
		}
		if authenticated {
			httpReq.Header.Set("Authorization", "Bearer "+c.Token)
		}
		response, lastErr = httpClient.Do(httpReq)
		if lastErr == nil {
			break
		}
		if attempt == 0 {
			c.sleep(250 * time.Millisecond)
		}
	}
	if lastErr != nil {
		return nil, "", &Error{Status: 0, Code: CodeUnreachable,
			Message: fmt.Sprintf("cannot reach %s: %v", c.Base, lastErr), URL: file}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, "", err
	}
	if response.StatusCode >= 400 {
		return nil, "", errorFrom(response.StatusCode, body, target)
	}
	ctype := response.Header.Get("Content-Type")
	if parsed, _, err := mime.ParseMediaType(ctype); err == nil {
		ctype = parsed
	}
	return body, ctype, nil
}

func (c *Client) resolveURL(path string) string {
	base, err := url.Parse(c.WebURL)
	if err != nil {
		return path
	}
	ref, err := url.Parse(path)
	if err != nil {
		return path
	}
	return base.ResolveReference(ref).String()
}

func isAnalogMediaURL(base string, target *url.URL) bool {
	if target == nil || target.Scheme == "" || target.Host == "" || target.User != nil {
		return false
	}
	origin, err := url.Parse(base)
	if err != nil || origin.Scheme == "" || origin.Host == "" || origin.User != nil {
		return false
	}
	if !strings.EqualFold(origin.Scheme, target.Scheme) || !strings.EqualFold(origin.Host, target.Host) {
		return false
	}
	parts := strings.Split(strings.Trim(target.EscapedPath(), "/"), "/")
	return len(parts) == 5 && parts[0] == "api" && parts[1] == "spaces" &&
		parts[2] != "" && parts[3] == "media" && parts[4] != ""
}

// --- convenience ----------------------------------------------------------------------------

func (c *Client) SpaceURL(slug string) string { return c.WebURL + "/s/" + slug }

// StreamEvents yields events as they arrive, reconnecting with Last-Event-ID.
//
// The token rides in a header, which is why this is a plain request rather than
// anything EventSource-shaped: EventSource cannot set one, and a token in the query
// string leaks into logs and referrers.
func (c *Client) StreamEvents(ctx context.Context, slug string, since int64,
	onEvent func(Event) error) error {
	last := since
	for {
		err := c.streamOnce(ctx, slug, &last, onEvent)
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second): // SPEC §5: fall back and retry rather than die
		}
	}
}

// streamOnce returns nil when the connection dropped and should be retried, and an
// error when the caller asked to stop or the server refused.
func (c *Client) streamOnce(ctx context.Context, slug string, last *int64,
	onEvent func(Event) error) error {
	target := c.Base + "/spaces/" + slug + "/events/stream"
	req, err := http.NewRequestWithContext(ctx, "GET", target, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Last-Event-ID", strconv.FormatInt(*last, 10))
	req.Header.Set("Accept", "text/event-stream")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	// No client timeout on a stream: it is meant to stay open.
	streamer := &http.Client{Transport: c.http.Transport}
	response, err := streamer.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return nil
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		body, _ := io.ReadAll(response.Body)
		return errorFrom(response.StatusCode, body, target)
	}

	return forEachSSEMessage(response.Body, func(event Event) error {
		if event.Seq > *last {
			*last = event.Seq
		}
		return onEvent(event)
	})
}
