# Verificación de la cuadrícula virtualizada (MediaGrid)

Arnés manual para comprobar que `MediaGrid` virtualiza de verdad: que el
número de tarjetas en el DOM se mantiene **acotado** aunque la lista tenga
miles de ítems (regresión A12).

No entra en el build de producción (`vite build` solo toma `index.html`)
ni en CI. La guardia automática en CI vive en
`src/components/media/MediaGrid.test.tsx` (jsdom).

## Uso

```bash
# 1) servidor de desarrollo
pnpm vite --port 5188 --strictPort

# 2) en otra terminal, medir (Chromium real)
#    PW_CHROME = ruta a un binario de Chrome/Chromium (o usa el de Playwright)
PW_CHROME=/ruta/a/chrome COUNT=5000 LABEL=virtualized node verify/measure-grid.mjs
```

Imprime `topCards` / `peakCards` / `bottomCards` y `scrollHeight`, y guarda
capturas `grid-<label>-top.png` / `-bottom.png`.

**Esperado (virtualizado):** `peakCards` de ~50-100 con `scrollHeight`
cubriendo los N ítems. Con el grid acumulativo anterior, `peakCards` ≈ N.
