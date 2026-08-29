import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The API is bound to 127.0.0.1:8787 (contracts/openapi.json servers[0]). Proxying
// /api through the dev server keeps the app same-origin in development, which means
// the sandboxed-iframe reasoning in SPEC §5 holds in dev exactly as it does in prod.
export default defineConfig({
  plugins: [react()],
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
  build: { outDir: "dist", sourcemap: true },
});
