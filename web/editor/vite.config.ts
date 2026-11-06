import { defineConfig } from "vite";
import { resolve } from "node:path";

// The build is embedded into the Go binary with //go:embed, and served at
// mygo://localhost/. Keep assets relative to that root.
export default defineConfig({
  base: "/",
  build: {
    outDir: resolve(__dirname, "../../internal/app/webassets"),
    emptyOutDir: true,
    target: "safari16",
    assetsInlineLimit: 0,
    cssCodeSplit: false,
    sourcemap: false,
  },
});
