# Subida de Media (Uploads) — Design Document

## Overview

HubPlay permite a usuarios autenticados con permiso `can_upload` subir
ficheros de media a una librería de su elección. El protocolo es **tus
1.0.0** (resumable uploads) servido por la librería oficial
`github.com/tus/tusd/v2` montada en `/api/v1/uploads/`. Tras llegar los
bytes, una pipeline backend valida → analiza con ffprobe → mueve
atómicamente a la librería → audita.

**Decisión clave**: usamos tusd en vez de un POST multipart porque las
subidas reales de media son grandes (decenas de GiB) y las conexiones
domésticas se cortan. Resumable cubre ese caso con coste de implementación
bajo (tusd es estable, lo usa Vimeo en producción).

---

## 1. Estados y permisos

### `can_upload` (migración 053, PR1)

Permiso individual por usuario. Off por defecto incluso para admins —
subir media no es "operación de operador", es "consumo activo". El admin
con `can_manage_users` lo otorga vía `PUT /users/{id}` o tras crear el
usuario.

### Cuota per-user

Dos columnas en `users`:

- `upload_quota_bytes`: tope absoluto que sus uploads pueden ocupar.
  `0` = subir deshabilitado de facto.
- `upload_used_bytes`: bytes ocupados actualmente por SUS uploads
  (mantenido por el repo vía `ReserveUploadBytes` / `ReleaseUploadBytes`).

La cuota se reserva **atómicamente en PreCreate** (antes de empezar a
recibir bytes), con un UPDATE conditional:

```sql
UPDATE users SET upload_used_bytes = upload_used_bytes + ?
WHERE id = ?
  AND can_upload = 1
  AND upload_quota_bytes > 0
  AND upload_used_bytes + ? <= upload_quota_bytes;
```

Si no se cumple, el UPDATE no afecta filas y el repo devuelve
`domain.ErrUploadQuotaExceeded`. **Sin race window**: el WHERE
condiciona la mutación.

---

## 2. Arquitectura

```
┌─────────────────────────────────────────────────────────────────────┐
│ Cliente (web/src/pages/Uploads.tsx)                                  │
│   tus-js-client v4 → POST /api/v1/uploads/ (creación)                │
│                    → PATCH /api/v1/uploads/<id> (chunks 8 MiB)       │
│   EventSource /api/v1/uploads/events ─── SSE filtrado por user_id    │
└─────────────────────┬───────────────────────────────────────────────┘
                      │
┌─────────────────────▼───────────────────────────────────────────────┐
│ internal/upload/tusd_handler.go                                      │
│   tusd.Handler (FileStore en <staging_dir>)                          │
│   ├── PreUploadCreateCallback ──► service.PreCreate                  │
│   │      - valida nombre/extensión, reserva cuota                    │
│   │      - redirige binPath a <staging>/<user>/<id>/<name>           │
│   ├── PreUploadTerminateCallback (DELETE)                            │
│   ├── CompleteUploads channel  ──► service.Finish (goroutine)        │
│   └── TerminatedUploads chan   ──► service.Aborted                   │
└─────────────────────┬───────────────────────────────────────────────┘
                      │
┌─────────────────────▼───────────────────────────────────────────────┐
│ internal/upload/service.go (orchestrator)                            │
│   Finish() corre síncrono en goroutine, emite SSE por fase:          │
│     1. validating  → magic-byte sniff (MKV/MP4/AVI/MPEG-TS/SRT/...)  │
│     2. probing     → ffprobe; rechaza < min_duration_ms              │
│     3. moving      → MoveTo con suffix -NNN ante colisión            │
│     4. indexing    → emite event.ItemAdded para el scanner           │
│   accepted → audit row + UploadDone                                  │
│   rejected/error → release cuota + audit + UploadError               │
└─────────────────────────────────────────────────────────────────────┘
```

### Layout en disco

```
<config_dir>/uploads/staging/
├── <user_id_1>/
│   └── <upload_id_a>/
│       ├── <upload_id_a>          # blob de tusd (durante upload)
│       ├── <upload_id_a>.info     # JSON con FileInfo + metadata
│       └── <sanitized_filename>   # bin después del PreCreate redirect
└── <user_id_2>/
    └── ...
```

El sub-dir por (user, upload) aísla nombres en colisión y permite
cleanup atómico (`RemoveAll` del dir entero al graduar / abortar).

---

## 3. Pipeline post-bytes en detalle

`internal/upload/service.go` → `Finish(ctx, FinishInput) FinishResult`

### Phase 1: validating

```go
// Sniff de los primeros 4 KiB; firmas verificadas:
//   MKV  (EBML)           : 0x1A 0x45 0xDF 0xA3
//   MP4  (ftyp box @ off4): "ftyp"
//   AVI  (RIFF)           : "RIFF"
//   MPEG (pack start)     : 0x00 0x00 0x01 0xBA
//   TS   (sync byte doble): 0x47 @ 0 y @ 188   ◄── strict, evita falsos pos
//   SRT/WebVTT/ASS por marcadores de texto
```

ANTI-FALSO-POSITIVO conocido: un único 0x47 al inicio no es TS. Hay que
verificar el segundo sync byte a offset 188 (invariante del estándar
ISO/IEC 13818-1).

### Phase 2: probing (ffprobe)

Sólo si `kind == KindVideo`. Subtítulos saltan ffprobe — no son media
decodificable. Rechaza:
- `result.Format.Duration < MinDurationMs` (default 1000 ms — defensa
  anti-payload trivial de 1 byte con magic bytes manualmente añadidos).
- Cualquier exit non-zero de ffprobe (codec malformado, etc.).

### Phase 2.5: SHA-256 streaming

Best-effort, NO bloquea el flujo si falla — el hash es nice-to-have
para auditoría/dedup futuro, no requisito.

### Phase 3: moving

`<staging>/<user>/<id>/<name>` → `<library.path[0]>/<name>`

- `os.Rename` cuando intra-filesystem (caso común si staging y libs
  comparten volumen, recomendado en config docs).
- Fallback `copyFile + remove` si cross-filesystem (EXDEV detectado
  por mensaje del LinkError — portable sin importar syscall.EXDEV).
- Colisión de nombre: sufijo `-001`, `-002`, ... hasta 1000 intentos
  (más sería patológico).

### Phase 4: indexing

Emite `event.ItemAdded` con `{library_id, path, source: "upload"}` para
que el scanner lo recoja en su siguiente pasada (en producción, el
scanner suele estar también suscrito al bus y reacciona en milisegundos).
No esperamos a la indexación — el cliente recibe `UploadDone` aquí.

### Cleanup

Tras `accepted`, `RemoveAll(<staging>/<user>/<id>/)` borra el blob de
tusd, su `.info`, y cualquier intermedio. Tras error, también — los
bytes ya no sirven.

---

## 4. Audit log

Tabla `upload_audit` (migración 054). Append-only. Una fila por upload
en su **estado final** — las fases intermedias fluyen por SSE, no
ensucian la DB.

```sql
CREATE TABLE upload_audit (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL,
    library_id      TEXT,                    -- vacío si no aterrizó
    original_name   TEXT NOT NULL,
    final_path      TEXT,                    -- vacío si no aterrizó
    bytes           INTEGER NOT NULL,
    sha256          TEXT,                    -- hex, NULL si falló
    mime_detected   TEXT,
    outcome         TEXT NOT NULL CHECK (
        outcome IN ('accepted','rejected','aborted','error')
    ),
    error_message   TEXT,                    -- NULL si accepted
    started_at      DATETIME NOT NULL,
    finished_at     DATETIME NOT NULL,
    duration_ms     INTEGER NOT NULL DEFAULT 0
);
```

**Sin FK a users/libraries**. Decisión deliberada: el log debe
sobrevivir al sujeto si lo borran. Una FK con CASCADE perdería rastro;
sin CASCADE bloquearía el delete. Desacoplar es el camino correcto
para audit logs.

Índices:
- `(user_id, started_at DESC)` — panel "tus uploads".
- `(outcome, started_at DESC) WHERE outcome != 'accepted'` — parcial,
  para el dashboard "fallos en las últimas N horas".

---

## 5. Eventos SSE

`/api/v1/uploads/events` — filtrado server-side por `claims.UserID`
(no por subscripción separada). Cuatro tipos:

| Tipo | Cuándo | Data |
|---|---|---|
| `upload.phase` | Transición de fase post-bytes | `{id, user_id, phase}` |
| `upload.done` | Pipeline accepted | `{id, user_id, library_id, final_path}` |
| `upload.error` | Pipeline failed o aborted | `{id, user_id, reason}` |
| `upload.bytes` | Reservado v1 | (no se emite todavía) |

**Garantía**: un upload emite a lo sumo UN evento terminal (`done` o
`error`, nunca los dos). Tras él, no llegan más eventos para ese id.

**Reglas del consumer**:
- Si llega un id desconocido, ignorar silencioso (puede ser una pestaña
  paralela del mismo usuario).
- Si falta `user_id` en el data (defensa), dejar pasar — mejor ruido
  que perder un terminal.

---

## 6. Validación del cliente vs servidor

**Espejo**, no réplica. El cliente (`web/src/pages/Uploads.tsx`)
implementa:

- Whitelist de extensiones (`ACCEPTED_EXTENSIONS`).
- Chequeo de cuota local (compara con `me.upload_quota_bytes -
  me.upload_used_bytes`).

Estos son **defense in depth + UX** — fallan rápido sin tocar la red.
**La fuente de verdad es el backend**:

- `internal/upload/validator.go` para extensión + magic bytes.
- `users.ReserveUploadBytes` para cuota atómica.

Si el cliente se salta la validación (drag-drop bypasses `accept`,
DevTools), el server rechaza con 403 igualmente.

---

## 7. Configuración

`hubplay.example.yaml`:

```yaml
upload:
  enabled: true
  # Vacío = <config dir>/uploads/staging. DEBE estar en el MISMO
  # filesystem que las librerías destino para que el move final sea
  # rename atómico — cross-fs cae a copy+remove (más lento, mismo
  # resultado).
  staging_dir: ""
  max_bytes_per_upload: 53687091200  # 50 GiB. 0 = sin tope (NO recomendado).
  min_duration_ms: 1000              # rechaza < 1s.
```

Si `enabled: false`, las rutas `/api/v1/uploads*` no se montan y el
binario arranca sin StagingDir. Lifecycle queda igual al de pre-PR2 —
el feature gate está limpio.

---

## 8. Tests

- `internal/upload/`: 36 tests unitarios cubriendo sanitize, validator,
  staging, library picker, service (todas las fases) y eventos.
- `internal/db/upload_audit_repository_test.go`: 3 tests del repo.
- `web/src/pages/Uploads.test.tsx`: 10 tests del componente (mock de
  tus-js + estado-máquina UI).

**No probados con cliente tus real** (deuda conocida v1). Un test E2E
con un servidor tusd embebido + cliente JS sería ideal pero está fuera
del alcance.

---

## 9. Limitaciones conocidas y futuro

### Crash recovery (no implementado)

Si el server cae mientras una pipeline está corriendo, el blob queda
huérfano en `<staging>/<user>/<id>/`. Soluciones futuras:

- GC programado (escanea staging, borra entradas más antiguas que N días).
- Reanudación al boot (mira los `.info` de tusd, decide si reanudar
  pipeline o descartar).

V1 no las hace porque el caso es raro y el operador puede limpiar a
mano (`rm -rf <staging>/*`). En producción real, un cron suele bastar.

### Audio puro y otros formatos

`allowedExtensions` no incluye `.mp3`/`.flac`/`.opus` — la app es de
vídeo en v1. Añadir audio puro implica:

1. Extender el whitelist en `validator.go`.
2. Considerar que ffprobe acepta audio (ya lo hace).
3. Decidir routing: ¿librerías "music" reciben audio uploads? La policy
   del `LibraryPicker` está abierta para extensión.

### Quota soft vs hard

V1 sólo enforce cuota HARD. Soft quota con alertas al admin sería un
add-on natural — la columna `upload_used_bytes` ya está, sólo falta
sumarizar y emitir notification cuando cruza un umbral.

### Refinar `/admin/system` por capability

`/admin/system/sessions/{id}` DELETE (kill session) sigue gated por
`RequireAdmin` en bloque. Refinarlo a un permiso específico
(¿`can_manage_users`? ¿permiso nuevo `can_kill_sessions`?) queda para
producto.

### Cross-origin auth

El cliente actual asume same-origin (binario Go sirve SPA + API). En
deploys CORS con SPA en host distinto, hay que añadir `onBeforeRequest`
al `tus.Upload` que llame `req.setRequestCredentials("include")`.
Trivial pero no necesario hasta que llegue ese deploy.
