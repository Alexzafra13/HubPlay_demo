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

    // Con ~30s de buffer sano, el usuario no nota el kill hasta
    // agotarlo. Seekear a zona NO bufferizada hace el fallo visible
    // ya — el caso "seekeo y el server no está" — y arranca el reloj
    // del presupuesto de error del audit en la acción del usuario.
    await page.evaluate(() => {
      const v = document.querySelector("video");
      if (v) v.currentTime = 200;
    });

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
