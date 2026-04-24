import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { writeFileSync } from "node:fs";
import path from "path";

/**
 * Vite empties `dist/` before every build (default `emptyOutDir: true`),
 * which wipes the `.gitkeep` sentinel we use to keep the directory
 * tracked in git. The Go backend embeds `web/dist` with `//go:embed
 * all:web/dist`, so the directory must exist on a fresh clone for
 * `go build` to compile. This tiny plugin re-writes `.gitkeep` after
 * each build so the sentinel survives across dev loops.
 */
const preserveGitkeep = {
  name: "hubplay-preserve-gitkeep",
  closeBundle() {
    const path = `${__dirname}/dist/.gitkeep`;
    writeFileSync(
      path,
      "# Sentinel — keeps web/dist/ tracked so go:embed compiles on a fresh\n" +
        "# clone. Rebuild real assets with `pnpm build` or `make web`.\n",
    );
  },
};

export default defineConfig({
  plugins: [react(), tailwindcss(), preserveGitkeep],
  test: {
    globals: true,
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    include: ["src/**/*.test.{ts,tsx}"],
    css: false,
  },
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    port: 3000,
    proxy: {
      "/api": {
        target: "http://localhost:8096",
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: "dist",
    sourcemap: false,
    rollupOptions: {
      output: {
        manualChunks: {
          react: ["react", "react-dom"],
          router: ["react-router"],
          query: ["@tanstack/react-query"],
        },
      },
    },
  },
});
