// Arranque + aprovisionamiento de un servidor HubPlay real para los
// smoke E2E. Cada spec lanza SU PROPIA instancia (data dir y puerto
// propios) apuntando a los fixtures compartidos de media.ts — así el
// smoke de "servidor caído" puede matar su proceso sin afectar al
// resto, y no hay estado compartido entre specs.
//
// El aprovisionamiento va por API pura (sin UI): el wizard de setup
// acepta requests sin auth mientras no haya usuarios, y el resto usa
// `Authorization: Bearer` (que además exime del double-submit CSRF —
// la validación solo aplica con cookie `hubplay_access`).
import { spawn, type ChildProcess } from "node:child_process";
import { mkdtemp, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import net from "node:net";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { MOVIES_DIR, SHOWS_DIR } from "./media";

const HERE = path.dirname(fileURLToPath(import.meta.url));

export const ADMIN_USER = "admin";
export const ADMIN_PASSWORD = "E2eSmoke!2026";

/** Puerto libre del SO: bind a 0 y leer el asignado. */
async function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const srv = net.createServer();
    srv.listen(0, "127.0.0.1", () => {
      const address = srv.address();
      if (address && typeof address === "object") {
        const port = address.port;
        srv.close(() => resolve(port));
      } else {
        srv.close(() => reject(new Error("no port assigned")));
      }
    });
    srv.on("error", reject);
  });
}

interface JsonResponse {
  status: number;
  body: Record<string, unknown>;
}

export class HubplayServer {
  readonly binPath: string;
  port = 0;
  baseURL = "";
  private proc: ChildProcess | null = null;
  private workDir = "";
  private accessToken = "";
  movieLibraryId = "";
  showLibraryId = "";

  constructor(binPath: string) {
    this.binPath = binPath;
  }

  /** Arranca el binario con config mínima en un dir temporal. */
  async start(): Promise<void> {
    this.workDir = await mkdtemp(path.join(tmpdir(), "hubplay-e2e-"));
    this.port = await freePort();
    this.baseURL = `http://127.0.0.1:${this.port}`;

    const configPath = path.join(this.workDir, "hubplay.yaml");
    const config = [
      "server:",
      '  bind: "127.0.0.1"',
      `  port: ${this.port}`,
      "database:",
      '  driver: "sqlite"',
      `  path: "${path.join(this.workDir, "hubplay.db")}"`,
      "logging:",
      '  level: "warn"',
      "streaming:",
      `  cache_dir: "${path.join(this.workDir, "transcode")}"`,
      "mdns:",
      "  enabled: false",
      "",
    ].join("\n");
    await writeFile(configPath, config);

    this.proc = spawn(this.binPath, ["--config", configPath], {
      stdio: ["ignore", "pipe", "pipe"],
    });
    // El stderr del server es la única pista cuando un smoke falla por
    // backend; volcarlo con prefijo mantiene el log de playwright legible.
    this.proc.stderr?.on("data", (chunk: Buffer) => {
      process.stderr.write(`[hubplay:${this.port}] ${chunk}`);
    });

    await this.waitHealthy();
  }

  private async waitHealthy(timeoutMs = 30_000): Promise<void> {
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
      if (this.proc?.exitCode != null) {
        throw new Error(`hubplay exited early with code ${this.proc.exitCode}`);
      }
      try {
        const res = await fetch(`${this.baseURL}/api/v1/setup/status`);
        if (res.ok) return;
      } catch {
        // aún arrancando
      }
      await new Promise((r) => setTimeout(r, 250));
    }
    throw new Error("hubplay did not become healthy in time");
  }

  private async request(
    method: string,
    apiPath: string,
    body?: unknown,
  ): Promise<JsonResponse> {
    const headers: Record<string, string> = { "Content-Type": "application/json" };
    if (this.accessToken) headers["Authorization"] = `Bearer ${this.accessToken}`;
    const res = await fetch(`${this.baseURL}${apiPath}`, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    let parsed: Record<string, unknown> = {};
    try {
      parsed = (await res.json()) as Record<string, unknown>;
    } catch {
      // respuestas vacías (204) son válidas
    }
    return { status: res.status, body: parsed };
  }

  private expectOk(label: string, res: JsonResponse): Record<string, unknown> {
    if (res.status < 200 || res.status >= 300) {
      throw new Error(`${label} failed: HTTP ${res.status} ${JSON.stringify(res.body)}`);
    }
    return (res.body.data ?? {}) as Record<string, unknown>;
  }

  /**
   * Wizard completo por API: admin + bibliotecas + complete(start_scan)
   * y espera a que el scan deje los items contados en cada biblioteca.
   */
  async provision(opts: { movies: number; episodes: number }): Promise<void> {
    const setup = this.expectOk(
      "auth/setup",
      await this.request("POST", "/api/v1/auth/setup", {
        username: ADMIN_USER,
        password: ADMIN_PASSWORD,
        display_name: "E2E Admin",
      }),
    );
    this.accessToken = String(setup.access_token ?? "");
    if (!this.accessToken) throw new Error("auth/setup returned no access_token");

    const libs = this.expectOk(
      "setup/libraries",
      await this.request("POST", "/api/v1/setup/libraries", {
        libraries: [
          { name: "Movies", content_type: "movies", paths: [MOVIES_DIR] },
          { name: "Shows", content_type: "shows", paths: [SHOWS_DIR] },
        ],
      }),
    ) as unknown as Array<Record<string, unknown>> | Record<string, unknown>;

    // El endpoint de setup serializa el struct de dominio sin tags →
    // claves PascalCase (ID, ContentType). Tolerar ambas formas.
    const created = Array.isArray(libs)
      ? libs
      : ((libs as Record<string, unknown>).libraries as Array<Record<string, unknown>>);
    for (const lib of created ?? []) {
      const id = String(lib.id ?? lib.ID ?? "");
      const contentType = String(lib.content_type ?? lib.ContentType ?? "");
      if (contentType === "movies") this.movieLibraryId = id;
      if (contentType === "shows") this.showLibraryId = id;
    }
    if (!this.movieLibraryId || !this.showLibraryId) {
      throw new Error(`setup/libraries response missing ids: ${JSON.stringify(libs)}`);
    }

    this.expectOk(
      "setup/complete",
      await this.request("POST", "/api/v1/setup/complete", { start_scan: true }),
    );

    await this.waitForItems(this.movieLibraryId, "movie", opts.movies);
    await this.waitForItems(this.showLibraryId, "episode", opts.episodes);
  }

  /** Polling de items hasta alcanzar el count esperado. */
  private async waitForItems(
    libraryId: string,
    type: string,
    expected: number,
    timeoutMs = 60_000,
  ): Promise<void> {
    const deadline = Date.now() + timeoutMs;
    let last = 0;
    while (Date.now() < deadline) {
      const items = await this.listItems(libraryId, type);
      last = items.length;
      if (last >= expected) return;
      await new Promise((r) => setTimeout(r, 500));
    }
    throw new Error(
      `library ${libraryId} scan: expected ${expected} ${type} items, got ${last}`,
    );
  }

  async listItems(
    libraryId: string,
    type?: string,
  ): Promise<Array<Record<string, unknown>>> {
    const qs = type ? `?limit=100&type=${type}` : "?limit=100";
    const res = await this.request("GET", `/api/v1/libraries/${libraryId}/items${qs}`);
    const data = this.expectOk("library items", res);
    return (data.items as Array<Record<string, unknown>>) ?? [];
  }

  /** Id del primer item cuyo título contiene `title` (case-insensitive). */
  async findItemId(libraryId: string, title: string, type?: string): Promise<string> {
    const items = await this.listItems(libraryId, type);
    const hit = items.find((i) =>
      String(i.title ?? "").toLowerCase().includes(title.toLowerCase()),
    );
    if (!hit) {
      const titles = items.map((i) => i.title).join(", ");
      throw new Error(`item "${title}" not found in library ${libraryId}; have: ${titles}`);
    }
    return String(hit.id);
  }

  /** Muerte abrupta — el smoke de "backend caído mid-play". */
  killHard(): void {
    this.proc?.kill("SIGKILL");
    this.proc = null;
  }

  /** Parada limpia + limpieza del workdir. */
  async stop(): Promise<void> {
    if (this.proc) {
      const proc = this.proc;
      this.proc = null;
      proc.kill("SIGTERM");
      await new Promise<void>((resolve) => {
        const t = setTimeout(() => {
          proc.kill("SIGKILL");
          resolve();
        }, 5_000);
        proc.once("exit", () => {
          clearTimeout(t);
          resolve();
        });
      });
    }
    if (this.workDir) {
      await rm(this.workDir, { recursive: true, force: true }).catch(() => {});
    }
  }
}

/** Ruta del binario bajo test: env o default del global-setup. */
export function binaryPath(): string {
  return process.env.HUBPLAY_E2E_BIN ?? path.resolve(HERE, "..", ".tmp", "hubplay");
}
