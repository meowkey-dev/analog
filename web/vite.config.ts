import { defineConfig, type Plugin } from "vite";
import react from "@vitejs/plugin-react";
import { readFileSync } from "node:fs";
import type { IncomingMessage, ServerResponse } from "node:http";

const plotlyPath = "/vendor/plotly-basic-2.35.2.min.js";
const plotlyFile = new URL("../internal/chartlib/plotly-basic.min.js", import.meta.url);

function serveChartVendor(req: IncomingMessage, res: ServerResponse, next: () => void): void {
  if (req.url?.split("?", 1)[0] !== plotlyPath) return next();
  res.setHeader("Content-Type", "text/javascript; charset=utf-8");
  res.setHeader("Cache-Control", "public, max-age=31536000, immutable");
  res.end(readFileSync(plotlyFile));
}

function chartVendor(): Plugin {
  return {
    name: "chart-vendor",
    configureServer(server) {
      server.middlewares.use(serveChartVendor);
    },
    configurePreviewServer(server) {
      server.middlewares.use(serveChartVendor);
    },
  };
}

/**
 * katex ships woff2+woff+ttf for each face. analog-server embeds the bundle,
 * so the ttf copies would have been most of the cost of #68. every browser
 * the UI runs in reads woff2; same reasoning as dropping source maps.
 */
function katexWoff2Only(): Plugin {
  return {
    name: "katex-woff2-only",
    enforce: "pre",
    transform(code, id) {
      if (!id.includes("katex") || !id.includes(".css")) return;
      return {
        code: code
          .replace(/,url\([^)]+\.woff\) format\("woff"\)/g, "")
          .replace(/,url\([^)]+\.ttf\) format\("truetype"\)/g, ""),
        map: null,
      };
    },
  };
}

// The API is bound to 127.0.0.1:8787 (contracts/openapi.json servers[0]). Proxying
// /api through the dev server keeps the app same-origin in development, which means
// the sandboxed-iframe reasoning in SPEC §5 holds in dev exactly as it does in prod.
export default defineConfig({
  plugins: [react(), katexWoff2Only(), chartVendor()],
  // WP3/WP4 render contracts/fixtures/ with no server behind them (?fixture),
  // which means importing JSON from outside web/.
  server: {
    fs: { allow: [".."] },
    port: 5173,
    strictPort: true,
    proxy: {
      // SSE passes through unbuffered; the server sets Cache-Control: no-cache
      // and X-Accel-Buffering: no itself.
      "/api": { target: "http://127.0.0.1:8787", changeOrigin: false },
    },
  },
  // "hidden": the map is written for debugging a production build locally, but the
  // bundle carries no sourceMappingURL comment. The server does not embed the map
  // (scripts/build.sh), so a comment would make devtools fetch a file that is not
  // there, hit the SPA fallback, and complain about parsing index.html as a map.
  build: { outDir: "dist", sourcemap: "hidden" },
});
