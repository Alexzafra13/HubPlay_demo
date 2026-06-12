// Smoke (c) del audit 2026-06-10: cambiar la pista de audio (dub) en
// mitad de la reproducción mantiene la posición. El switch estilo
// Jellyfin re-emite el master con `?audio=N` (sesión nueva de
// transcode con la pista elegida — PB-6) y reanuda en el playhead;
// antes de PB-6 esto podía acabar en vídeo mudo o reinicio desde cero.
import { test, expect } from "@playwright/test";
import { HubplayServer, binaryPath } from "./helpers/server";
import { loginViaUI, openPlayer, waitForPlayback, videoTime } from "./helpers/player";

test.describe("smoke: cambio de dub mid-play", () => {
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

  test("seleccionar otra pista mantiene la posición", async ({ page }) => {
    await loginViaUI(page, server);
    await openPlayer(page, server, movieId);

    // Deja avanzar el playhead lo bastante como para que un reinicio
    // desde cero sea distinguible de un resume.
    await waitForPlayback(page, 8);
    const before = await videoTime(page);

    // Menú de audio: el fixture trae eng (default) + spa, así que el
    // picker DB-driven muestra "English · …" y "Español · …".
    await page.mouse.move(640, 360);
    await page.getByRole("button", { name: "Audio" }).click();

    // El switch re-emite el master con ?audio=1 — esperar esa request
    // distingue "el switch ocurrió" de "el vídeo viejo sigue sonando".
    const switched = page.waitForRequest((req) => req.url().includes("audio=1"), {
      timeout: 30_000,
    });
    await page.getByRole("button", { name: /Español/ }).click();
    await switched;

    // La sesión nueva debe reanudar cerca del playhead: ni desde cero
    // (lo que rompía antes) ni saltando hacia delante.
    await waitForPlayback(page, Math.max(0.5, before - 5));
    await expect
      .poll(() => videoTime(page), { timeout: 30_000 })
      .toBeGreaterThan(before - 5);
    const after = await videoTime(page);
    expect(after).toBeLessThan(before + 30);

    // Y sigue reproduciendo de verdad (el playhead avanza).
    await waitForPlayback(page, after + 1);
  });
});
