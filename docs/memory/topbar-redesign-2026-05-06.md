# Topbar redesign — sidebar → topbar-only nav

> Sesión iniciada **2026-05-06**. Branch: `claude/pedantic-wozniak-9f70b1`.
> Worktree: `.claude/worktrees/pedantic-wozniak-9f70b1/`.
> Esta nota existe para retomar la sesión sin contexto previo.

## Objetivo

Eliminar el panel lateral (`Sidebar`) y montar toda la navegación en el
topbar al estilo Plex: brand a la izquierda, items principales con
dropdown animado en el centro, search + avatar a la derecha.

## Decisiones cerradas con el usuario

1. **Hamburger** solo en móvil (`<md`). En desktop no hay nada que
   colapsar, así que se elimina.
2. **"Servidores conectados"** en la nav central → **admin-only** y
   solo si hay `peers.length > 0`. Si no, no aparece el item.
3. **Avatar dropdown** absorbe lo personal/admin: Ajustes · Vincular
   dispositivo · Administración (link a `/admin/dashboard`, no
   sub-items) · Logout.
4. **TV en vivo**: sus tabs (`LiveTvTopBar`) y los controles de
   `MediaBrowse` (sort/filtros) que hoy se inyectan al topbar via
   `TopBarSlot` se mueven a una **subbarra inline dentro de cada
   página**. Ya tienen camino de fallback inline → solo hay que
   activarlo y matar el slot.
5. Dropdowns de Movies/Series con dos columnas: **Explorar** (links a
   sort/filter URL params que el página ya entiende) +
   **Géneros** (preset `?genre=...`). LiveTV con **Vistas** (`?tab=`)
   + **Categorías** (`?cat=`). Sin inventar endpoints.

## Estructura de archivos nuevos / modificados

```
web/src/components/layout/
  navConfig.ts        ← NUEVO  · schema declarativo de la nav
  MainNav.tsx         ← NUEVO  · barra central desktop (incluye dropdown panels)
  MobileDrawer.tsx    ← NUEVO  · versión apilada para móvil
  TopBar.tsx          ← EDITAR · monta MainNav, hamburger sólo móvil,
                                 avatar dropdown amplía con Vincular dispositivo
  AppLayout.tsx       ← EDITAR · elimina Sidebar + margin-left, switch a MobileDrawer
  Sidebar.tsx         ← BORRAR
  TopBarSlot.tsx      ← BORRAR (último paso)

web/src/hooks/
  useSidebarCollapsed.ts ← BORRAR

web/src/components/livetv/
  LiveTvTopBar.tsx    ← EDITAR · dropear `useTopBarSlot`, render inline siempre

web/src/pages/
  MediaBrowse.tsx     ← EDITAR · dropear `useTopBarSlot`, render inline siempre

web/src/i18n/locales/
  en.json + es.json   ← EDITAR · keys nuevas `navMenu.*`
```

## Plan de ejecución (orden)

- [x] Leer codebase (LiveTV, TopBarSlot, BrandMark, Movies/Series,
      useSidebarCollapsed)
- [x] Crear `navConfig.ts`
- [x] Crear `MainNav.tsx` con NavMenu primitive inline (hover-intent
      80/200ms, click toggle para táctil, Escape, role=menu, dropdown
      con `framer-motion`, dropdown dinámico de peers)
- [x] Nota de continuidad en `docs/memory/`
- [ ] **Commit 1**: navConfig + MainNav + esta nota
- [ ] Crear `MobileDrawer.tsx` (mismo schema apilado, accordions)
- [ ] Editar `TopBar.tsx`: hamburger sólo móvil, monta MainNav,
      avatar dropdown añade "Vincular dispositivo"
- [ ] Editar `AppLayout.tsx`: borra Sidebar, drawer móvil pasa a
      MobileDrawer, elimina `marginLeft` del main
- [ ] **Commit 2**: TopBar + AppLayout + MobileDrawer integrados
- [ ] Editar `LiveTvTopBar.tsx` y `MediaBrowse.tsx` para no usar
      `useTopBarSlot` (render inline siempre)
- [ ] Borrar `Sidebar.tsx`, `useSidebarCollapsed.ts`, `TopBarSlot.tsx`
- [ ] **Commit 3**: cleanup + páginas inline
- [ ] Añadir keys i18n nuevas (`navMenu.*`) a en.json + es.json
- [ ] **Commit 4**: i18n
- [ ] Verificar en preview a 1440/1024/768/360, dropdown hover y
      teclado, dark mode

## Decisiones técnicas no obvias

- **Single open-id con dos timers (open 80ms, close 200ms)** en
  `MainNav.tsx` para hover-intent. Switch entre triggers ya abiertos
  es inmediato (sin delay) — emula Plex.
- **Panel anclado al trigger** con `absolute left-1/2 -translate-x-1/2`
  + flecha conectora rotada 45deg. Sin `LayoutGroup` cross-item por
  ahora — KISS. Si el usuario lo pide, se añade después.
- **Peers dropdown** vive separado (no en `navConfig`) porque su
  contenido es dinámico (hook `useAllPeerLibraries`). Se renderiza
  cuando `isAdmin && peerLibs.length > 0`.
- **Mobile drawer** comparte el mismo `navConfig` para no duplicar
  fuente de verdad. Se renderiza con secciones colapsables (no
  hover; tap toggles).
- **`TopBarSlot` se elimina** porque ahora el topbar tiene contenido
  fijo (MainNav). Dejarlo activo crearía conflictos visuales con el
  dropdown. Los dos consumers actuales (LiveTV, MediaBrowse) ya
  tienen camino inline de fallback → solo activar.

## Comandos útiles para retomar

```bash
git checkout claude/pedantic-wozniak-9f70b1
cd .claude/worktrees/pedantic-wozniak-9f70b1
make web-dev    # vite dev server
```

Fichero de tareas vivo: ver `TodoWrite` del agente. Si arranca otra
sesión, leer este fichero y `git log --oneline -20` para ver qué
commits van hechos.
