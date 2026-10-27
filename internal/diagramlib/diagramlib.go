// Package diagramlib owns the pinned Mermaid renderer used by portable exports.
package diagramlib

import (
	"embed"
	"strings"
)

const Version = "1.9.0"
const SHA256 = "323c16de7844d4b609743f70449ebce041a531e4f48f23b1e9e7ec9d3b917287"

//go:embed pele.global.js export-bootstrap.js LICENSE
var files embed.FS

func Bytes() []byte {
	data, _ := files.ReadFile("pele.global.js")
	return data
}

// fence is what goldmark renders a ```mermaid block to.
const fence = `<code class="language-mermaid">`

// Uses reports whether rendered markdown holds a diagram. It looks at goldmark's
// output rather than the source, so a fence quoted inside another code block
// does not count.
func Uses(renderedMarkdown string) bool { return strings.Contains(renderedMarkdown, fence) }

// Script carries the renderer once per export, then replaces each fence with
// its diagram. It goes at the end of the body: it runs while the page parses,
// so the images it adds hold the load event, and Chrome prints after that.
func Script() string {
	js, _ := files.ReadFile("export-bootstrap.js")
	return "<script>" + string(Bytes()) + "</script><script>" + string(js) + "</script>"
}
