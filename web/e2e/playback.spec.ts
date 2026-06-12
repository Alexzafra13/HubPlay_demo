// Smoke (a) del audit 2026-06-10: login → play → primer frame → seek
// lejano → cerrar → resume en la posición correcta.
//
// La película es un MKV h264+aac: el server decide DirectStream y todo
// pasa por la cadena HLS real (manager de sesiones, manifest VOD
// sintético, seek-restart de ffmpeg). jsdom no puede ver nada de esto —
// exactamente el hueco que motivó el smoke E2E (gap de test nº 7).
import { test, expect } from "@playwright/test";
import { HubplayServer, binaryPath } from "./helpers/server";
import { loginViaUI, openPlayer, waitForPlayback, videoTime } from "./helpers/player";

const SEEK_TARGET = 120; // segundos — fuera de lo codificado al arrancar

test.describe("smoke: reproducción VOD", () => {
  let server: HubplayServer;
  let movieId = "";

  test.beforeAll(async () => {
    server = new HubplayServer(binaryPath());
    await server.start();
    await server.provision({ movies: 1, episodes: 2 });
    movieId = await server.findItemId(server.movieLibraryId, "Test Movie", "movie");
  });

  test.afterAll(async () => {
    await server?.stop();
  });

  test("play → primer frame → seek → cerrar → resume", async ({ page }) => {
    await loginViaUI(page, server);

    // Play + primer frame.
    await openPlayer(page, server, movieId);
    await waitForPlayback(page, 0.5);

    // Seek lejano: dispara el seek-restart del transcoder (el segmento
    // pedido no existe aún en disco). Programático sobre el <video> —
    // es lo mismo que hace la barra de seek, sin depender de geometría.
    await page.evaluate((target) => {
      const v = document.querySelector("video");
      if (v) v.currentTime = target;
    }, SEEK_TARGET);
    await waitForPlayback(page, SEEK_TARGET + 1);

    // Pausa (el reporter de progreso guarda en pause — PB-18) y respiro
    // para que el POST de progreso aterrice antes de cerrar.
    await page.evaluate(() => document.querySelector("video")?.pause());
    await page.waitForTimeout(1_000);

    // Cierra el player con su botón Atrás (los controles se revelan
    // con el movimiento del ratón).
    await page.mouse.move(640, 360);
    await page.getByRole("button", { name: "Atrás" }).click();
    await expect(page.locator("video")).toBeHidden({ timeout: 10_000 });

    // Reabre: debe reanudar cerca del punto del seek, no desde cero.
    await openPlayer(page, server, movieId);
    await waitForPlayback(page, 0.5);
    await expect
      .poll(() => videoTime(page), { timeout: 30_000 })
      .toBeGreaterThan(SEEK_TARGET - 15);
  });
});
