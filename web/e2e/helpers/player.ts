// Helpers de player compartidos por los smoke specs.
import { expect, type Page } from "@playwright/test";
import { ADMIN_PASSWORD, ADMIN_USER, type HubplayServer } from "./server";

/**
 * Login por la UI real (el smoke también cubre el form de login).
 * Deja la sesión en cookies HTTP-only + el user en localStorage.
 */
export async function loginViaUI(page: Page, server: HubplayServer): Promise<void> {
  await page.goto(`${server.baseURL}/login`);
  await page.locator('input[autocomplete="username"]').fill(ADMIN_USER);
  await page.locator('input[autocomplete="current-password"]').fill(ADMIN_PASSWORD);
  await page.locator('button[type="submit"]').click();
  await page.waitForURL((url) => !url.pathname.startsWith("/login"), {
    timeout: 15_000,
  });
}

/**
 * Abre el player vía deep-link `?play=1` (la misma ruta que usan el
 * hero de la home y Continue Watching).
 */
export async function openPlayer(page: Page, server: HubplayServer, itemId: string): Promise<void> {
  await page.goto(`${server.baseURL}/items/${itemId}?play=1`);
  await expect(page.locator("video")).toBeVisible({ timeout: 30_000 });
}

/**
 * Espera reproducción REAL: currentTime avanzando más allá de
 * `beyondSeconds` con datos decodificados (readyState >= 2). Es la
 * definición de "primer frame" del audit — un spinner eterno o un
 * elemento parado fallan aquí.
 */
export async function waitForPlayback(
  page: Page,
  beyondSeconds: number,
  timeoutMs = 60_000,
): Promise<void> {
  await page.waitForFunction(
    (min) => {
      const v = document.querySelector("video");
      return !!v && v.readyState >= 2 && v.currentTime > min;
    },
    beyondSeconds,
    { timeout: timeoutMs },
  );
}

/** currentTime actual del `<video>` del player. */
export async function videoTime(page: Page): Promise<number> {
  return page.evaluate(() => document.querySelector("video")?.currentTime ?? -1);
}
