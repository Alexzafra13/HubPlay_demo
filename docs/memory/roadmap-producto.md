# Roadmap de producto — "mi Plex/Jellyfin"

> Hacia dónde va HubPlay y qué significa "terminado" en cada pilar.
> Complementa a `project-status.md` (estado del día a día) con la visión.
> Creado 2026-11-26. **Todo lo marcado "medido" se comprobó ese día
> contra el binario real**; lo demás viene de leer el código o los audits.
>
> **Objetivo final:** cerrar la app de TV en Kotlin
> (`Alexzafra13/Hubplay_Demo_Android`) sobre un servidor en el que todo
> funcione. Cada pilar dice qué le falta al servidor y qué le falta a la
> app.

## La visión en una frase

Un servidor de casa que lee tus carpetas, las rellena solo con
carátulas y fichas, reproduce **cualquier** fichero en la tele o el
navegador saltando adelante y atrás sin tirones, y te deja compartir tu
biblioteca con amigos que también tienen HubPlay, todo emparejando la
tele con un QR.

---

## Pilar 1 · Biblioteca y metadatos

**Terminado =** apuntas a una carpeta, en minutos ves todas las pelis y
series con póster, fondo, sinopsis, reparto y tráiler; lo nuevo aparece
solo; si algo sale mal se corrige con "Identificar".

| | Estado |
|---|---|
| Escaneo de carpetas (pelis/series), watcher de cambios, scheduler | ✅ (medido: 2 pelis escaneadas en segundos) |
| Metadatos TMDb + Fanart, imágenes con blurhash, trickplay | ✅ — requiere API key de TMDb en el wizard |
| Identificar / refrescar metadatos (web y TV) | ✅ |
| Saltar intro/créditos (capítulos) | ✅ detector por capítulos; sin detección por audio |
| Colecciones, estudios, personas | ✅ — bug: estudio "Lucasfilm" devuelve 0 títulos (anotado en la app) |

**Falta:** el bug de estudios; logos de canales que dan 404
(`/channels/{id}/logo`); decidir si basta la detección de intros por
capítulos.

## Pilar 2 · Reproducción (ffmpeg) — adelante/atrás sin problemas

**Terminado =** cualquier fichero arranca en < 3 s y un salto a cualquier
punto, adelante o atrás, reanuda en < 2 s **en el sitio exacto**, en la
web y en la TV. Criterio automático: `scripts/seek-check.py` en verde.

| Modo | Estado (medido 2026-11-26 con `scripts/seek-check.py`) |
|---|---|
| **Transcode** (códec no soportado → H.264) | ✅ segmentos alineados con el manifest; salto lejano en 2,5 s |
| **DirectStream / remux** (H.264 copiado — el caso MÁS común) | ✅ **arreglado 2026-11-26** (remux por segmentos): keyframes regulares, irregulares con B-frames y GOP abierto con audio AC3→AAC, todos alineados; cada segmento en 0,00–0,09 s (hasta 0,56 s convirtiendo audio) |
| DirectPlay (fichero tal cual) | ✅ por diseño (range requests) |

**Qué era:** el manifest VOD era sintético ("6 s por segmento") pero
copiando el vídeo ffmpeg solo corta en los keyframes del fichero: antes,
6/11 segmentos empezaban hasta 2 s tarde. ExoPlayer (la app de TV) se fía
del manifest, así que los saltos caían en otro sitio. Con el demuxer HLS
de ffmpeg como "reproductor que se fía del manifest", un salto pedido al
segundo X caía hasta 6,7 s después; ahora cae en el sitio (el resto que
queda en ficheros irregulares es la política de ese demuxer de avanzar al
siguiente keyframe, y sale mejor que un HLS de referencia generado por el
propio ffmpeg).

**Cómo está hecho** (`internal/stream/keyframes.go`, `segplan.go`,
`remux.go`; detalle en `docs/architecture/streaming.md` § Segmented
Remux): índice de keyframes cacheado por fichero → plan de segmentos con
los cortes reales → cada segmento es una invocación corta de ffmpeg
independiente (adelante y atrás son lo mismo, sin reinicios), con 3 de
prefetch. Receta validada fotograma a fotograma; cobertura en
`remux_integration_test.go` (ffmpeg real, con tests de mutación hechos a
mano: quitar cualquier pieza de la receta los hace fallar) y
`seek-check.py`.

**Pendiente de esta pieza:**
- **Probar en la Mi TV** tras desplegar (ExoPlayer real).
- **Indexar en segundo plano tras el escaneo**: hoy el índice se calcula
  en el primer play (espera máx. 3 s) o al pedir `/info`; un fichero
  enorme en disco duro no llega en 3 s y esa primera reproducción va por
  el remux continuo de siempre (la siguiente ya sale por segmentos).
- Opcional: leer los cues de Matroska directamente (indexar sin leer el
  fichero entero) y poda de la caché `keyframes/` (un JSON pequeño por
  fichero, sin límite hoy).
- Transcode: en el run inicial los segmentos salen con +1,47 s de
  desfase y tras un reinicio con +1,40 (70 ms; dentro de tolerancia).
  Probable mismo efecto que se corrigió en el remux
  (`-avoid_negative_ts`); revisar.

**Mejora barata:** el salto lejano espera 2 s fijos a que aparezca el
segmento antes de reiniciar ffmpeg (`waitForFile(…, 2s)` en
`handlers/media/stream.go`). Si el índice pedido está claramente por
delante del último producido, reiniciar ya: 2,5 s → ~0,5 s.

**Pendientes del audit de playback (P3):** PB-19 (hardware lento →
restart-thrash), PB-26 (duración N/A), PB-29/30 (DNS-rebind IPTV),
PB-31/33/36-39; gaps de test 1, 4, 5, 6.

## Pilar 3 · Compartir con otros servidores (federación)

**Terminado =** emparejas tu servidor con el de un amigo con una
invitación, eliges qué bibliotecas compartes, y su contenido aparece en
tu web **y en tu tele** como una sección más, con progreso propio.

| | Estado |
|---|---|
| Emparejado por invitación, cripto Ed25519, revocación | ✅ base sólida (audit 2026-06-12) |
| Compartir por biblioteca con permisos browse/play/download | ✅ |
| Consumir: navegar, buscar, recientes, seguir viendo, pósters, streaming HLS, progreso (`/me/peers/*`) | ✅ en servidor y **web** |
| Consumir en la **app de TV** | ✅ en la rama `claude/stoic-feynman-pyojkw` (2026-11-26): pestaña "Compartido", ficha, reproducción con subtítulos y progreso, rails en Inicio, búsqueda. Sin probar en real |
| Reproducir un MP4 H.264/AAC de un amigo | ✅ **arreglado 2026-11-26** (antes 409 `WRONG_METHOD` en web y TV: la federación no tiene camino directo y el amigo decidía DirectPlay) |
| Series compartidas | ❌ solo se comparten items de primer nivel: la serie se ve, sus episodios no |
| Límites por amigo | 🟡 concurrencia sí (5 por defecto); sin cuota de ancho de banda |
| Descargar del amigo a tu servidor, rotación de claves | ❌ no implementado (Phase 7 / 2+) |
| Hardening F-5..F-9, F-11..F-13 | ⏳ (para amigos de confianza no bloquea) |

**Falta (servidor):** F-6 (cortar el stream federado al desconectar) y
F-5 (segmentos fuera del rate-limit), que afectan a ver contenido de un
amigo en la tele.
**Falta (app TV):** probarlo en real con un segundo servidor; quitar de
"Seguir viendo de tus amigos" (no hay endpoint).

## Pilar 4 · Emparejar la tele con QR

**Terminado =** la tele muestra un QR; lo escaneas con el móvil, inicias
sesión si hace falta, confirmas, y la tele entra sola.

| | Estado |
|---|---|
| Flujo RFC 8628 (código en TV + `/link` en móvil) | ✅ servidor + app TV |
| QR → cámara nativa del móvil → `/link?code=…` **sin sesión** | ✅ **arreglado 2026-11-26**: el login perdía el código y había que teclearlo; ahora vuelve a `/link?code=…` (también pasando por "¿Quién está viendo?") |
| Botón "Escanear" dentro de `/link` | ✅ solo con HTTPS (los navegadores no dan cámara en HTTP). En HTTP ahora se explica que vale la cámara nativa (antes el botón desaparecía sin decir nada) |
| Decodificador QR offline (sin CDN) | ✅ (arreglado antes) |

**Falta:** probarlo en real con el móvil contra el servidor desplegado.
Si se quiere el escáner dentro de la página en la LAN, hace falta HTTPS
en la LAN (en "distribución avanzada": TLS LAN).

## Pilar 5 · App de TV (Kotlin) — lo que la separa de "terminada"

Estado de la app (su `docs/memory/project-status.md`, 2026-09-16):
funciona en la Mi TV real (login/emparejado, Inicio, catálogo, fichas,
reproductor VOD con audio/subtítulos, TV en directo, buscar,
colecciones, salvapantallas). Pendiente, en orden propuesto:

1. **Desplegar el servidor actual** en `192.168.1.100:8097` (la caché de
   subtítulos, el id de instalación y los arreglos de hoy no están allí).
2. **Remux con keyframes reales** (pilar 2) y verificar saltos en la Mi TV.
3. ~~**Sección "Compartido contigo"** (pilar 3)~~ — hecho 2026-11-26
   (rama `claude/stoic-feynman-pyojkw`), junto con liberar sesiones de
   ffmpeg al salir/zapear, quitar de Continuar viendo y logout real.
4. Rails nuevos de Inicio (favoritos, colecciones, estrenos, por género…;
   necesitan endpoints) — propuestos el 2026-09-16, sin decidir.
5. Bugs: estudio Lucasfilm 0 títulos, logos de canal 404, ficha sin
   artwork, `CollectionDetailScreen`.
6. Release: firma real, Baseline Profile, Play Store (guía en su
   `docs/PLAY_STORE.md`); probar en móvil/tablet.

## Ideas para que se sienta "pro" (no comprometidas)

- **Ajustes de subtítulos** (tamaño, fondo, retardo ±): el render ya es
  propio en web y TV.
- **Velocidad de reproducción** y **normalizar volumen** (diálogos bajos).
- **"Ver juntos"** (SyncPlay) entre usuarios o entre servidores amigos.
- **Control remoto**: lanzar algo a la tele desde el móvil.
- **Acceso desde fuera de casa** sin abrir puertos (relay o TLS + DDNS).
- **Avisos**: "nuevo episodio de X", "tu amigo compartió Y".
- **Detección de intros por audio** cuando no hay capítulos.

## Orden de ataque recomendado

1. Mergear la rama actual + desplegar en el servidor de la TV.
2. Pilar 2: ~~remux con keyframes reales~~ ✅ (2026-11-26); queda probar
   en la Mi TV, indexar tras el escaneo y el reinicio inmediato del
   transcode en saltos lejanos.
3. Pilar 3: ~~"Compartido contigo" en la app~~ (2026-11-26, sin mergear);
   queda F-5/F-6 en servidor y probarlo con dos servidores reales.
4. Pilar 1/5: bugs de estudios/logos y rails nuevos.
5. Release de la app (firma, Baseline Profile, Play Store).
