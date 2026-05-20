# Plan de auditoría completa — ejecutable

Este documento es un checklist accionable, no narrativo. Cuando lo abras
en una sesión futura, ve tachando casillas y anotando hallazgos en línea.
Cinco pases secuenciales, 5–6h totales repartibles, cada uno con
entregable concreto.

**Pre-requisito antes de empezar**:
- Tener `docker compose up -d` con la imagen del último merge.
- ≥1 película (4K HDR si es posible), ≥1 serie con varios capítulos,
  ≥1 archivo con subs PGS o ASS embebidos, ≥1 archivo con audio 5.1.
- Una segunda cuenta de usuario (no admin) creada para probar permisos.
- Si tienes IPTV M3U: configurarlo en una librería.
- Si tienes acceso a una segunda máquina o un segundo docker compose:
  poder simular federation.

---

## Pase 1 — Auditoría funcional (1.5–2 h)

Objetivo: confirmar que cada feature visible al usuario hace lo que dice.
Método: smoke test end-to-end manual, anotando ✓ / ⚠ / ✗ + nota.

### Auth & sesión

- [ ] Login con usuario válido — token issued, redirect a /
- [ ] Login con password incorrecto 10 veces seguidas — rate limit
      activa, mensaje claro
- [ ] Refresh token rotation: abrir `me/sessions` antes de un refresh,
      después; el session_id debe renovarse
- [ ] Logout — la sesión desaparece de `me/sessions` en OTROS
      dispositivos vía SSE en <1 s
- [ ] Change password — la sesión actual sigue válida, las demás se
      invalidan al instante
- [ ] Pair device: abrir `/pair` en pestaña A, escanear QR desde
      pestaña B / móvil en `/link?code=…`, aprobar; pestaña A debe
      loguear en <2 s sin polling visible en network tab

### Profiles (household)

- [ ] Crear profile bajo el admin desde Settings
- [ ] Login con profile, ver que hereda librerías del top-level
- [ ] Admin gestiona library_access del top-level desde panel
- [ ] Quitar acceso a una librería; el profile pierde acceso al
      instante (recargar Home)
- [ ] Editar bibliotecas de un profile desde el kebab — el modal debe
      avisar "editas el titular del hogar"

### Discovery / Home

- [ ] Hero auto-play funciona (clicar reproducir desde hero, ?play=1
      en la URL del detail)
- [ ] Continue Watching aparece tras ver 1+ minuto de algo
- [ ] Marca como visto desde rail (botón ✓)
- [ ] Quitar de Continue Watching (botón ✕) — la card desaparece
      optimistically + se confirma con DELETE
- [ ] Recently Added rail con últimas adiciones
- [ ] Recommended / "Porque viste X" rail si hay historial
- [ ] En directo ahora rail si IPTV configurada
- [ ] Click en card → ItemDetail abre con metadata correcta

### ItemDetail

- [ ] Poster + backdrop + Aurora colors aplicados al fondo
- [ ] Logo treatment si TMDb lo trae
- [ ] Lista de seasons + episodes (para series)
- [ ] Cast & crew
- [ ] Similar items
- [ ] Botón Play funciona
- [ ] Mark watched / unwatched
- [ ] Add to favorites
- [ ] Refrescar metadata (admin only)

### Player — lo más crítico

- [ ] **DirectPlay** con un MP4 H.264/AAC compatible: badge "Directo"
- [ ] **DirectStream** con MKV+AC3 remuxable: badge "Stream directo"
- [ ] **Transcode** forzando una resolución menor: badge "Transcode"
- [ ] **HDR tone-mapping** con un HDR10 → SDR en navegador: imagen
      no aparece "lavada" gris
- [ ] **Skip intro / créditos / recap** botones aparecen si el detector
      funcionó
- [ ] **Trickplay** previews al hacer hover en seek bar
- [ ] **Chapter markers** en seek bar si el item tiene capítulos
- [ ] **Up Next** countdown al final de episodio, "Reproducir ahora" /
      "Cancelar"
- [ ] **Audio switch** mid-playback — el player remonta, reanuda en
      el mismo segundo, badge actualiza
- [ ] **Subtitle picker SRT/VTT nativo** (HLS sub track aparece sin
      reinicio)
- [ ] **Subtitle picker burn-in PGS** (Blu-ray rip): aparece con
      etiqueta "Integrado · reinicia el stream", al pinchar reinicia
      y se ve el sub burned
- [ ] **Subtitle picker burn-in ASS** (anime release): mismo flujo
- [ ] **Buscar online subs** (OpenSubtitles) — si tienes API key
- [ ] **Touch móvil**: tap muestra/oculta controles (no pausa),
      picker se abre como bottom sheet
- [ ] **Doble tap derecha/izquierda**: +10s / -10s (si está implementado)
- [ ] **Velocidad de reproducción**: cambia, se mantiene tras seek
      (efecto re-aplicar)
- [ ] **Ajustes (engranaje)**: tinte success/warning según método
- [ ] **Fullscreen + ESC + cierre**: estados correctos
- [ ] **Cierre auto-play**: vuelve a season list (episodios) o
      navigate(-1) (movies). Manual play vuelve a la página de
      detail original
- [ ] **Resume from saved progress**: cerrar a la mitad, volver,
      debe ofrecer reanudar
- [ ] **Up Next** se carga siguiente episodio con backdrop suave
- [ ] **Volumen + mute** se persiste entre sesiones (zustand
      persist a localStorage)
- [ ] **PiP** (si el navegador lo expone)
- [ ] **Stats overlay**: NO existe todavía, anotar como gap

### LiveTV

- [ ] Lista de canales carga, agrupados por categoría
- [ ] EPG (programación) visible — programa actual destacado
- [ ] Play canal → transmux funciona, no se atasca, primer frame
      en <5 s
- [ ] Favoritos toggle (estrella en la card)
- [ ] Schedule programar grabación
- [ ] Bulk schedule masivo (si está)
- [ ] Deep-link desde Home `?channel=X` abre correctamente, sin
      bucle de re-render (regresión 2026-05-10)

### Search

- [ ] Texto encuentra movies, series, people
- [ ] Empty state cuando no hay resultados, sin error
- [ ] Search global desde header siempre disponible
- [ ] Resultados de federation si peers conectados

### Federation

- [ ] Crear invite desde Admin → Federation, copiarlo
- [ ] Aceptar invite desde otro HubPlay (segundo contenedor en el
      mismo docker network)
- [ ] Ver listado de peers
- [ ] Browse librerías de peer desde `/peers/{id}/libraries`
- [ ] Play item de peer (camino más frágil — vigilar):
      transcoding remoto, subs federados, paleta de colores
- [ ] Search cross-peer encuentra items remotos
- [ ] Recent del peer

### Admin

- [ ] Dashboard: stats live cambian al cabo de unos segundos
- [ ] **System → Identity strip** muestra CPU model + cores + GPU
      tras rebuild con commit bea8286
- [ ] **System → Host card** CPU% se mueve cuando inicias un
      transcode (>15% mínimo durante encode)
- [ ] **System → Host card** RAM bar verde / ámbar / rojo según %
- [ ] **System → Host card** GPU info aparece si tienes NVIDIA
      con nvidia-smi montado
- [ ] **System → settings**: 7 entradas, cambiar `transcode_preset`
      a `medium`, ver que aplica al siguiente transcode (sin reinicio)
- [ ] **System → settings**: cambiar `max_transcode_sessions`,
      marca "requiere reiniciar"
- [ ] **System → force_direct_play = true**: banner rojo aparece,
      confirm() salta al toggle
- [ ] **Sessions activas**: aparece al iniciar play, columna método,
      kill desde admin termina playback en cliente en <1 s
- [ ] **Users**: crear usuario nuevo con grant_library_ids
- [ ] **Users**: matriz de bibliotecas (kebab → "Bibliotecas") edita
      grants existentes
- [ ] **Libraries**: crear librería nueva, escanear, items aparecen
- [ ] **Libraries**: preflight check con paths inexistentes
- [ ] **Providers**: añadir TMDb API key, refrescar metadata de un item
- [ ] **Providers**: OpenSubtitles configurado
- [ ] **Federation peers**: gestión completa
- [ ] **Backup**: download de DB completa
- [ ] **Backup**: restore desde un archivo subido (cuidado — destructivo)
- [ ] **Logs**: live stream tail funciona, scroll auto al final
- [ ] **Auth keys**: rotate manual

### Mobile / responsive

- [ ] Home en móvil 375px wide: cards 16:9 caben, scroll horizontal OK
- [ ] Player en móvil con gestures
- [ ] Settings legible en pantalla pequeña
- [ ] Setup wizard mobile-friendly

### Setup wizard (primera ejecución)

- [ ] DB fresca → wizard aparece automáticamente
- [ ] Account step: crear admin
- [ ] Settings step: configuración inicial
- [ ] Libraries step: añadir primera librería
- [ ] Complete step: redirect al home

**Entregable Pase 1**: `docs/memory/audit-functional-YYYY-MM-DD.md`
con cada casilla marcada + nota por fallo. Veredicto: "X ✓ / Y ⚠ / Z ✗".

---

## Pase 2 — Auditoría de código (1 h)

Comandos (todos via docker para evitar el Windows tax):

```bash
# 1. golangci-lint estricto
docker run --rm -v "$PWD:/src" -w /src golangci/golangci-lint:v2.5.0 \
  golangci-lint run --enable-all \
    --disable=depguard,wsl,nlreturn,wrapcheck,exhaustruct,exhaustive,gci,gofmt,gofumpt \
    --timeout=10m ./... > audit-lint.txt 2>&1

# 2. Complejidad ciclomática
docker run --rm -v "$PWD:/src" -w /src golang:1.25-alpine \
  sh -c "go install github.com/fzipp/gocyclo/cmd/gocyclo@latest && \
         gocyclo -over 15 -avg internal/ cmd/" > audit-complexity.txt

# 3. Coverage (todos los paquetes)
docker run --rm -v "$PWD:/src" -w /src golang:1.25-alpine \
  sh -c "apk add --no-cache build-base >/dev/null && \
         go test -coverprofile=/tmp/cov.out ./... 2>&1 && \
         go tool cover -func=/tmp/cov.out" > audit-coverage.txt

# 4. Frontend: exports muertos
cd web && pnpm dlx ts-unused-exports tsconfig.json \
  --excludePathsFromReport='\.test\.|setup\.ts' > ../audit-deadexports.txt; cd ..

# 5. TODOs y FIXMEs
grep -rn "TODO\|FIXME\|HACK\|XXX" \
  --include="*.go" --include="*.ts" --include="*.tsx" \
  internal/ web/src/ cmd/ > audit-todos.txt
```

Revisar después:

- [ ] Funciones con complejidad > 25 (top 5 de `audit-complexity.txt`):
      refactorizar o documentar por qué son así
- [ ] Archivos con < 30% coverage que estén en hot paths
      (router, decision, manager, stream, transcoder): priorizar tests
- [ ] Lint findings nuevos (algunos linters que el repo no tiene
      activos pueden generar ruido — filtrar a los que sean reales)
- [ ] TODOs con > 3 meses (mirar `git blame`): resolver o crear issue
- [ ] Exports frontend muertos > 50: limpiar

**Entregable Pase 2**: lista priorizada en
`docs/memory/audit-code-YYYY-MM-DD.md` con:
- Top 5 fixes urgentes
- 10 mejoras de calidad recomendadas
- TODOs viejos a cerrar

---

## Pase 3 — Performance (1.5 h)

```bash
# Setup: server arrancado, admin token capturado
TOKEN=$(curl -s -X POST http://localhost:8097/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"YOURPASS"}' | jq -r '.data.access_token')

# 1. Hammer endpoints clave con hey
docker run --rm --network host -e TOKEN=$TOKEN \
  ghcr.io/rakyll/hey:latest \
  -n 2000 -c 50 -H "Authorization: Bearer $TOKEN" \
  http://localhost:8097/api/v1/items?limit=50 > audit-perf-items.txt

docker run --rm --network host -e TOKEN=$TOKEN \
  ghcr.io/rakyll/hey:latest \
  -n 2000 -c 50 -H "Authorization: Bearer $TOKEN" \
  http://localhost:8097/api/v1/me/home > audit-perf-home.txt

# 2. CPU profile durante carga (correr en paralelo a hey)
curl -o cpu.pprof "http://localhost:8097/debug/pprof/profile?seconds=30"
go tool pprof -top -cum cpu.pprof | head -30 > audit-pprof-top.txt

# 3. Heap profile (memory leak check, después de 30 min de uso)
curl -o heap.pprof http://localhost:8097/debug/pprof/heap
go tool pprof -top heap.pprof | head -20 > audit-heap.txt

# 4. Bundle analyzer frontend
cd web && pnpm dlx vite-bundle-visualizer; cd ..

# 5. SQL EXPLAIN para las top 5 queries lentas (sqlite)
# Identifícalas mirando los logs con `level=DEBUG` durante carga,
# o añadiendo logging temporal a internal/db/*.go
```

Métricas a registrar:

- [ ] **p50 / p95 / p99** latency de `/items`, `/me/home`,
      `/items/{id}`, `/stream/{id}/info`
- [ ] **Heap antes** y **después** de 1 h de uptime con un scan
      corriendo: ¿crece monótonamente? (leak red flag)
- [ ] **Bundle frontend**: tamaño total gzip, chunks > 100 KB,
      identificar splittable
- [ ] **Top 10 funciones por CPU time** en pprof
- [ ] **CPU% del proceso** durante un transcode estándar:
      ¿escala linealmente con sesiones?

Hits a buscar (los esperables):

- [ ] N+1 queries en home rails (sospecha alta — añadir EXPLAIN a
      `home_repository.go` queries)
- [ ] SQL sin índice descubierto vía EXPLAIN
- [ ] Image proxy sin Cache-Control adecuados
- [ ] hls.js 520 KB en bundle — separar en chunk lazy
- [ ] aurora.ts 69 KB — ¿se importa eager innecesariamente?

**Entregable Pase 3**:
- `docs/memory/audit-perf-baseline-YYYY-MM-DD.md` con números
- 5–10 optimizaciones priorizadas por impacto / coste

---

## Pase 4 — Seguridad (45 min)

```bash
# 1. gosec — security linter Go
docker run --rm -v "$PWD:/src" -w /src securego/gosec:latest \
  -severity medium -confidence medium ./... > audit-gosec.txt

# 2. govulncheck — vulnerabilidades en deps Go
docker run --rm -v "$PWD:/src" -w /src golang:1.25-alpine \
  sh -c "go install golang.org/x/vuln/cmd/govulncheck@latest && \
         govulncheck ./..." > audit-vulns.txt

# 3. Trivy — vulnerabilidades en filesystem + Dockerfile
docker run --rm -v "$PWD:/src" -w /src aquasec/trivy:latest \
  fs --severity HIGH,CRITICAL --skip-dirs node_modules . > audit-trivy.txt

# 4. npm audit frontend
cd web && pnpm audit --severity moderate > ../audit-npm.txt 2>&1; cd ..
```

Revisar a mano:

- [ ] Path traversal en `/stream/{id}/segment/{file}` — verificar
      `filepath.Dir(p) != ms.OutputDir` está y funciona con
      `../` malicioso
- [ ] Path traversal en `/api/v1/images/...` proxy
- [ ] CSRF middleware: leer 5 endpoints mutating y confirmar que
      aplica. Probar request POST sin token desde curl con cookie
      válida: debe rechazar
- [ ] Federation peer JWT: ¿qué pasa si un peer adversario reusa un
      token expirado? Probar manualmente con un JWT vencido manualmente
- [ ] Refresh token reuse detection: simular el escenario "atacante
      con RT viejo" — verificar que el chain entero se revoca
- [ ] SQL injection: revisar todos los sitios con `fmt.Sprintf` que
      construyen SQL (raw SQL holdouts)
- [ ] XSS en player y detail (rendering de títulos / descriptions
      provenientes de TMDb): React escapa por defecto, pero
      `dangerouslySetInnerHTML` en algún sitio sería red flag
- [ ] Rate limiting: solo login lo tiene; ¿otros endpoints sensibles
      deberían (password change, refresh)?

**Entregable Pase 4**: lista de hallazgos con CVSS aproximado +
fix sugerido por cada uno.

---

## Pase 5 — Producción / operaciones (1 h)

### Dockerfile

- [ ] Multi-stage final image basada en `gcr.io/distroless/static`
      o `alpine` (no `golang` con todo el toolchain)
- [ ] User non-root (`USER hubplay` con uid fijo, p. ej. 1000)
- [ ] HEALTHCHECK definido
- [ ] Sin secretos en layers (`docker history hubplay:latest` no
      debe mostrar nada sensible)
- [ ] Imagen final < 100 MB sin contenido de assets

### docker-compose prod

- [ ] `restart: unless-stopped`
- [ ] Logs con rotación: `logging.options.max-size: 10m`,
      `logging.options.max-file: 3`
- [ ] Volumes mounts: `/config` (DB + uploads), `/media` (read-only
      idealmente)
- [ ] Env vars seguros: secrets en env file, NO en compose
- [ ] Network: bind a 127.0.0.1 si hay reverse proxy delante

### Reverse proxy

- [ ] nginx / Caddy config de ejemplo con TLS + HSTS
- [ ] Headers correctos: `X-Forwarded-For`, `X-Forwarded-Proto`,
      `X-Real-IP`
- [ ] CORS limitado al dominio público
- [ ] WebSocket / SSE proxy correcto (timeout largo)

### Logging + backup

- [ ] Logs estructurados JSON cuando `format: json`
- [ ] Niveles correctos (no DEBUG en producción)
- [ ] Backup automatizado: cron entry o systemd timer que llame
      al API `/admin/system/backup`
- [ ] Backups rotados: mantener N=7 últimos

### Recovery test

- [ ] Para el contenedor, copia la DB a backup.db, borra el
      original, intenta arrancar — debe pedir setup
- [ ] Restaura backup.db, reinicia, debe levantar todo intacto
- [ ] Documentar el procedimiento exacto en `docs/architecture/operations.md`

### Stress test

- [ ] 10 transcodes simultáneos con NVENC (si tienes) o software
      según tu auto-tune
- [ ] 1 h sostenida con 5 usuarios viendo simultáneamente
- [ ] Heap MB estable en el panel (no crece linealmente con tiempo)
- [ ] Active sessions bajan al cabo del idle timeout

### Alertas (opcional, si tienes Prometheus)

- [ ] Alerta cuando `transcode_sessions_active >= max * 0.9`
- [ ] Alerta cuando `cpu_percent > 90` sostenido > 5 min
- [ ] Alerta cuando heap MB > umbral o crece monotonamente
- [ ] Alerta cuando DB ping falla
- [ ] Alerta cuando últimas migrations fallan

**Entregable Pase 5**:
- `docs/architecture/operations.md` con setup completo
- `deploy/docker-compose.prod.yml` revisado y comentado
- Lista de alertas Prometheus de ejemplo en `deploy/prometheus/alerts.yml`

---

## Veredicto final

Tras los cinco pases, escribir un `docs/memory/audit-summary-YYYY-MM-DD.md`
con:

1. **Estado real**: X items ✓ funcionales, Y bugs conocidos, Z mejoras
   no urgentes.
2. **Capacidad real**: número de usuarios concurrentes que el data
   real soporta (basado en Pase 3).
3. **Próximos pasos**: top 5 fixes priorizados de cada pase combinados.
4. **Production readiness**: ¿estás listo para 10 / 50 / 100 usuarios?
   ¿qué falta para cada umbral?

Y entonces decidimos el siguiente bloque de trabajo con datos en mano.
