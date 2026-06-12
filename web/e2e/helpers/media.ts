// Generación de media de prueba con ffmpeg real. Los smoke E2E
// necesitan ficheros que el scanner reconozca y que el pipeline de
// transcode pueda masticar — testsrc2 + tonos sine, h264+aac, baja
// resolución para que la generación y el transcode sean rápidos en CI.
//
// Los fixtures se generan UNA vez por invocación de playwright (global
// setup) en `e2e/.tmp/media/` y se comparten entre specs: cada spec
// arranca su propio servidor apuntando a este árbol (solo lectura).
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { mkdir, access } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const run = promisify(execFile);

const HERE = path.dirname(fileURLToPath(import.meta.url));

const MEDIA_ROOT = path.resolve(HERE, "..", ".tmp", "media");
export const MOVIES_DIR = path.join(MEDIA_ROOT, "movies");
export const SHOWS_DIR = path.join(MEDIA_ROOT, "shows");
const LIVE_DIR = path.join(MEDIA_ROOT, "live");
/** Stream "en vivo" sintético para el smoke de Live TV: un MPEG-TS
 *  h264+aac servido por HTTP. Suficientemente largo como para que el
 *  zapping nunca alcance el EOF. */
export const LIVE_TS_FILE = path.join(LIVE_DIR, "channel.ts");
const LIVE_TS_DURATION = 180;

/** Duración de la película de prueba (segundos). Suficiente para
 *  seeks "lejanos" (>1 min) sin que el encode del fixture tarde. */
const MOVIE_DURATION = 300;
/** Duración de cada episodio (segundos). Cortos: el smoke de UpNext
 *  seekea cerca del final y espera el `ended` real. */
export const EPISODE_DURATION = 30;

async function exists(p: string): Promise<boolean> {
  try {
    await access(p);
    return true;
  } catch {
    return false;
  }
}

/**
 * Genera un vídeo sintético h264+aac. `audioTracks` > 1 añade pistas
 * adicionales (tonos distintos) con metadata de idioma — base para el
 * smoke de cambio de dub.
 */
async function generateVideo(
  outPath: string,
  seconds: number,
  audioTracks = 1,
): Promise<void> {
  if (await exists(outPath)) return; // cache entre runs locales
  await mkdir(path.dirname(outPath), { recursive: true });

  const args = ["-y", "-f", "lavfi", "-i", `testsrc2=size=320x180:rate=10:duration=${seconds}`];
  const langs = ["eng", "spa", "fre"];
  for (let i = 0; i < audioTracks; i++) {
    args.push("-f", "lavfi", "-i", `sine=frequency=${440 * (i + 1)}:duration=${seconds}`);
  }
  args.push("-map", "0:v");
  for (let i = 0; i < audioTracks; i++) {
    args.push("-map", `${i + 1}:a`);
  }
  args.push(
    "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
    "-c:a", "aac", "-b:a", "64k", "-ac", "2",
  );
  for (let i = 0; i < audioTracks; i++) {
    args.push(`-metadata:s:a:${i}`, `language=${langs[i] ?? "und"}`);
  }
  args.push("-shortest", outPath);

  await run("ffmpeg", args, { timeout: 120_000 });
}

/** Genera el árbol completo de fixtures. Idempotente.
 *
 *  La película va en MKV a propósito: el navegador no demuxea
 *  Matroska, así que el server decide DirectStream (remux HLS) y el
 *  smoke ejercita la cadena completa de sesiones — manifest VOD
 *  sintético, segmentos bajo demanda y seek-restart — sin el coste de
 *  un re-encode. Los episodios van en MP4 (DirectPlay): para UpNext lo
 *  que importa es el `ended` → auto-advance, no el transcoder. */
export async function generateFixtures(): Promise<void> {
  await generateVideo(
    path.join(MOVIES_DIR, "Test Movie (2020)", "Test Movie (2020).mkv"),
    MOVIE_DURATION,
    2,
  );
  await generateVideo(
    path.join(SHOWS_DIR, "Test Show", "Season 01", "Test Show - S01E01 - Pilot.mp4"),
    EPISODE_DURATION,
  );
  await generateVideo(
    path.join(SHOWS_DIR, "Test Show", "Season 01", "Test Show - S01E02 - Second.mp4"),
    EPISODE_DURATION,
  );
  // La extensión .ts hace que ffmpeg muxee MPEG-TS (Annex-B), el
  // formato que el transmux de IPTV espera de un upstream típico.
  await generateVideo(LIVE_TS_FILE, LIVE_TS_DURATION);
}
