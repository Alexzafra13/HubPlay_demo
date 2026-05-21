import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { VitePWA } from "vite-plugin-pwa";
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
  plugins: [
    react({
      // Activa el React Compiler (optimización automática de React 19):
      // memoiza componentes y hooks sin que el usuario tenga que envolver
      // a mano con useMemo / useCallback / React.memo. El healthcheck
      // confirma 533/533 componentes compatibles; el plugin
      // `eslint-plugin-react-compiler` (en eslint.config.js) garantiza
      // que cada PR siga siéndolo.
      babel: {
        plugins: [['babel-plugin-react-compiler', {}]],
      },
    }),
    tailwindcss(),
    // PWA: hace que el frontend sea instalable como app nativa
    // (icono en escritorio/menú, ventana standalone sin barra del
    // navegador, funciona offline en lo cacheable). Los iconos PNG
    // de public/ vienen pre-generados desde public/hubplay_icon_mark.svg
    // con `pnpm gen:pwa-assets` — regenera tras cambiar el logo.
    //
    // injectRegister: 'auto' añade el snippet de registro del SW al
    // build automáticamente; no hay que tocar src/main.tsx.
    //
    // workbox.navigateFallback: cualquier ruta no encontrada cae a
    // index.html — necesario para SPA routing (React Router) cuando
    // el usuario abre la app desde el icono y la última URL era
    // /libraries/foo/.
    VitePWA({
      registerType: "autoUpdate",
      injectRegister: "auto",
      includeAssets: [
        "favicon.ico",
        "apple-touch-icon-180x180.png",
        "hubplay_icon_mark.svg",
      ],
      manifest: {
        name: "HubPlay",
        short_name: "HubPlay",
        description: "Servidor de media self-hosted",
        theme_color: "#0a0e17",
        background_color: "#0a0e17",
        display: "standalone",
        scope: "/",
        start_url: "/",
        lang: "es",
        icons: [
          {
            src: "pwa-64x64.png",
            sizes: "64x64",
            type: "image/png",
          },
          {
            src: "pwa-192x192.png",
            sizes: "192x192",
            type: "image/png",
          },
          {
            src: "pwa-512x512.png",
            sizes: "512x512",
            type: "image/png",
          },
          {
            src: "maskable-icon-512x512.png",
            sizes: "512x512",
            type: "image/png",
            purpose: "maskable",
          },
        ],
      },
      workbox: {
        navigateFallback: "/index.html",
        // No precachear el HTML del backend — el SW podría servir
        // una index.html stale en arranques con server nuevo. Sólo
        // assets estáticos (JS/CSS/imágenes/fonts) entran al cache.
        globPatterns: ["**/*.{js,css,png,svg,ico,woff2}"],
        // El workbox runtime es ~12KB añadidos al bundle; aceptable
        // por la ganancia UX. Si en algún momento el bundle pesa,
        // este es el primer candidato a poner detrás de un flag.
        cleanupOutdatedCaches: true,
      },
      // En dev mode el SW está deshabilitado por defecto — evita
      // que un SW cacheado interfiera con HMR.
      devOptions: {
        enabled: false,
      },
    }),
    preserveGitkeep,
  ],
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
        target: process.env.VITE_API_TARGET ?? "http://localhost:8096",
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
          // hls.js is ~400 KB minified and only matters on routes
          // that touch the player. Splitting keeps it out of the
          // initial bundle so Home / Browse / Login skip the parse
          // cost on first paint; the chunk fetches lazily when the
          // detail page mounts the player.
          hls: ["hls.js"],
        },
      },
    },
  },
});
