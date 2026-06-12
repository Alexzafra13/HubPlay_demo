// Global setup de los smoke E2E: genera los fixtures de media (una
// vez, compartidos por todos los specs) y construye el binario bajo
// test si el caller no apuntó uno vía HUBPLAY_E2E_BIN.
//
// El binario se construye con la SPA embebida (go:embed web/dist), así
// que si `web/dist` no tiene un build real, primero corre `pnpm build`.
// CI puede pre-construir ambos y exportar HUBPLAY_E2E_BIN para saltarse
// este paso.
import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { generateFixtures } from "./helpers/media";

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEB_DIR = path.resolve(HERE, "..");
const REPO_ROOT = path.resolve(WEB_DIR, "..");
const DEFAULT_BIN = path.join(HERE, ".tmp", "hubplay");

export default async function globalSetup(): Promise<void> {
  if (!process.env.CI) {
    // Local: que un `pnpm test:e2e` pelado funcione sin pasos manuales.
    if (!process.env.HUBPLAY_E2E_BIN) {
      if (!existsSync(path.join(WEB_DIR, "dist", "index.html"))) {
        console.log("[e2e] web/dist sin build — corriendo `pnpm build`…");
        execFileSync("pnpm", ["build"], { cwd: WEB_DIR, stdio: "inherit" });
      }
      console.log("[e2e] construyendo binario hubplay…");
      mkdirSync(path.dirname(DEFAULT_BIN), { recursive: true });
      execFileSync("go", ["build", "-o", DEFAULT_BIN, "./cmd/hubplay"], {
        cwd: REPO_ROOT,
        stdio: "inherit",
      });
    }
  } else if (!process.env.HUBPLAY_E2E_BIN) {
    throw new Error("CI debe exportar HUBPLAY_E2E_BIN con el binario pre-construido");
  }

  console.log("[e2e] generando fixtures de media…");
  await generateFixtures();
}
