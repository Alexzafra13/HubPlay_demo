# Memoria de proyecto

Memoria viva de trabajo para HubPlay. **Versionada en git**, a diferencia de
`.claude/` que contiene configuración local de la herramienta.

## Diferencia con `docs/architecture/`

| Carpeta | Propósito | Audiencia | Tono |
|---------|-----------|-----------|------|
| `docs/architecture/` | Spec formal de *cómo funciona el sistema* | Cualquier contribuidor | Referencia estable |
| `docs/memory/` | Estado de *qué estamos haciendo, qué decidimos, qué falta* | Sesiones de trabajo (humanas o asistidas) | Working notes, se edita a menudo |

Si algo madura lo suficiente para ser "cómo funciona el sistema", se promueve
a `docs/architecture/` y se elimina de aquí. La memoria no duplica la spec;
complementa con contexto, decisiones y pendientes.

## Contenido

- `project-status.md` — estado actual de la rama, qué se hizo en la última
  iteración, qué falta, próximos pasos concretos.
- `architecture-decisions.md` — ADRs cortos (Contexto → Decisión →
  Consecuencias → Alternativas) de las decisiones no triviales ya tomadas.
- `conventions.md` — patrones del codebase descubiertos al trabajar:
  anti-ciclos, helpers de test, gotchas, reglas de dependencia entre paquetes.
- `archive/` — sesiones antiguas que ya no aportan al entrypoint de sesión.
  No se lee al inicio; sólo cuando hace falta arqueología sobre una decisión
  vieja. Cada fichero cubre un rango temporal cerrado
  (p.ej. `2026-pre-04-28.md`).

## Política de actualización

- **Cada sesión que cambie el estado del proyecto** debe actualizar
  `project-status.md` al terminar. No es opcional — es la única forma de que
  la siguiente sesión arranque con contexto preciso.
- **Cada decisión arquitectónica nueva** se añade a `architecture-decisions.md`
  como un ADR numerado (ADR-00N). Nunca se edita un ADR cerrado; si cambia,
  se añade uno nuevo que lo supersede.
- **Cada patrón del codebase que descubras** (no solo que apliques) va a
  `conventions.md`. Si ya lo sabías pero no estaba escrito, escríbelo — la
  memoria se erosiona; el git no.
- **Toda afirmación se verifica contra código antes de escribirse**. Esta
  memoria no narra intenciones; registra hechos auditables. Si algo no se
  puede verificar, se marca explícitamente como hipótesis.

## Qué NO va aquí

- Secretos, tokens, rutas absolutas con datos del usuario.
- Narrativa de pasos de sesión ("primero hice X, luego Y"). Solo el resultado.
- Duplicados de `docs/architecture/` — si ya está documentado allí, se enlaza.
- Especulación sobre futuro no comprometido. Si no está en el roadmap real o
  en un issue, no va.

## Política de archivo

`project-status.md` es el entrypoint de cada sesión nueva. Cuando crece
demasiado (>~50 KB) y las sesiones más viejas dejan de ser relevantes para
el estado actual, se mueven a `archive/<rango>.md` con un puntero al final
del fichero activo. **Nunca se borra contenido**: sólo se reubica. La regla:
si llevas más de dos semanas sin necesitar leer una sesión, archívala.
