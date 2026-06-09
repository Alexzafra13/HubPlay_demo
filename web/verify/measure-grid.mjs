// Mide en Chromium real cuántas <PosterCard> hay en el DOM mientras se
// recorre toda la lista. Métrica clave: PICO de tarjetas simultáneas.
// Grid acumulativo => el pico crece con N (DOM bloat). Virtualizado =>
// el pico se mantiene acotado aunque scrollHeight cubra los N ítems.
import { chromium } from "playwright-core";

const EXEC = process.env.PW_CHROME || "/opt/pw-browsers/chromium-1194/chrome-linux/chrome";
const COUNT = process.env.COUNT || "5000";
const PORT = process.env.PORT || "5188";
const URL = `http://localhost:${PORT}/verify/grid-harness.html?count=${COUNT}`;
const LABEL = process.env.LABEL || "run";
const sel = '[data-testid="poster-card"]';

const browser = await chromium.launch({ executablePath: EXEC, headless: true });
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
await page.goto(URL, { waitUntil: "load" });
await page.waitForSelector(sel, { timeout: 15000 });

const topCards = await page.locator(sel).count();
await page.screenshot({ path: `verify/grid-${LABEL}-top.png` });

// Recorre el documento por pasos de 1200px registrando el pico.
let peak = topCards, lastY = -1, stuck = 0;
for (let i = 0; i < 800 && stuck < 6; i++) {
  const y = await page.evaluate(() => {
    window.scrollBy(0, 1200);
    return Math.round(window.scrollY);
  });
  await page.waitForTimeout(70);
  const c = await page.locator(sel).count();
  if (c > peak) peak = c;
  stuck = y === lastY ? stuck + 1 : 0;
  lastY = y;
}
await page.waitForTimeout(300);

const bottomCards = await page.locator(sel).count();
const scrollHeight = await page.evaluate(() => document.body.scrollHeight);
await page.screenshot({ path: `verify/grid-${LABEL}-bottom.png` });

console.log(JSON.stringify({ label: LABEL, items: Number(COUNT), topCards, peakCards: peak, bottomCards, scrollHeight }, null, 2));
await browser.close();
