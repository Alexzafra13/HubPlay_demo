// Smoke (b) del audit 2026-06-10: fin de episodio → overlay UpNext →
// avance al siguiente episodio. Cubre el wiring nextUp de usePlayback +
// usePlayerOverlays (SHOW_UP_NEXT en `ended`) + el arranque limpio del
// episodio siguiente sin remount del player.
import { test, expect } from "@playwright/test";
import { EPISODE_DURATION } from "./helpers/media";
import { HubplayServer, binaryPath } from "./helpers/server";
import { loginViaUI, openPlayer, waitForPlayback, videoTime } from "./helpers/player";

test.describe("smoke: UpNext entre episodios", () => {
  let server: HubplayServer;
  let ep1Id = "";

  test.beforeAll(async () => {
    server = new HubplayServer(binaryPath());
    await server.start();
    await server.provision({ movies: 1, episodes: 2 });
    // El scanner titula los episodios con el nombre parseado del
    // fichero ("Pilot"), no con el código SxxEyy.
    ep1Id = await server.findItemId(server.showLibraryId, "Pilot", "episode");
  });

  test.afterAll(async () => {
    await server?.stop();
  });

  test("ended → UpNext → reproducir siguiente", async ({ page }) => {
    await loginViaUI(page, server);
    await openPlayer(page, server, ep1Id);
    await waitForPlayback(page, 0.5);

    // Salta cerca del final y deja que el episodio TERMINE de verdad —
    // el overlay se dispara con el evento `ended`, no por proximidad.
    await page.evaluate((dur) => {
      const v = document.querySelector("video");
      if (v) v.currentTime = dur - 3;
    }, EPISODE_DURATION);

    const overlay = page.getByTestId("upnext-overlay");
    await expect(overlay).toBeVisible({ timeout: 30_000 });

    // Confirmar en vez de esperar el countdown de 5s: determinista.
    await overlay.getByRole("button", { name: /Reproducir en/ }).click();
    await expect(overlay).toBeHidden({ timeout: 10_000 });

    // El siguiente episodio arranca desde el principio y reproduce.
    await waitForPlayback(page, 0.5);
    const t = await videoTime(page);
    expect(t).toBeGreaterThan(0.2);
    expect(t).toBeLessThan(EPISODE_DURATION - 5);
  });
});
