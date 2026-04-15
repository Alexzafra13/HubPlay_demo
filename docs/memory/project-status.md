# Estado del proyecto

> Snapshot: **2026-04-15** · Rama: `claude/review-documentation-gC6eE` · HEAD: `f0fbe5f`

## Resumen ejecutivo

HubPlay está en **MVP funcional** con la superficie de endurecimiento
production-ready recién completada en la rama previa (`claude/project-review-setup-QYnZs`,
mergeada vía PR #66). La iteración actual es de **documentación**: consolidar
memoria de proyecto y corregir referencias obsoletas en `CLAUDE.md`.

No hay código nuevo pendiente de commit; el working tree está limpio.

## Última iteración (6 commits en orden cronológico)

Todos auto-contenidos, con tests propios, en la rama anterior ya mergeada:

1. **`e1353be docs: add CLAUDE.md`** — contexto de proyecto auto-cargable.
2. **`6185ca8 feat(api): typed AppError + request_id correlation`** —
   `domain.AppError` rico (Code, HTTPStatus, Message, Hint, Details,
   RetryAfter, cause, sentinel kind). `handleServiceError` renderiza
   `*AppError` vía `errors.As`, mantiene fallback a sentinels para legacy.
   Request ID de chi propagado en toda respuesta. 17 tests nuevos.
3. **`7de6f08 feat(observability): Prometheus metrics + /metrics`** —
   `internal/observability/` con registry propio (no `DefaultRegisterer`),
   6 métricas bounded-cardinality, `MetricsSink` interface en `stream/`
   para evitar ciclo, `errorRecorder` inyectado en handlers. `/metrics`
   fuera de CORS/CSRF/auth por convención Prometheus.
4. **`a95c679 feat(auth): JWT signing keystore with rotation + overlap`** —
   migración `004_jwt_signing_keys.sql`. `auth.KeyStore` con bootstrap
   idempotente desde `cfg.Auth.JWTSecret`, `Current()`/`Lookup(kid)`/
   `Rotate(overlap)`/`Prune(cutoff)`. JWTs llevan `kid` en header.
   Validador desacoplado vía `keyResolver` function (evita ciclo).
5. **`e25805a feat(auth,observability): admin rotation API + keystore metrics`** —
   `GET /api/v1/admin/auth/keys`, `POST .../rotate`, `POST .../prune`
   gated por `auth.RequireAdmin`. `hubplay_auth_signing_keys{state}`
   como `GaugeFunc` (lee live del keystore). Observer hook para contar
   rotaciones sin importar Prometheus en handlers.
6. **`5b5146d ci: fix golangci-lint timeout + pin linter version`** —
   `.golangci.yml` con `run.timeout=5m`, action pinned a `v1.64.8`.
7. **`f0fbe5f feat(config): preflight checks at startup`** — `Config.Preflight()`
   verifica ffmpeg/ffprobe en PATH, DB dir writable, cache dir writable
   (crea si falta). Errores agregados con `errors.Join`. Extrae
   `StreamingConfig.EffectiveCacheDir()` como única fuente de verdad.

## Estado verificado contra código

| Item | Estado | Ubicación |
|------|--------|-----------|
| `domain.AppError` | ✅ existe | `internal/domain/errors.go` |
| Package `observability` | ✅ existe | `internal/observability/` (5 files) |
| `auth.KeyStore` | ✅ existe | `internal/auth/keystore.go` |
| `Config.Preflight()` | ✅ existe | `internal/config/preflight.go` |
| Migración 004 (JWT keys) | ✅ existe | `migrations/sqlite/004_jwt_signing_keys.sql` |
| Admin keys routes | ✅ existe | `internal/api/router.go:171` |

## Hallazgos pendientes (detectados al auditar, no bloquean)

### 1. `CLAUDE.md` tiene 4 datos obsoletos

| Línea | Dice | Real | Acción |
|-------|------|------|--------|
| Métricas: `.go` files | **105** | **75** | actualizar |
| Métricas: `_test.go` | **38** | **43** | actualizar |
| "Rama activa" | `claude/project-review-setup-QYnZs` | `claude/review-documentation-gC6eE` | actualizar (o quitar, es un dato efímero que queda obsoleto a cada rama) |
| "Memoria de sesiones" | `.claude/memory/` | `docs/memory/` | **crítico**: `.claude/` está en `.gitignore` línea 14 — la memoria nunca viajaba con git |

> Se corrige en el mismo commit que introduce `docs/memory/` para que
> la referencia apunte al sitio real desde el primer día.

### 2. `go.mod`: `prometheus/client_golang` marcado como `// indirect`

Verificado: importado **directamente** en 4 ficheros de
`internal/observability/` (`metrics.go`, `http.go`, `auth.go`,
`metrics_test.go`). El flag `// indirect` es incorrecto — indica que no
se corrió `go mod tidy` tras el commit de observability.

**Acción sugerida (fuera del scope de documentación):** `go mod tidy`
en la próxima sesión que toque código. Es cosmético, no funcional
(go build lo resuelve igual).

### 3. Working tree limpio, sin stash ni untracked

`git status --ignored` está limpio. `git stash list` vacío. `git reflog`
solo muestra los dos checkouts de entrada a la rama. No hay trabajo en
curso perdido.

## Próximos pasos accionables

Ordenados por valor/esfuerzo:

### Inmediato (esta rama de documentación)

1. Completar `docs/memory/` con `architecture-decisions.md` y
   `conventions.md` (ver todos en la sesión actual).
2. Actualizar `CLAUDE.md` para apuntar a `docs/memory/` y corregir las
   4 imprecisiones detectadas.
3. Commit + push a `claude/review-documentation-gC6eE`.

### Siguiente iteración de código (candidatos, no comprometidos)

- `go mod tidy` — arregla el flag `indirect` de prometheus.
- Migrar los `respondError` ad-hoc restantes a constructores específicos
  de `AppError`. El commit `6185ca8` lo dejó deliberadamente pendiente:
  *"existing respondError call sites keep their ad-hoc codes — they
  will migrate to specific constructors when the surrounding handler
  is next touched"*. Oportunidad de limpiar al tocar cada handler.
- CSRF middleware (`internal/api/middleware/csrf.go` — confirmar ruta
  al tocarlo) escribe errores con `http.Error()` → `Content-Type:
  text/plain` aunque el body sea JSON. Inconsistente con el envelope
  estándar. Fix: usar `respondAppError` con un `AppError` de
  `ErrCSRFInvalid` nuevo.
- Auditoría `any`/`interface{}` en backend y migración progresiva a
  tipos concretos.
- Subida de imágenes (`internal/api/handlers/image.go`): revisar
  validación MIME, tamaño máximo, path traversal.

> Estos candidatos son observaciones de auditoría. Antes de
> implementarlos, crear un ADR corto o issue para ratificar el alcance.

## Contexto crítico para la próxima sesión

- **Proyecto no lanzado.** No hay usuarios en producción, no hay
  compatibilidad hacia atrás que preservar. Libertad de romper APIs
  si mejora la calidad del código.
- **Convención de ramas:** trabajar en la rama que diga `CLAUDE.md`
  (o la instrucción de sesión). No pushear a `main` directamente.
- **sqlc-generated:** no editar `internal/db/*.go` a mano; modificar
  `internal/db/queries/*.sql` y correr `make sqlc`.
- **Tests con `chmod` o PATH manipulation** skipan bajo root (dev
  sandbox) y corren en CI con runner sin privilegios. Ver commit
  `f0fbe5f` para el patrón exacto.
