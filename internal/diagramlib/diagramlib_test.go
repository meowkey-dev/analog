package diagramlib

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestPinnedBundle(t *testing.T) {
	digest := sha256.Sum256(Bytes())
	if got := hex.EncodeToString(digest[:]); got != SHA256 {
		t.Fatalf("vendor hash = %s, want %s", got, SHA256)
	}
	// Script inlines the library verbatim; either sequence would end or comment
	// out the element early.
	lower := strings.ToLower(string(Bytes()))
	if strings.Contains(lower, "</script") || strings.Contains(lower, "<!--") {
		t.Fatal("library cannot be inlined in a <script> element")
	}
}

// The web UI and the export must draw the same diagram from the same text.
func TestMatchesTheWebUIVersion(t *testing.T) {
	data, err := os.ReadFile("../../web/package.json")
	if err != nil {
		t.Skip("web/ not present:", err)
	}
	var pkg struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		t.Fatal(err)
	}
	if got := pkg.Dependencies["pele"]; got != Version {
		t.Fatalf("web/package.json pins pele %q, diagramlib vendors %q", got, Version)
	}
}

func TestUsesSeesRenderedFencesOnly(t *testing.T) {
	if !Uses(`<pre><code class="language-mermaid">flowchart LR
</code></pre>`) {
		t.Error("a rendered mermaid fence was not detected")
	}
	if Uses("<pre><code>```mermaid\n</code></pre>") {
		t.Error("a quoted fence is text, not a diagram")
	}
}
