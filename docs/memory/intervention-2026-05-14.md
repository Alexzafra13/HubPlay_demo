# Intervención post-auditoría — 2026-05-14

> Rama: `claude/review-go-media-backend-37MDe`
> Audit de referencia: `docs/memory/audit-2026-05-14-go-backend-review.md`
> (inmutable; este doc tracée el trabajo iteración por iteración).

## Protocolo

Cada commit en esta rama referencia el olor por su letra única
(ej. `fix(security): FFF — añade CheckRedirect a imaging.SafeGet`).
Al cerrar un olor, se añade un párrafo de cierre debajo de su entrada
en este doc (no se edita el audit original).

## Estado por iteración

| It. | Estado | Foco | Olores | Notas |
|----:|---|---|---|---|
| 0 | 🔄 en curso | Pre-trabajo: ADRs + conventions.md | — | — |
| 1 | 🔄 en curso | Fixes urgentes seguridad + correctness | FFF, F16-1, RRR-mig, RR, Y, DD, GGGG, AAA, EE, HHH, F16-6, F16-7 | — |
| 2 | ⏳ pendiente | Sub-paquetes de `db/` | B, J, K, T, L | — |
| 3 | ⏳ pendiente | Migración Opción B incremental | M (iptv → auth → library) | — |
| 4 | ⏳ pendiente | Split de god-handlers/services | P, Z, QQ | — |
| 5 | ⏳ pendiente | Refactor estructural `iptv/` | CC | — |
| 6 | ⏳ pendiente | Composition root | G, H, V, Q, LL, JJ | — |
| 7 | ⏳ pendiente | Cosmética + schema | D, X, W, BB, UUU-mig, etc. | — |
| 8 | ⏳ pendiente | Polish de calidad de código | F14-X, F15-X, F16-X | — |
| 9 | ⏳ pendiente | Verificación empírica | `-race`, `goleak`, `govulncheck` | post-merge |

---

## Iteración 0 — Pre-trabajo

Se documentan los ADRs nuevos en `architecture-decisions.md` y se
actualizan convenciones en `conventions.md`. Los ADRs que **aplican
directamente a fixes de Iteración 1** (019 SSRF, 021 EvalSymlinks)
se abren primero; los demás se abren cuando llegue su iteración
correspondiente.

### Cierres

- ✅ **ADR-019** (SSRF guard con `CheckRedirect` obligatorio) —
  añadido al final de `architecture-decisions.md`. Documenta el
  vector, la decisión, la implementación de referencia, y la
  convención hacia futuros HTTP clients outbound.
- ✅ **ADR-021** (Path traversal: `EvalSymlinks` obligatorio antes
  de `filepath.Rel`) — añadido. Documenta el vector CVE-class de
  F16-1, el algoritmo de resolución (incluido "trepar al primer
  componente existente" para destinos no-creados) y los
  trade-offs.

Pendiente: ADR-015 (Opción B), ADR-016 (composition root), ADR-017
(timeouts), ADR-018 (comentarios en español), ADR-020 (up-only),
ADR-022 (clock injection), ADR-023 (error filtering) — se abren
cuando llegue su iteración.

---

## Iteración 1 — Fixes urgentes de seguridad + correctness

Cero cambios de API pública. Cierra los 2 CVE-class + 3 bugs
latentes de drain + violación up-only + leak de ratelimit + 2
olores menores de federation + 1 de audit trail.

### Cierres

- ✅ **FFF** (SSRF redirect bypass en `imaging.SafeGet`) —
  `internal/imaging/safety.go`: `http.Client` ahora setea
  `CheckRedirect` que re-valida la IP en cada hop con la misma
  función `validateOutboundURL` extraída. Patrón paralelo al de
  `iptv/proxy.go`. Test de regresión:
  `TestSafeGet_RejectsRedirectToPrivateIP` monta un httptest
  server que devuelve `302` hacia otro httptest server, con un
  mock de `BlockedIP` contador para verificar que el redirect se
  rechaza con `ErrUnsafeURL`. 4 tests `TestSafeGet_*` verdes;
  suite completa de `internal/imaging/...` sin regresión.
- ✅ **F16-1** (path traversal en `people.isUnderImageDir`) —
  nuevo helper compartido `internal/api/handlers/imagedir.go` con
  `isPathUnderImageDir(imageDir, p)` que: (1) resuelve el
  imageDir con `EvalSymlinks` una vez, (2) intenta `EvalSymlinks`
  sobre el path completo, (3) si falla (destino aún no creado),
  trepa al primer componente existente y resuelve allí, pegando
  el sufijo no-creado. `PeopleHandler.isUnderImageDir` e
  `ImageHandler.isUnderImageDir` ahora delegan en el helper —
  resuelve la duplicación que tenía exactamente el mismo bug.
  Tests nuevos: `TestIsPathUnderImageDir_Symlink`,
  `_TraversalLiteral`, `_SymlinkInParentChain`,
  `_NonExistentTargetUnderRootAccepted`,
  `_NonExistentOutsideRoot`, `_HappyPath`. Suite completa
  `internal/api/handlers/...` sin regresión.

Pendientes en Iteración 1: RRR-mig, RR, Y, DD+GGGG, AAA, EE, HHH,
F16-6, F16-7. Se abordan en próximos commits.
