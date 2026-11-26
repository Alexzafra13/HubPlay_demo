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
| **Transcode** (códec no soportado → H.264) | ✅ 11/11 segmentos alineados con el manifest, saltos lejanos en 2,5 s |
| **DirectStream / remux** (H.264 copiado — el caso MÁS común) | ❌ 6/11 segmentos empiezan **+2 s** tarde |
| DirectPlay (fichero tal cual) | ✅ por diseño (range requests) |

**El fallo del remux:** el manifest VOD es sintético (todos los
segmentos "6 s"), pero copiando el vídeo ffmpeg solo puede cortar en los
keyframes del fichero. Con keyframes cada 4 s salen segmentos de 8 y 4 s;
en ficheros reales (x264 con scenecut) los keyframes son irregulares, así
que el desajuste es la norma. hls.js suele corregirlo con los PTS;
**ExoPlayer (la app de TV) confía en el manifest**: posición desplazada,
saltos que caen en otro sitio, solapes. Es el candidato nº 1 a "al pasar
adelante/atrás hace cosas raras" en la tele.

**Arreglo propuesto (prioridad 1):** manifest de remux con los keyframes
reales, como Jellyfin (`KeyframeExtractor`):
1. Extraer los tiempos de keyframe del vídeo (cues de Matroska / índice
   MP4; si no, `ffprobe -select_streams v:0 -show_entries
   packet=pts_time,flags`), cachearlos por fichero (ruta+tamaño+mtime) y
   calcularlos al escanear o en el primer play.
2. Agrupar keyframes en segmentos de ~6 s y emitir EXTINF reales.
3. Cortar ffmpeg exactamente ahí (`-f segment -segment_times …` o
   `-hls_time` con los mismos tiempos) y que el seek-restart arranque en
   el keyframe de inicio del segmento pedido.
4. `seek-check.py` en verde para remux con keyframes de 4 s e
   irregulares; añadir el caso al CI (job opcional, necesita ffmpeg).

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
| Consumir en la **app de TV** | ❌ **la app no llama a ningún endpoint de federación** |
| Límites por amigo | 🟡 concurrencia sí (5 por defecto); sin cuota de ancho de banda |
| Descargar del amigo a tu servidor, rotación de claves | ❌ no implementado (Phase 7 / 2+) |
| Hardening F-5..F-9, F-11..F-13 | ⏳ (para amigos de confianza no bloquea) |

**Falta (servidor):** F-6 (cortar el stream federado al desconectar) y
F-5 (segmentos fuera del rate-limit), que afectan a ver contenido de un
amigo en la tele.
**Falta (app TV):** sección "Compartido contigo": rail de recientes de
amigos en Inicio, navegar sus bibliotecas, ficha, reproducir vía
`/me/peers/{peer}/stream/...` y guardar progreso. El servidor ya lo da
todo.

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
3. **Sección "Compartido contigo"** (pilar 3).
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
2. Pilar 2: remux con keyframes reales + reinicio inmediato en saltos
   lejanos (`seek-check.py` en verde).
3. Pilar 3 en la app: "Compartido contigo" (+ F-5/F-6 en servidor).
4. Pilar 1/5: bugs de estudios/logos y rails nuevos.
5. Release de la app (firma, Baseline Profile, Play Store).
