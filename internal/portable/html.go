// Package portable turns a space into an artifact that does not need
// analog-server: a portable HTML snapshot, or a PDF printed from that snapshot.
package portable

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"math"
	"mime"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	gmhtml "github.com/yuin/goldmark/renderer/html"

	"github.com/meowkey-dev/analog/client"
)

const pad = 48.0

// FetchMedia loads a file node's bytes. The CLI passes client.GetMedia; tests stub it.
type FetchMedia func(file string) (data []byte, contentType string, err error)

type Options struct {
	Title string
	Slug  string
	Fetch FetchMedia
}

var md = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithRendererOptions(gmhtml.WithXHTML()),
)

// HTML is a portable snapshot of the board: cards at their canvas positions and
// Analog-owned media inlined as data URIs. External media that cannot be fetched
// anonymously becomes a visible placeholder rather than receiving credentials.
func HTML(canvas client.Canvas, opt Options) (string, error) {
	nodes := canvas.Nodes
	minX, minY, width, height := boundsOf(nodes)

	var world strings.Builder
	world.WriteString(renderLinks(canvas.Edges, nodes, minX, minY, width, height))
	for _, node := range nodes {
		body, err := renderBody(node, opt.Fetch)
		if err != nil {
			return "", err
		}
		world.WriteString(renderCard(node, body))
	}

	title := opt.Title
	if title == "" {
		title = opt.Slug
	}
	pageW := width + 2*pad
	pageH := height + 2*pad + 44 // 44 = export-head
	if len(nodes) == 0 {
		pageW, pageH = 640, 360
	}

	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html lang=\"en\"><head><meta charset=\"utf-8\">")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">")
	fmt.Fprintf(&b, "<title>%s — analog</title>", escape(title))
	b.WriteString("<style>\n")
	b.WriteString(exportCSS)
	fmt.Fprintf(&b, "@page { size: %spx %spx; margin: 0; }\n", px(pageW), px(pageH))
	b.WriteString("</style></head><body>")
	fmt.Fprintf(&b, "<header class=\"export-head\"><span class=\"brand\">analog</span>"+
		"<span class=\"title\">%s</span><span class=\"slug\">/%s</span></header>",
		escape(title), escape(opt.Slug))
	if len(nodes) == 0 {
		b.WriteString("<p class=\"empty\">this space is empty</p></body></html>\n")
		return b.String(), nil
	}
	fmt.Fprintf(&b, "<div class=\"board\" style=\"width:%spx;height:%spx\">", px(width+2*pad), px(height+2*pad))
	fmt.Fprintf(&b, "<div class=\"world\" style=\"transform:translate(%spx,%spx)\">",
		px(pad-minX), px(pad-minY))
	b.WriteString(world.String())
	b.WriteString("</div></div></body></html>\n")
	return b.String(), nil
}

func boundsOf(nodes []client.Node) (minX, minY, width, height float64) {
	if len(nodes) == 0 {
		return 0, 0, 0, 0
	}
	minX, minY = 1e12, 1e12
	maxX, maxY := -1e12, -1e12
	for _, n := range nodes {
		x, y, w, h := box(n)
		if x < minX {
			minX = x
		}
		if y < minY {
			minY = y
		}
		if x+w > maxX {
			maxX = x + w
		}
		if y+h > maxY {
			maxY = y + h
		}
	}
	return minX, minY, maxX - minX, maxY - minY
}

func renderCard(node client.Node, body string) string {
	x, y, w, h := box(node)
	kind := kindOf(node)
	class := "card"
	if str(node, "sp_superseded_by") != "" {
		class += " superseded"
	}
	if str(node, "sp_deleted_at") != "" {
		class += " deleted"
	}
	title := str(node, "sp_title")
	if title == "" {
		title = str(node, "id")
	}
	by := str(node, "sp_created_by")
	rev := str(node, "sp_rev")
	if rev == "" {
		rev = "1"
	}
	return fmt.Sprintf(
		`<article class="%s" style="left:%spx;top:%spx;width:%spx;height:%spx">`+
			`<header class="card-head"><span class="card-title">%s</span>`+
			`<span class="card-kind">%s</span></header>%s`+
			`<footer class="card-foot"><span>%s</span><span>rev %s</span></footer></article>`,
		class, px(x), px(y), px(w), px(h),
		escape(title), escape(kind), body,
		escape(by), escape(rev),
	)
}

func renderBody(node client.Node, fetch FetchMedia) (string, error) {
	kind := kindOf(node)
	switch kind {
	case "md":
		var buf strings.Builder
		if err := md.Convert([]byte(str(node, "text")), &buf); err != nil {
			return "", err
		}
		return `<div class="card-body md">` + buf.String() + `</div>`, nil
	case "svg":
		// The web renderer sanitizes SVG before putting it in the parent document.
		// Keep the CLI export on the same side of that boundary; an exported file
		// is routinely opened outside the iframe sandbox used for html cards.
		return `<div class="card-body svg">` + sanitizeSVG(str(node, "text")) + `</div>`, nil
	case "html":
		return fmt.Sprintf(
			`<iframe class="card-body html" sandbox="allow-scripts" srcdoc="%s" title="%s"></iframe>`,
			escapeSrcdoc(str(node, "text")),
			escape(str(node, "sp_title")),
		), nil
	case "file":
		return renderFile(node, fetch)
	default:
		return `<pre class="card-body plain">` + escape(str(node, "text")) + `</pre>`, nil
	}
}

func renderFile(node client.Node, fetch FetchMedia) (string, error) {
	path := str(node, "file")
	title := str(node, "sp_title")
	if fetch == nil || path == "" {
		return `<div class="card-body file muted">` + escape(or(title, path, "file")) + `</div>`, nil
	}
	data, ctype, err := fetch(path)
	if err != nil {
		return `<div class="card-body file muted">media unavailable</div>`, nil
	}
	if ctype == "" {
		ctype = sniffType(path)
	}
	var ok bool
	ctype, ok = normalizeMediaType(ctype, path)
	if !ok {
		return `<div class="card-body file muted">unsupported media type</div>`, nil
	}
	uri := "data:" + ctype + ";base64," + base64.StdEncoding.EncodeToString(data)
	if ctype == "application/pdf" || strings.HasSuffix(strings.ToLower(path), ".pdf") {
		// A PDF extension is a safe fallback when a server omitted or mislabeled
		// its type; keep the emitted data URI on the same supported allowlist.
		ctype = "application/pdf"
		uri = "data:" + ctype + ";base64," + base64.StdEncoding.EncodeToString(data)
		return fmt.Sprintf(
			`<div class="card-body file"><embed src="%s" type="application/pdf" title="%s"></div>`,
			uri, escape(title),
		), nil
	}
	return fmt.Sprintf(
		`<div class="card-body file"><img src="%s" alt="%s"></div>`,
		uri, escape(title),
	), nil
}

func normalizeMediaType(contentType, path string) (string, bool) {
	if parsed, _, err := mime.ParseMediaType(strings.TrimSpace(contentType)); err == nil {
		contentType = strings.ToLower(parsed)
		if supportedMediaType(contentType) {
			return contentType, true
		}
	}
	inferred := sniffType(path)
	if supportedMediaType(inferred) {
		return inferred, true
	}
	return "", false
}

func supportedMediaType(contentType string) bool {
	switch contentType {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/svg+xml", "application/pdf":
		return true
	default:
		return false
	}
}

func sniffType(path string) string {
	switch {
	case strings.HasSuffix(strings.ToLower(path), ".png"):
		return "image/png"
	case strings.HasSuffix(strings.ToLower(path), ".jpg"), strings.HasSuffix(strings.ToLower(path), ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(strings.ToLower(path), ".gif"):
		return "image/gif"
	case strings.HasSuffix(strings.ToLower(path), ".webp"):
		return "image/webp"
	case strings.HasSuffix(strings.ToLower(path), ".svg"):
		return "image/svg+xml"
	case strings.HasSuffix(strings.ToLower(path), ".pdf"):
		return "application/pdf"
	}
	return "application/octet-stream"
}

type side string

const (
	sideTop    side = "top"
	sideRight  side = "right"
	sideBottom side = "bottom"
	sideLeft   side = "left"
)

func renderLinks(edges []client.Edge, nodes []client.Node, minX, minY, width, height float64) string {
	if len(edges) == 0 {
		return ""
	}
	byID := map[string]client.Node{}
	for _, n := range nodes {
		byID[str(n, "id")] = n
	}
	seen := map[string]bool{}
	var colors []string
	for _, e := range edges {
		c := safeEdgeColor(str(e, "color"))
		if c == "" {
			c = "default"
		}
		if !seen[c] {
			seen[c] = true
			colors = append(colors, c)
		}
	}
	sort.Strings(colors)
	var b strings.Builder
	fmt.Fprintf(&b, `<svg class="links" style="left:%spx;top:%spx;width:%spx;height:%spx" viewBox="%s %s %s %s">`,
		px(minX), px(minY), px(width), px(height), px(minX), px(minY), px(width), px(height))
	b.WriteString("<defs>")
	for _, color := range colors {
		id := markerID(color)
		fill := ""
		if color != "default" {
			fill = fmt.Sprintf(` style="fill:%s"`, escape(color))
		}
		fmt.Fprintf(&b, `<marker id="%s" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path d="M 0 0 L 10 5 L 0 10 z"%s/></marker>`,
			id, fill)
	}
	b.WriteString("</defs>")
	for _, e := range edges {
		from, okFrom := byID[str(e, "fromNode")]
		to, okTo := byID[str(e, "toNode")]
		if !okFrom || !okTo {
			continue
		}
		autoFrom, autoTo := autoSides(from, to)
		fromSide := sideOr(e, "fromSide", autoFrom)
		toSide := sideOr(e, "toSide", autoTo)
		ax, ay := anchor(from, fromSide)
		bx, by := anchor(to, toSide)
		d := curve(ax, ay, bx, by, fromSide, toSide)
		color := safeEdgeColor(str(e, "color"))
		style := ""
		if color != "" {
			style = fmt.Sprintf(` style="--edge-color:%s"`, escape(color))
		}
		dangling := str(from, "sp_deleted_at") != "" || str(to, "sp_deleted_at") != ""
		class := "edge"
		if dangling {
			class += " dangling"
		}
		marker := ""
		if !dangling {
			c := color
			if c == "" {
				c = "default"
			}
			marker = fmt.Sprintf(` marker-end="url(#%s)"`, markerID(c))
		}
		fmt.Fprintf(&b, `<g class="%s"%s><path class="edge-line"%s d="%s"/>`, class, style, marker, d)
		if label := str(e, "label"); label != "" {
			fmt.Fprintf(&b, `<text class="edge-label" x="%s" y="%s" text-anchor="middle" dy="0.32em">%s</text>`,
				px((ax+bx)/2), px((ay+by)/2), escape(label))
		}
		b.WriteString("</g>")
	}
	b.WriteString("</svg>")
	return b.String()
}

// Edge colors arrive from contract data, not a trusted UI enum. The canvas
// palette uses hex values; accepting only CSS hex forms plus the default preset
// keeps those values intact without interpolating arbitrary CSS into the SVG.
func safeEdgeColor(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "default") {
		return ""
	}
	if value[0] != '#' || (len(value) != 4 && len(value) != 5 && len(value) != 7 && len(value) != 9) {
		return ""
	}
	for _, r := range value[1:] {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return ""
		}
	}
	return value
}

func markerID(color string) string {
	var b strings.Builder
	b.WriteString("arrow-")
	for _, r := range color {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

func autoSides(from, to client.Node) (side, side) {
	fx, fy, fw, fh := box(from)
	tx, ty, tw, th := box(to)
	dx := (tx + tw/2) - (fx + fw/2)
	dy := (ty + th/2) - (fy + fh/2)
	if math.Abs(dx) >= math.Abs(dy) {
		if dx >= 0 {
			return sideRight, sideLeft
		}
		return sideLeft, sideRight
	}
	if dy >= 0 {
		return sideBottom, sideTop
	}
	return sideTop, sideBottom
}

func sideOr(e client.Edge, key string, fallback side) side {
	if s := str(e, key); s != "" {
		return side(s)
	}
	return fallback
}

func anchor(node client.Node, s side) (float64, float64) {
	x, y, w, h := box(node)
	switch s {
	case sideTop:
		return x + w/2, y
	case sideBottom:
		return x + w/2, y + h
	case sideLeft:
		return x, y + h/2
	default:
		return x + w, y + h/2
	}
}

func curve(ax, ay, bx, by float64, aSide, bSide side) string {
	pull := math.Hypot(bx-ax, by-ay) / 2.5
	if pull < 40 {
		pull = 40
	}
	ox, oy := out(aSide, pull)
	ix, iy := out(bSide, pull)
	return fmt.Sprintf("M %s %s C %s %s, %s %s, %s %s",
		px(ax), px(ay), px(ax+ox), px(ay+oy), px(bx+ix), px(by+iy), px(bx), px(by))
}

func out(s side, pull float64) (float64, float64) {
	switch s {
	case sideLeft:
		return -pull, 0
	case sideRight:
		return pull, 0
	case sideTop:
		return 0, -pull
	default:
		return 0, pull
	}
}

func box(n client.Node) (x, y, w, h float64) {
	return num(n, "x"), num(n, "y"), num(n, "width"), num(n, "height")
}

func kindOf(n client.Node) string {
	if str(n, "type") == "file" {
		return "file"
	}
	if k := str(n, "sp_kind"); k != "" {
		return k
	}
	return "plain"
}

func num(m map[string]any, key string) float64 {
	switch v := m[key].(type) {
	case json.Number:
		f, _ := v.Float64()
		return f
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	}
	return 0
}

func str(m map[string]any, key string) string {
	switch v := m[key].(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	}
	return ""
}

func px(v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%.2f", v)
}

func escape(s string) string { return html.EscapeString(s) }

// srcdoc is HTML parsed as the iframe document: escape & and " so the attribute
// stays intact, but leave tags alone so the card still renders.
func escapeSrcdoc(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}

func or(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
