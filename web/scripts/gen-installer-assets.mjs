// Genera los assets del installer Windows desde el SVG del logo:
//   - scripts/hubplay.ico                (multi-res 16/32/48/64/128/256)
//   - scripts/wizard-large.png           (banner lateral del wizard)
//   - scripts/wizard-small.png           (icono cabecera del wizard)
//
// Ejecutar manualmente cuando cambie el logo:
//   cd web && pnpm gen:installer-assets

import sharp from "sharp";
import toIco from "to-ico";
import { readFile, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import path from "node:path";

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, "../..");

const SVG = path.join(repoRoot, "web/public/hubplay_icon_mark.svg");
const OUT_ICO = path.join(repoRoot, "scripts/hubplay.ico");
const OUT_LARGE = path.join(repoRoot, "scripts/wizard-large.png");
const OUT_SMALL = path.join(repoRoot, "scripts/wizard-small.png");

// Color de fondo del logo (rect en el SVG: fill="#0d1220").
const BG = { r: 0x0d, g: 0x12, b: 0x20, alpha: 1 };

async function svgPng(size) {
  const buf = await readFile(SVG);
  return sharp(buf, { density: 384 })
    .resize(size, size, { fit: "contain", background: { ...BG, alpha: 0 } })
    .png()
    .toBuffer();
}

async function genIco() {
  const sizes = [16, 32, 48, 64, 128, 256];
  const pngs = await Promise.all(sizes.map(svgPng));
  const ico = await toIco(pngs);
  await writeFile(OUT_ICO, ico);
  console.log(`✓ ${OUT_ICO} (${sizes.join(",")})`);
}

async function genWizardLarge() {
  // Banner vertical del wizard. Tamaño 3x del mínimo (164x314) para
  // que Inno haga downscale nítido en pantallas hi-dpi.
  const W = 492;
  const H = 942;
  const iconSize = 280;
  const icon = await svgPng(iconSize);
  const out = await sharp({
    create: { width: W, height: H, channels: 4, background: BG },
  })
    .composite([
      {
        input: icon,
        left: Math.round((W - iconSize) / 2),
        top: Math.round(H * 0.32),
      },
    ])
    .png()
    .toBuffer();
  await writeFile(OUT_LARGE, out);
  console.log(`✓ ${OUT_LARGE} (${W}x${H})`);
}

async function genWizardSmall() {
  // Cabecera del wizard. 3x del mínimo (55x58). Lo dejamos cuadrado y
  // que Inno lo centre en el header.
  const SIZE = 174;
  const icon = await svgPng(Math.round(SIZE * 0.78));
  const out = await sharp({
    create: { width: SIZE, height: SIZE, channels: 4, background: BG },
  })
    .composite([
      {
        input: icon,
        gravity: "center",
      },
    ])
    .png()
    .toBuffer();
  await writeFile(OUT_SMALL, out);
  console.log(`✓ ${OUT_SMALL} (${SIZE}x${SIZE})`);
}

await Promise.all([genIco(), genWizardLarge(), genWizardSmall()]);
