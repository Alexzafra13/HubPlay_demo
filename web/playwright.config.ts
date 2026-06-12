// Configuración de los smoke E2E (Playwright). Ver e2e/README.md.
//
// Un solo worker a propósito: cada spec arranca su propio servidor
// HubPlay + ffmpeg, y en runners de CI compartir CPU entre dos
// transcodes paralelos convierte timeouts de player en flakes.
import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e",
  globalSetup: "./e2e/global-setup.ts",
  timeout: 120_000,
  expect: { timeout: 30_000 },
  fullyParallel: false,
  workers: 1,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI
    ? [["list"], ["html", { open: "never" }]]
    : [["list"]],
  use: {
    viewport: { width: 1280, height: 720 },
    // Los specs seleccionan por labels visibles; fijar el idioma del
    // navegador fija el idioma de la UI (i18next detecta navigator
    // language) y evita selectores bilingües.
    locale: "es-ES",
    trace: "retain-on-failure",
    // Los smoke reproducen vídeo de verdad, así que el browser tiene
    // que decodificar H.264/AAC — los builds de Chromium de Playwright
    // (incluido el headless shell) son open-codecs-only y fallan con
    // "could not decode". Usar Chrome real:
    //   - PW_CHROME: ruta a un binario Chrome/Chrome-for-Testing
    //     (misma convención que web/verify/).
    //   - channel chrome: el Chrome del sistema (CI: preinstalado en
    //     ubuntu-latest).
    ...(process.env.PW_CHROME ? {} : { channel: "chrome" }),
    launchOptions: {
      ...(process.env.PW_CHROME ? { executablePath: process.env.PW_CHROME } : {}),
      // El player llama a video.play() sin gesto del usuario; sin este
      // flag la política de autoplay de Chrome lo bloquea en headless.
      args: ["--autoplay-policy=no-user-gesture-required"],
    },
  },
});
