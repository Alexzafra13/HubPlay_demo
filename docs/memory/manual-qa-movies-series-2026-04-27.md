# Manual QA — rama `claude/review-movies-series-feature-9npZH`

> Checklist de verificación manual cuando puedas arrancar el server.
> Marca con `[x]` lo que vayas validando. Las claves son los commits
> de la rama; el orden recomendado va de los más visibles a los más
> sutiles.
>
> Setup mínimo:
> ```
> make dev          # backend con air
> cd web && pnpm dev
> ```
> Asume al menos 1 librería de películas + 1 de series con metadata
> ya escaneada y al menos 1 episodio de serie reproducible.

---

## Commit `eb7795e` — `user_data` en listings + badges visto/progreso

**El cambio más visible. Hay que verlo a simple vista.**

### Backend
- [ ] `GET /api/v1/libraries/{id}/items` devuelve `user_data` por
  ítem cuando hay token.
  ```bash
  TOKEN=...  # token JWT de un user que haya visto algo
  curl -s -H "Authorization: Bearer $TOKEN" \
    "http://localhost:8096/api/v1/libraries/{id}/items?limit=5" \
    | jq '.data.items[] | {id, title, user_data}'
  ```
  Comprueba: ítems sin estado **NO** tienen la clave `user_data` en
  el JSON (ausente, no `null`); ítems vistos tienen
  `user_data.played === true` y `progress.percentage === 100`.
- [ ] La misma URL **sin** `Authorization` no devuelve `user_data`
  en ningún ítem.
- [ ] `GET /api/v1/items/{id}` ahora devuelve `user_data` cuando
  hay token. Antes era ausente y por eso "Add to favorites" siempre
  parecía vacío.

### Frontend — grids de Movies y Series
- [ ] Marca un ítem como visto: abre `/movies/{id}` → click "Play"
  → espera al final, o usa el endpoint POST para forzar:
  ```
  curl -X POST -H "Authorization: Bearer $TOKEN" \
    http://localhost:8096/api/v1/me/progress/{id}/played
  ```
- [ ] Vuelve a `/movies` → el poster muestra **un check verde
  redondo en la esquina superior izquierda**.
- [ ] Reproduce otra peli y para a la mitad. Vuelve a `/movies` →
  **barra de accent abajo** (1 px de alto), proporcional al avance.
- [ ] **Mutua exclusión**: nunca debe verse check + barra a la vez.
  Una vez `played`, la barra desaparece.
- [ ] Lo mismo en `/series` (series enteras suelen no tener
  `played` propio, pero los episodios sí; ver siguiente bloque).

### Frontend — accesibilidad
- [ ] Inspecciona un poster con progreso parcial: el `<div>` de la
  barra tiene `role="progressbar"` y `aria-valuenow` con el % real.
- [ ] El check tiene `aria-label="Visto"` (o "Watched" en inglés).
  Verifica con un screen reader si tienes uno a mano (VoiceOver:
  `Cmd+F5`), pero también vale inspeccionarlo en DOM.

### Frontend — `/movies/{id}` (detalle)
- [ ] El corazón del Hero ahora **refleja correctamente** el estado
  de favorito (antes siempre se veía como "no favorito"). Toggle
  → debería persistir tras refresh.

---

## Commit `697734c` — Movies/Series unificados + i18n aria-labels

### Funcional
- [ ] `/movies` y `/series` siguen funcionando exactamente igual:
  scroll infinito, búsqueda en cliente, sort dropdown.
- [ ] Cambia el idioma de la app a español (selector de idioma o
  `localStorage.setItem("hubplay_lang","es")` + reload). En
  `/movies/{id}` con un kebab menu visible:
  - [ ] Inspecciona el botón de tres puntitos → `aria-label="Mas
    opciones"` (no "More options").
  - [ ] Botón corazón → `aria-label="Quitar de favoritos"` o
    "Anadir a favoritos" (no "Remove from favorites" / "Add to").
- [ ] Cambia a inglés → mismos botones con `aria-label="More
  options"` / "Add to favorites" / "Remove from favorites".

### Code health
- [ ] Verifica que `web/src/pages/Movies.tsx` y `Series.tsx` son
  ahora 5 líneas cada uno (wrappers de `MediaBrowse`).

---

## Commit `bb8dc17` — cache + backoff TMDb / Fanart

### Cache hit
- [ ] Tira un escaneo de librería desde admin con TMDb configurado.
- [ ] Mira logs: la primera tanda de items emite requests a TMDb.
- [ ] **Tira un segundo escaneo con `refresh_metadata=true`** sin
  esperar 7 días. Los logs no deberían mostrar las mismas N
  requests — el cache las absorbe. (Para una verificación más
  estricta: mete un proxy / mitmproxy delante y observa el conteo
  de requests realmente salidas).
- [ ] Reinicia el server. El cache es in-memory → primera tanda
  vuelve a ir contra TMDb. Esperado.

### Backoff con 429
- Difícil de provocar con TMDb real sin saturar deliberadamente.
- [ ] Test indirecto: con `httpcache_test.go` ya cubierto, basta
  verificar que `go test ./internal/provider/ -count=1 -v -run
  Caching` pasa los 7 tests.

### Single-flight
- [ ] Si tienes una serie con muchos episodios, escanea — el log
  de TMDb debería mostrar **una** llamada a `/tv/{id}` aunque la
  serie + episodios sean N+1 candidatos. Antes habría N+1 llamadas.

---

## Commit `07fd29f` — Up Next overlay con countdown

**Necesitas una serie con al menos 2 episodios consecutivos
escaneada.**

### Happy path
- [ ] Abre `/series/{id}` → entra al S1E1 → click Play.
- [ ] Salta al final del vídeo (atajo End o arrastra el seek bar).
- [ ] **Al disparar `ended`**: aparece tarjeta abajo a la derecha
  con:
  - [ ] Thumb del próximo episodio (backdrop o poster).
  - [ ] `S1 · E2` en chip negro sobre la thumb.
  - [ ] Título del próximo episodio.
  - [ ] Botón "Reproducir en 5s" (en español; en inglés "Play in
    5s") con foco automático.
  - [ ] Botón circular X de cancelar.
  - [ ] Barra fina de progreso bajo los botones, contracorriente.

### Auto-advance
- [ ] No tocas nada → tras ~5 s el reproductor cambia al siguiente
  episodio sin interrupción.

### Cancelación con click
- [ ] Reproduce otro ep → al final, **click en la X** → overlay
  desaparece, reproductor queda en frame final, no avanza.

### Cancelación con Esc
- [ ] Reproduce otro ep → al final, pulsa **Esc** → overlay
  desaparece. (Esc no cierra todo el reproductor; eso es B-back).

### Click "Reproducir ahora"
- [ ] Antes de que expire el contador → click en el botón principal
  → cambia inmediatamente al siguiente.

### Sin siguiente episodio
- [ ] Termina el **último** episodio de una serie / temporada → **no**
  debería aparecer overlay (no hay siguiente). El reproductor cae al
  flujo legacy (cierre normal o quedar en ended).

### Películas (no serie)
- [ ] Termina una **película** → no debería aparecer overlay (no
  hay `nextUp`). Cierre normal.

---

## Commit `6d904db` — selector de calidad

**Necesita un ítem que pase por transcode (no direct play).**

### Visibilidad condicional
- [ ] Reproduce una película que entre en **direct play** (el
  servidor sirve el fichero directo). En el bottom bar del player
  → **no hay icono de calidad** (porque hls.js ni se usa).
- [ ] Reproduce algo que dispare transcode (cliente sin códec, o
  fuerza transcode desde admin). En el bottom bar → **aparece
  icono de calidad** entre subtítulos y fullscreen.
- [ ] Si por casualidad solo hay 1 nivel en la ladder (raro) → el
  icono **no** aparece (regla "más de un nivel").

### Funcional
- [ ] Hover el icono de calidad → dropdown con:
  - [ ] "Auto" arriba (con highlight accent si está seleccionado).
  - [ ] Las rungs disponibles: ej. `1080p`, `720p`, `480p`, `360p`.
- [ ] Click "720p" → la reproducción debería cambiar de bitrate
  (puede haber un re-buffer corto). En DevTools Network, los
  segmentos solicitados ahora deberían venir del playlist 720p.
- [ ] Click "Auto" de nuevo → vuelve a ABR. En DevTools, los
  segmentos pueden subir/bajar de calidad según ancho de banda.

### Visualización del estado
- [ ] Con "Auto" elegido, aunque hls.js esté reproduciendo
  internamente 1080p, el dropdown sigue mostrando "Auto"
  highlighted (no "1080p"). Eso es **intencional**.
- [ ] Con "720p" pinned, el dropdown muestra "720p" highlighted.

---

## Commit `33c9f9c` — i18n del player + sort dropdown

**Necesitas cambiar el idioma a `es` (selector de la app o
`localStorage.setItem("hubplay_lang","es")` + reload).**

### Player en español
- [ ] Reproduce algo. En el bottom bar:
  - [ ] Hover el icono de audio → label "Audio" (igual en ambos
    idiomas, sin sorpresa).
  - [ ] Hover el icono de subtítulos → label **"Subtitulos"**, opción
    "Off" se llama **"Ninguno"**.
  - [ ] Hover el icono de calidad (si hay >1 nivel) → label
    **"Calidad"**, "Auto" igual.
- [ ] Inspecciona `aria-label` del botón Play/Pause central →
  "Reproducir" / "Pausa" según estado.
- [ ] `aria-label` del botón fullscreen → "Pantalla completa" /
  "Salir de pantalla completa".
- [ ] `aria-label` del botón mute → "Silenciar" / "Activar sonido".
- [ ] `aria-label` del botón Back (flecha izquierda arriba) →
  "Atras".

### Sort dropdown en español
- [ ] En `/movies` o `/series`, abre el dropdown de orden:
  - [ ] **"Titulo"**, **"Ano"**, **"Anadidos recientemente"**,
    **"Valoracion"** (en ese orden).
- [ ] `aria-label` del select → "Ordenar por".

### Player en inglés (regresión)
- [ ] Cambia a `en`. Confirma que todo lo de arriba pasa a "Audio /
  Subtitles / Off / Play / Pause / Fullscreen / Exit fullscreen /
  Mute / Unmute / Back / Title / Year / Recently Added / Rating /
  Sort by".

---

## Commit `c7120a7` — tests de MediaGrid + ItemDetail

**No tiene UI; lo verifica el suite.**

- [ ] `cd web && pnpm test` → 274/274 verde.
- [ ] `cd web && pnpm exec tsc --noEmit` → 0 errores.

Si rompes algo en `MediaGrid.tsx` (cambiar virtualización a
setState-in-effect, p.ej.) los tests fallan. Si rompes el flow de
favorito o el menú admin/user en `ItemDetail.tsx`, también.

---

## Commit `06bde24` — scanner descarga imágenes al disco

**El fix arquitectónico más relevante de la rama. La forma de
verificarlo es DB + filesystem, no UI.**

### Pre-condición
- Setup limpio (DB nueva, sin imágenes previas) o al menos una
  librería nueva con TMDb configurado y al menos una película que
  el matcher reconozca.
- Identifica el directorio donde HubPlay guarda imágenes:
  `<dirname-del-database>/images`. Por defecto: `~/.hubplay/images`
  o donde apunte `database.path` en `hubplay.yaml`.

### Comportamiento esperado tras el scan
- [ ] Lanza un scan completo (admin → Bibliotecas → Escanear).
- [ ] Inspecciona la tabla `images` en SQLite:
  ```bash
  sqlite3 <ruta-db> 'SELECT id, type, path, provider, blurhash IS NOT NULL FROM images LIMIT 10'
  ```
  - **`path`** debe ser de la forma `/api/v1/images/file/<uuid>`,
    **NUNCA** una URL `https://image.tmdb.org/...` o `https://assets.fanart.tv/...`.
  - **`provider`** debe ser `tmdb` o `fanart` (no `unknown` salvo
    proveedor nuevo no reconocido).
  - **`blurhash`** debe estar presente para JPEG/PNG (TMDb posters);
    puede estar vacío para WebP (logos de Fanart) — eso es deuda P1
    documentada, no este commit.
- [ ] Inspecciona `<imageDir>/`:
  ```bash
  ls -la <imageDir>/
  ```
  - Hay subdirectorio `<itemID>/` por cada item con imágenes.
  - Dentro de cada uno, ficheros con la forma
    `primary_<16hex>.jpg`, `backdrop_<16hex>.jpg`, `logo_<16hex>.png`.
  - Hay subdirectorio `.mappings/` con un fichero por imagen
    (UUID = nombre, contenido = path local absoluto).
  - **NO** debe quedar ningún fichero `.tmp` (síntoma de write
    no atómico interrumpido).

### Comportamiento del navegador
- [ ] Abre `/movies` con DevTools → Network → Img.
- [ ] Cada poster cargado debe ser un GET a `/api/v1/images/file/<uuid>`
  desde el mismo origen (HubPlay), **NO** un 307 redirect a
  `image.tmdb.org`.
- [ ] Tira la conectividad externa (modo avión, o `iptables -A
  OUTPUT -d image.tmdb.org -j DROP` si te animas) y recarga
  `/movies`. **Los posters siguen cargando** (vienen de disco).
  Antes de este commit, todos romperían.

### Atomicidad de uploads
- [ ] Sube una imagen manual desde el ImageManager admin.
- [ ] Mientras está en vuelo, mata el server (`kill -9` al PID).
- [ ] Re-arranca y comprueba que **no** queda un `.tmp` en
  `<imageDir>/<itemID>/` ni un fichero corrupto.

### Refresh manual
- [ ] POST `/api/v1/libraries/<id>/images/refresh` (50 items por
  llamada). Comprueba que sigue funcionando idéntico que antes —
  el refresher ahora va a través del mismo helper que el scanner.

### Tests automáticos
- [ ] `go test -race ./...` → verde.
- [ ] En particular `TestFetchAndStoreImages_PersistsLocalPathNotURL`
  es el guard de regresión: si alguien introduce un nuevo path
  `http://...` en la tabla images, este test falla.

---

## Smoke tests transversales

### Backend
- [ ] `go test -race ./...` → todo verde.
- [ ] `make build` → ok.
- [ ] Arranca con `make dev` → no panics, no errores de wiring de
  providers en logs.

### Frontend
- [ ] `cd web && pnpm test` → 260/260 verde.
- [ ] `cd web && pnpm exec tsc --noEmit` → 0 errores.
- [ ] `cd web && pnpm build` → bundle ok, sin warnings nuevos.

### Regresión visual rápida
- [ ] `/` (Home) renderiza sin cambios.
- [ ] `/live-tv` sigue funcionando (no debería haber tocado nada;
  los livetv tests siguen verdes pero conviene un vistazo).
- [ ] `/admin/libraries`, `/admin/users`, `/admin/system` ok.
- [ ] Login + setup wizard ok (no toqué nada de auth, pero por si
  acaso).

---

## Riesgos conocidos / cosas a vigilar

1. **Respuesta `user_data` con `null` desde el backend en futuros
   cambios**. Los tests verifican que se omita la clave cuando no
   hay row, no que sea `null`. Si alguien cambia la lógica para
   serializar `null`, los badges del frontend siguen funcionando
   (`user_data?.played` da `undefined`), pero la respuesta crece
   innecesariamente.

2. **Cache de providers no se persiste**. Tras restart, el primer
   scan vuelve a pegarle a TMDb. Esperado, pero si quieres
   verificarlo: `tail -f` los logs durante un restart + scan.

3. **El UpNextOverlay solo se muestra al `ended` real**. No
   detecta créditos ni el "salto a los 30 s del final" estilo
   Netflix. Si terminas el vídeo manualmente con seek a 99% +
   pause, no dispara hasta que llegue el evento `ended`.

4. **Quality selector + ABR**: si el dropdown se queda con la
   última selección manual y luego cambias de archivo, useHls
   debería resetear `currentQuality` a -1 cuando se monta nuevo
   `<video>` (lo hace, pero conviene un ojo).

---

## Cuando termines la pasada

Marca este fichero como cerrado en `project-status.md` y
elimínalo si todo verde, o anota issues encontrados al final
para que la siguiente sesión los recoja.
