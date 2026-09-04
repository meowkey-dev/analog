package portable

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meowkey-dev/analog/client"
)

func TestHTMLRendersTheFixtureBoard(t *testing.T) {
	canvas := loadCanvas(t)
	html, err := HTML(canvas, Options{
		Title: "Nav redesign",
		Slug:  "redesign",
		Fetch: func(file string) ([]byte, string, error) {
			if !strings.HasSuffix(file, "m_01.png") {
				t.Errorf("unexpected fetch %s", file)
			}
			return []byte("\x89PNG"), "image/png", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<!DOCTYPE html>",
		"Nav redesign",
		"/redesign",
		"Option A",
		"Option B",
		"lowest risk",
		"Render time by option",
		"viewBox=\"0 0 200 120\"",
		"Prototype",
		`sandbox="allow-scripts"`,
		"srcdoc=",
		"Current UI, 4k rows",
		"data:image/png;base64,",
		"depends on",
		"class=\"links\"",
		"claude-code",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("html missing %q", want)
		}
	}
	script := strings.Index(html, "<script>const l=")
	srcdoc := strings.Index(html, "srcdoc=")
	if script >= 0 && (srcdoc < 0 || script < srcdoc) {
		t.Error("html card leaked into the parent document; it belongs in srcdoc")
	}
}

func TestHTMLEscapesAHostileTitle(t *testing.T) {
	html, err := HTML(client.Canvas{Nodes: []client.Node{{
		"id": "c_1", "type": "text", "x": 0, "y": 0, "width": 100, "height": 80,
		"text": "<script>alert(1)</script>", "sp_kind": "plain",
		"sp_title": `A & B <C>"`,
	}}}, Options{Title: `x <y>`, Slug: `a&b`})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "<script>alert") {
		t.Error("plain-text script tags must be escaped")
	}
	if !strings.Contains(html, "A &amp; B &lt;C&gt;") {
		t.Errorf("title not escaped: %s", html)
	}
}

func TestHTMLSanitizesHostileSVG(t *testing.T) {
	canvas := client.Canvas{Nodes: []client.Node{{
		"id": "c_1", "type": "text", "x": 0, "y": 0, "width": 160, "height": 100,
		"sp_kind": "svg", "sp_title": "chart",
		"text": `<svg xmlns="http://www.w3.org/2000/svg" onload="alert('root')">
<script>alert('script')</script>
<rect width="20" height="20" fill="red" filter="url(https://evil.example/filter)" onclick="alert('rect')"/>
<foreignObject><body><img src="https://evil.example/track"/></body></foreignObject>
<image href="https://evil.example/image.png"/>
</svg>`,
	}}}
	html, err := HTML(canvas, Options{Slug: "s"})
	if err != nil {
		t.Fatal(err)
	}
	for _, hostile := range []string{"alert", "foreignObject", "evil.example", "onload", "onclick"} {
		if strings.Contains(html, hostile) {
			t.Errorf("hostile SVG token %q survived export: %s", hostile, html)
		}
	}
	if !strings.Contains(html, "<rect") || !strings.Contains(html, `fill="red"`) {
		t.Errorf("safe SVG content did not survive: %s", html)
	}
}

func TestHTMLCardKeepsTheOpaqueOriginSandbox(t *testing.T) {
	html, err := HTML(client.Canvas{Nodes: []client.Node{{
		"id": "c_1", "type": "text", "x": 0, "y": 0, "width": 160, "height": 100,
		"text": `<button>run</button>`, "sp_kind": "html", "sp_title": "demo",
	}}}, Options{Slug: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, `sandbox="allow-scripts"`) {
		t.Fatalf("html card lost its scripts-only sandbox: %s", html)
	}
	for _, capability := range []string{"allow-same-origin", "allow-forms"} {
		if strings.Contains(html, capability) {
			t.Errorf("portable html card unexpectedly grants %s", capability)
		}
	}
}

func TestHTMLNormalizesHostileEdgeColor(t *testing.T) {
	canvas := client.Canvas{
		Nodes: []client.Node{
			{"id": "c_1", "type": "text", "sp_kind": "plain", "x": 0, "y": 0, "width": 100, "height": 80},
			{"id": "c_2", "type": "text", "sp_kind": "plain", "x": 240, "y": 0, "width": 100, "height": 80},
		},
		Edges: []client.Edge{
			{"id": "l_safe", "fromNode": "c_1", "toNode": "c_2", "color": "#e06c75"},
			{"id": "l_hostile", "fromNode": "c_1", "toNode": "c_2", "color": `#e06c75; background:url(https://evil.example/)`},
		},
	}
	html, err := HTML(canvas, Options{Slug: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, `--edge-color:#e06c75`) {
		t.Errorf("normal hex edge color was not preserved: %s", html)
	}
	if !strings.Contains(html, "arrow-default") {
		t.Errorf("invalid edge color did not fall back to the default preset: %s", html)
	}
	for _, hostile := range []string{"evil.example", `style="--edge-color:#e06c75;`, `url(https://evil.example/)`} {
		if strings.Contains(html, hostile) {
			t.Errorf("hostile edge color token %q survived export: %s", hostile, html)
		}
	}
}

func TestHTMLLeavesMarkdownHTMLEscaped(t *testing.T) {
	html, err := HTML(client.Canvas{Nodes: []client.Node{{
		"id": "c_1", "type": "text", "x": 0, "y": 0, "width": 100, "height": 80,
		"text": "hello <script>alert(1)</script>", "sp_kind": "md", "sp_title": "md",
	}}}, Options{Slug: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "<script>alert") {
		t.Error("markdown must not execute raw HTML (the live renderer doesn't either)")
	}
}

func TestHTMLInlinesSrcdocWithoutEscapingTags(t *testing.T) {
	html, err := HTML(client.Canvas{Nodes: []client.Node{{
		"id": "c_1", "type": "text", "x": 0, "y": 0, "width": 100, "height": 80,
		"text": `<h3>hi</h3><p class="x">ok</p>`, "sp_kind": "html", "sp_title": "p",
	}}}, Options{Slug: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "&quot;x&quot;") {
		t.Error("srcdoc quotes must be escaped so the attribute does not break")
	}
	if !strings.Contains(html, "<h3>hi</h3>") {
		t.Error("html card tags must survive inside srcdoc")
	}
}

func TestHTMLEmptySpace(t *testing.T) {
	html, err := HTML(client.Canvas{}, Options{Slug: "blank", Title: "Blank"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "this space is empty") {
		t.Errorf("got %s", html)
	}
}

func TestHTMLMissingMediaIsAPlaceholder(t *testing.T) {
	html, err := HTML(client.Canvas{Nodes: []client.Node{{
		"id": "c_1", "type": "file", "x": 0, "y": 0, "width": 100, "height": 80,
		"file": "/api/spaces/s/media/m_1.png", "sp_title": "shot",
	}}}, Options{Slug: "s", Fetch: func(string) ([]byte, string, error) {
		return nil, "", os.ErrNotExist
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "media unavailable") {
		t.Errorf("got %s", html)
	}
}

func TestHTMLNormalizesHostileMediaType(t *testing.T) {
	html, err := HTML(client.Canvas{Nodes: []client.Node{{
		"id": "c_1", "type": "file", "x": 0, "y": 0, "width": 100, "height": 80,
		"file": "/api/spaces/s/media/m_1.png", "sp_title": "shot",
	}}}, Options{Slug: "s", Fetch: func(string) ([]byte, string, error) {
		return []byte("png"), `image/png" onerror="alert(1)`, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "data:image/png;base64,") {
		t.Errorf("hostile content type did not fall back to the path type: %s", html)
	}
	for _, hostile := range []string{"onerror", "alert(1)", `data:image/png"`} {
		if strings.Contains(html, hostile) {
			t.Errorf("hostile content type token %q survived export: %s", hostile, html)
		}
	}
}

func TestPDFPrintsWhenChromeIsAvailable(t *testing.T) {
	if os.Getenv("ANALOG_TEST_CHROME") == "" || ChromePath() == "" {
		t.Skip("set ANALOG_TEST_CHROME=1 to exercise chrome --print-to-pdf")
	}
	html := `<!DOCTYPE html><html><head><title>t</title></head><body><p>hello analog</p></body></html>`
	pdf, err := PDF(html)
	if err != nil {
		t.Fatal(err)
	}
	if string(pdf[:5]) != "%PDF-" {
		t.Fatalf("not a pdf (%d bytes)", len(pdf))
	}
}

func TestChromePathHonoursANALOG_CHROME(t *testing.T) {
	t.Setenv("ANALOG_CHROME", "/no/such/chrome")
	if p := ChromePath(); p != "" {
		t.Errorf("missing binary = %q", p)
	}
}

func loadCanvas(t *testing.T) client.Canvas {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "contracts", "fixtures", "canvas.json"))
	if err != nil {
		t.Fatal(err)
	}
	var canvas client.Canvas
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&canvas); err != nil {
		t.Fatal(err)
	}
	return canvas
}
