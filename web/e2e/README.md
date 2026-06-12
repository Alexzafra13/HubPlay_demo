# Smoke E2E (Playwright)

Smoke de la cadena de playback **real**: binario de producción con la
SPA embebida, ffmpeg de verdad, navegador de verdad. Es el gap de test
nº 7 del audit `docs/memory/audit-2026-06-10-playback-chain.md` — jsdom
no ve nada de esto (layout, MSE, hls.js, sesiones de transcode).

## Qué cubre

| Spec | Escenario del audit |
|---|---|
| `playback.spec.ts` | (a) login → play → primer frame → seek lejano (seek-restart de ffmpeg) → cerrar → resume en posición |
| `upnext.spec.ts` | (b) fin de episodio → overlay UpNext → reproducir el siguiente |
| `audio-switch.spec.ts` | (c) cambio de dub mid-play (`?audio=N`, sesión nueva — PB-6) mantiene la posición |
| `livetv-zap.spec.ts` | (d) import M3U → transmux → zapear 3 canales sin spinner colgado |
| `server-down.spec.ts` | (e) SIGKILL al backend mid-play + seek → error terminal acotado (PB-16), no bucle infinito |

Los **cinco escenarios del audit están cubiertos**. El smoke de Live TV
levanta su propio upstream HTTP (M3U + MPEG-TS sintético) en loopback;
funciona porque el transmux no valida `isSafeUpstream` — si ese guard
se extiende al transmux, este spec necesitará el knob de config que lo
acompañe (upstreams de LAN son un caso de uso legítimo).

## Cómo funciona

- `global-setup.ts` genera fixtures de media con ffmpeg en `e2e/.tmp/`
  (película MKV h264+aac de 5 min con 2 pistas de audio → fuerza
  DirectStream/HLS; 2 episodios MP4 de 30s → DirectPlay) y, en local,
  construye el binario si no le pasas uno.
- Cada spec arranca **su propia instancia** del servidor (puerto y data
  dir temporales) y la aprovisiona por API pura: wizard de setup →
  admin → bibliotecas → scan. Sin estado compartido entre specs; el
  smoke de servidor caído puede matar su proceso sin romper al resto.
- Un solo worker a propósito: dos transcodes en paralelo en un runner
  de CI convierten timeouts de player en flakes.

## Ejecutar

```bash
cd web
pnpm test:e2e            # local: construye SPA + binario si hace falta
```

Variables útiles:

- `HUBPLAY_E2E_BIN` — binario pre-construido (CI lo exige; en local se
  construye solo si falta).
- `PW_CHROME` — ruta a un binario Chrome/Chrome-for-Testing concreto.
  Sin él se usa `channel: "chrome"` (el Chrome del sistema).

> **Por qué Chrome y no el Chromium de Playwright:** los builds de
> Playwright (incluido el headless shell) son open-codecs-only — no
> decodifican H.264/AAC, que es lo único que produce el transcoder.
> Chrome real (o Chrome for Testing) sí. En `ubuntu-latest` de GitHub
> Actions ya viene preinstalado.

Requisitos locales: `ffmpeg`/`ffprobe` en el PATH (los usa tanto el
server bajo test como la generación de fixtures), Go y pnpm.
