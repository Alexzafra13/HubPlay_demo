// Smoke (d) del audit 2026-06-10: zapear 3 canales de Live TV sin
// spinner colgado. Cubre import M3U → transmux (ffmpeg MPEG-TS → HLS
// en vivo) → useLiveHls en el player fullscreen, y el zap desde
// "Canales similares".
//
// El upstream es un servidor HTTP del propio test sirviendo un MPEG-TS
// sintético en loopback. Nota: esto pasa porque el transmux NO valida
// isSafeUpstream (el guard SSRF solo cubre el proxy passthrough) — si
// ese hueco se cierra, este smoke necesitará el knob de config que lo
// acompañe (p. ej. `iptv.allow_private_upstreams`) para upstreams de
// LAN/loopback. Ver docs/memory/project-status.md (hallazgo 2026-06-12).
import http from "node:http";
import { createReadStream } from "node:fs";
import { test, expect, type Page } from "@playwright/test";
import { LIVE_TS_FILE } from "./helpers/media";
import { HubplayServer, binaryPath } from "./helpers/server";
import { loginViaUI } from "./helpers/player";

const CHANNELS = ["Canal Uno", "Canal Dos", "Canal Tres"];

/** Upstream IPTV sintético: /playlist.m3u + /stream/N.ts en loopback. */
function startUpstream(): Promise<{ url: string; close: () => void }> {
  const server = http.createServer((req, res) => {
    if (req.url === "/playlist.m3u") {
      const base = `http://${req.headers.host}`;
      const lines = ["#EXTM3U"];
      CHANNELS.forEach((name, i) => {
        // Mismo group-title para que los tres caigan en la misma
        // categoría → aparecen juntos en "Canales similares" (el zap).
        lines.push(`#EXTINF:-1 tvg-id="e2e${i + 1}" group-title="Noticias",${name}`);
        lines.push(`${base}/stream/${i + 1}.ts`);
      });
      res.writeHead(200, { "Content-Type": "application/x-mpegurl" });
      res.end(lines.join("\n") + "\n");
      return;
    }
    if (req.url?.endsWith(".ts")) {
      res.writeHead(200, { "Content-Type": "video/mp2t" });
      createReadStream(LIVE_TS_FILE).pipe(res);
      return;
    }
    res.writeHead(404).end();
  });
  return new Promise((resolve) => {
    server.listen(0, "127.0.0.1", () => {
      const addr = server.address();
      const port = typeof addr === "object" && addr ? addr.port : 0;
      resolve({
        url: `http://127.0.0.1:${port}`,
        close: () => server.close(),
      });
    });
  });
}

/** El <video> del canal (ChannelPlayer lo etiqueta con el nombre)
 *  reproduce de verdad — la definición de "sin spinner colgado". */
async function waitChannelPlaying(page: Page, name: string): Promise<void> {
  await page.waitForFunction(
    (n) => {
      const v = document.querySelector(`video[aria-label="${n}"]`);
      return v instanceof HTMLVideoElement && v.readyState >= 2 && v.currentTime > 0.5;
    },
    name,
    { timeout: 30_000 },
  );
}

test.describe("smoke: zapping Live TV", () => {
  let server: HubplayServer;
  let upstream: { url: string; close: () => void };

  test.beforeAll(async () => {
    upstream = await startUpstream();
    server = new HubplayServer(binaryPath());
    await server.start();
    await server.provision({ movies: 1, episodes: 2 });
    await server.provisionLiveTV(`${upstream.url}/playlist.m3u`, CHANNELS.length);
  });

  test.afterAll(async () => {
    await server?.stop();
    upstream?.close();
  });

  test("zapear 3 canales sin spinner colgado", async ({ page }) => {
    await loginViaUI(page, server);
    await page.goto(`${server.baseURL}/live-tv`);

    // El hero spotlight elige un canal y su botón abre el fullscreen.
    const heroButton = page
      .getByRole("button", { name: /^Canal (Uno|Dos|Tres)/ })
      .first();
    await expect(heroButton).toBeVisible({ timeout: 20_000 });
    const heroLabel = (await heroButton.getAttribute("aria-label")) ?? "";
    const first = CHANNELS.find((c) => heroLabel.startsWith(c)) ?? CHANNELS[0];
    await heroButton.click();

    // Fullscreen: el primer canal reproduce (transmux cold-start).
    const overlay = page.getByRole("dialog", { name: /EN VIVO/ });
    await expect(overlay).toBeVisible({ timeout: 10_000 });
    await waitChannelPlaying(page, first);

    // Zap a los otros dos desde "Canales similares". Cada salto
    // arranca otra sesión de transmux — el spinner debe resolverse
    // en frío en segundos, no quedarse colgado (PB-27/28).
    const others = CHANNELS.filter((c) => c !== first);
    await overlay.getByRole("tab", { name: /Canales similares/ }).click();
    for (const name of others) {
      await overlay.getByRole("button", { name, exact: false }).click();
      await waitChannelPlaying(page, name);
    }
  });
});
