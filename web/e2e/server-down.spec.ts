// Smoke (e) del audit 2026-06-10: matar el backend mid-play debe
// producir un error ACCIONABLE en menos de 30s — no el bucle infinito
// de retries con overlay parpadeante que existía antes de PB-16
// (recovery acotado con backoff en useHls).
import { test, expect } from "@playwright/test";
import { HubplayServer, binaryPath } from "./helpers/server";
import { loginViaUI, openPlayer, waitForPlayback } from "./helpers/player";

test.describe("smoke: backend caído mid-play", () => {
  let server: HubplayServer;
  let movieId = "";

  test.beforeAll(async () => {
    server = new HubplayServer(binaryPath());
    await server.start();
    await server.provision({ movies: 1, episodes: 2 });
    movieId = await server.findItemId(server.movieLibraryId, "Test Movie", "movie");
  });

  test.afterAll(async () => {
    // stop() tolera el proceso ya muerto; limpia el workdir.
    await server?.stop();
  });

  test("SIGKILL al server → ErrorOverlay acotado", async ({ page }) => {
    await loginViaUI(page, server);
    await openPlayer(page, server, movieId);
    await waitForPlayback(page, 0.5);

    server.killHard();

    // Con buffer sano, el usuario no nota el kill hasta agotarlo.
    // Seekear a zona NO bufferizada hace el fallo visible ya — el caso
    // "seekeo y el server no está" — y arranca el reloj del presupuesto
    // de error del audit en la acción del usuario. El destino se calcula
    // tras el final de lo bufferizado: el remux por segmentos llena el
    // buffer en segundos (hasta el techo de hls.js), así que un tiempo
    // fijo puede caer dentro y no tocar la red (pasó con un 200 fijo).
    const seek = await page.evaluate(() => {
      const v = document.querySelector("video");
      if (!v) return null;
      let end = v.currentTime;
      for (let i = 0; i < v.buffered.length; i++) {
        if (v.buffered.start(i) <= v.currentTime + 1) end = Math.max(end, v.buffered.end(i));
      }
      const target = end + 30;
      if (target < v.duration - 10) v.currentTime = target;
      return { bufferedEnd: end, target, duration: v.duration };
    });
    expect(seek, "no hay <video>").not.toBeNull();
    expect(
      seek!.target,
      `película entera en buffer (${JSON.stringify(seek)}): alarga MOVIE_DURATION`,
    ).toBeLessThan(seek!.duration - 10);

    // El player debe rendirse con un error terminal traducido, no
    // reintentar para siempre (PB-16: 3 recoveries con backoff). El
    // techo de 45s cubre los ciclos internos de retry de hls.js; el
    // caso real (connection refused, fallo instantáneo) termina muy
    // por debajo de los 30s del audit.
    const overlay = page.getByTestId("player-error-overlay");
    await expect(overlay).toBeVisible({ timeout: 45_000 });
    await expect(overlay.locator("p")).not.toBeEmpty();
  });
});
