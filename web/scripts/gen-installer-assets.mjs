// Genera los assets del installer Windows desde el SVG del logo:
//   - scripts/hubplay.ico                (multi-res 16/32/48/64/128/256)
//   - scripts/wizard-large.png           (banner lateral del wizard)
//   - scripts/wizard-small.png           (icono cabecera del wizard)
//
// Ejecutar manualmente cuando cambie el logo:
//   cd web && pnpm gen:installer-assets

import sharp from "sharp";
import pngToIco from "png-to-ico";
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
  // png-to-ico acepta un array de Buffers PNG (firma compatible con la
  // que usaba `to-ico`). Swap motivado por seguridad: `to-ico` arrastra
  // un chain de deps deprecated (jimp 0.2.28 → resize-img → request →
  // form-data/qs/tough-cookie/minimist) que reportaban 5 alertas
  // Dependabot, una de severidad crítica (form-data unsafe random).
  // `png-to-ico` tiene 3 deps directas (pngjs, minimist@^1.2.8 ya
  // parcheado, @types/node) — chain limpia.
  const sizes = [16, 32, 48, 64, 128, 256];
  const pngs = await Promise.all(sizes.map(svgPng));
  const ico = await pngToIco(pngs);
  await writeFile(OUT_ICO, ico);
  console.log(`✓ ${OUT_ICO} (${sizes.join(",")})`);
}

async function genWizardLarge() {
  // Banner vertical del wizard (3x del mínimo 164x314 para downscale nítido).
  // Composición: icono → "HubPlay" → tagline, sobre fondo del logo con un
  // sutil degradado para que el banner respire.
  const W = 492;
  const H = 942;
  const iconSize = 260;
  const icon = await svgPng(iconSize);

  const overlay = `
<svg xmlns="http://www.w3.org/2000/svg" width="${W}" height="${H}">
  <defs>
    <linearGradient id="g" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0%" stop-color="#1a2238" stop-opacity="0.6"/>
      <stop offset="60%" stop-color="#0d1220" stop-opacity="0"/>
    </linearGradient>
  </defs>
  <rect width="${W}" height="${H}" fill="url(#g)"/>
  <text x="${W / 2}" y="${H * 0.62}" text-anchor="middle"
        font-family="Segoe UI, Helvetica, Arial, sans-serif"
        font-size="72" font-weight="700" fill="#ffffff"
        letter-spacing="-1">HubPlay</text>
  <text x="${W / 2}" y="${H * 0.68}" text-anchor="middle"
        font-family="Segoe UI, Helvetica, Arial, sans-serif"
        font-size="22" font-weight="400" fill="#a8b3cf"
        letter-spacing="2">SERVIDOR DE MEDIA · SELF-HOSTED</text>
</svg>`;

  const out = await sharp({
    create: { width: W, height: H, channels: 4, background: BG },
  })
    .composite([
      { input: Buffer.from(overlay), top: 0, left: 0 },
      {
        input: icon,
        left: Math.round((W - iconSize) / 2),
        top: Math.round(H * 0.18),
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
