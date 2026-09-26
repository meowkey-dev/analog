// Package chartlib owns the pinned script used by HTML cards and portable exports.
package chartlib

import (
	"embed"
	"encoding/base64"
	"strings"
)

const Path = "/vendor/plotly-basic-2.35.2.min.js"
const SHA256 = "138c2e81014b979dc00867a93da55b7605a17495ee78dd7afb433b7f021dfcfa"

//go:embed plotly-basic.min.js export-bootstrap.js LICENSE
var files embed.FS

func Bytes() []byte {
	data, _ := files.ReadFile("plotly-basic.min.js")
	return data
}

// Uses reports whether a card may need the vendor asset. The export bootstrap
// checks actual script elements, so a mention in prose is harmless.
func Uses(html string) bool { return strings.Contains(html, Path) }

// BootstrapScript carries the library once in an exported document. Each sandboxed
// frame receives the source after it loads; execution stays inside that frame.
func BootstrapScript() string {
	js, _ := files.ReadFile("export-bootstrap.js")
	return "<script>" + strings.Replace(string(js), "__ANALOG_PLOTLY_BASE64__",
		base64.StdEncoding.EncodeToString(Bytes()), 1) + "</script>"
}
