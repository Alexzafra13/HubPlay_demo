// Smoke (d) del audit 2026-06-10: zapear 3 canales de Live TV sin
// spinner colgado. Cubre import M3U → transmux (ffmpeg MPEG-TS → HLS
// en vivo) → useLiveHls en el player fullscreen, y el zap desde
// "Canales similares".
//
// El upstream es un servidor HTTP del propio test sirviendo un MPEG-TS
// sintético en loopback. El guard SSRF del transmux (y del proxy) lo
// bloquearía por defecto; el harness arranca el server con
// `iptv.allow_private_upstreams: true` (ver helpers/server.ts) para
// permitir upstreams de LAN/loopback en este smoke.
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
/** Peticiones a /api/ y errores de consola del test en curso (ver test). */
const netLog: string[] = [];

async function waitChannelPlaying(page: Page, name: string): Promise<void> {
  try {
    await page.waitForFunction(
      (n) => {
        const v = document.querySelector(`video[aria-label="${n}"]`);
        return v instanceof HTMLVideoElement && v.readyState >= 2 && v.currentTime > 0.5;
      },
      name,
      { timeout: 30_000 },
    );
  } catch (err) {
    // Estado del <video> al vencer el plazo: distingue "nunca cargó"
    // (readyState 0, networkState) de "cargó pero no avanza" o de un
    // MediaError del decoder. Sin esto el fallo solo dice "timeout".
    const state = await page.evaluate((n) => {
      const v = document.querySelector(`video[aria-label="${n}"]`);
      if (!(v instanceof HTMLVideoElement)) return { found: false };
      const ranges: string[] = [];
      for (let i = 0; i < v.buffered.length; i++) {
        ranges.push(`${v.buffered.start(i).toFixed(1)}-${v.buffered.end(i).toFixed(1)}`);
      }
      return {
        found: true,
        readyState: v.readyState,
        networkState: v.networkState,
        currentTime: v.currentTime,
        paused: v.paused,
        buffered: ranges.join(","),
        error: v.error ? `${v.error.code} ${v.error.message}` : null,
      };
    }, name);
    const trail = netLog.filter((l) => !l.includes("/events")).slice(-40).join("\n  ");
    throw new Error(`canal "${name}" sin reproducir: ${JSON.stringify(state)}\n  ${trail}`, { cause: err });
  }
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
    // Rastro de red de la API para el diagnóstico de waitChannelPlaying:
    // distingue "el player nunca pidió el manifest" de "el server no
    // contestó" o "contestó con error".
    netLog.length = 0;
    const t0 = Date.now();
    const stamp = () => `${((Date.now() - t0) / 1000).toFixed(1)}s`;
    const short = (u: string) => u.replace(/^https?:\/\/[^/]+/, "");
    page.on("request", (r) => {
      if (r.url().includes("/api/")) netLog.push(`${stamp()} → ${r.method()} ${short(r.url())}`);
    });
    page.on("response", (r) => {
      if (r.url().includes("/api/")) netLog.push(`${stamp()} ← ${r.status()} ${short(r.url())}`);
    });
    page.on("requestfailed", (r) => {
      if (r.url().includes("/api/")) netLog.push(`${stamp()} ✗ ${r.failure()?.errorText} ${short(r.url())}`);
    });
    page.on("console", (m) => {
      if (m.type() === "error" || m.type() === "warning") netLog.push(`${stamp()} console.${m.type()}: ${m.text().slice(0, 200)}`);
    });

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
