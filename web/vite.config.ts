import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { viteSingleFile } from "vite-plugin-singlefile";

// The dashboard ships as ONE self-contained index.html (JS, CSS and fonts
// inlined) written straight into ../cluster/web, which the Go binary embeds
// with go:embed. `go build` therefore keeps working without Node: the built
// file is committed, and CI fails if it is out of date with this source.
export default defineConfig({
  plugins: [react(), tailwindcss(), viteSingleFile()],
  build: {
    outDir: "../cluster/web",
    emptyOutDir: true,
    assetsInlineLimit: Number.MAX_SAFE_INTEGER,
  },
  server: {
    // `npm run dev` talks to a cluster started with `raftkv cluster -port 8080`.
    proxy: { "/api": "http://localhost:8080", "/healthz": "http://localhost:8080" },
  },
});
