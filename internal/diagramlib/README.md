# Diagram vendor asset

`pele.global.js` is `dist/pele.global.js` from
[`pele@1.9.0`](https://www.npmjs.com/package/pele/v/1.9.0), the Mermaid renderer
the web UI uses for ` ```mermaid ` fences in md cards. The adjacent `LICENSE` is
from that package. SHA-256:

```
323c16de7844d4b609743f70449ebce041a531e4f48f23b1e9e7ec9d3b917287
```

The web UI imports the same version from npm (`web/package.json`), where it is
split into one chunk per diagram type; this single-file build is for the CLI
HTML/PDF export, which has no bundler. `diagramlib_test.go` fails if the two
versions drift apart. To upgrade, bump `web/package.json`, copy the new
`dist/pele.global.js` and `LICENSE` here, and update `Version` and `SHA256`.
