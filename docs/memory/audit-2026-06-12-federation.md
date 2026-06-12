# Auditoría de la federación / P2P — 2026-06-12

> Audit focalizado en el módulo de **federación servidor-a-servidor**
> (`internal/federation/`, `internal/api/handlers/federation/`, surface
> web de peers). Método: 4 sweeps paralelos especializados (handshake/
> cripto · auth runtime JWT/nonce/RL · streaming/SSRF/proxy · manager/
> storage/sweeper/frontend) + verificación manual cruzada de cada
> hallazgo load-bearing contra el código. **Solo análisis — 0 cambios.**
> Toda evidencia con `file:line`.
>
> Calibración: los sweeps sobre-graduaron severidades y se contradijeron
> entre sí (uno afirmó que el nonce cache crece sin cota — FALSO, tiene
> cap de 10k + eviction, `nonce.go:43-49`). Las severidades de abajo son
> las **re-graduadas tras leer el código**, no las de los sweeps.
>
> Contexto de modelo de amenaza: un peer está **emparejado por un admin
> vía invite out-of-band**, así que "peer hostil" exige que un peer ya
> confiado se vuelva malicioso o sea comprometido. El propio
> `federation.md` lista ese caso como in-scope, así que cuenta — pero
> modula la severidad (no es un atacante anónimo de internet).

Leyenda: 🔴 Crítico · 🟠 Alto · 🟡 Medio · 🟢 Bajo/hardening.

---

## Veredicto

La **base criptográfica y de autenticación es sólida**: Ed25519 por
request (independiente de TLS), verificación de `alg` explícita (sin
algorithm-confusion), pinning de pubkey, replay con nonce cache acotado,
audiencia validada, rate-limit token-bucket por peer, y un SSRF guard
(`validatePeerURL`) en la URL de pairing. Eso está bien hecho.

Lo que **falta o flaquea** se concentra en dos frentes: (1) **defensas
de agotamiento de recursos** que el diseño promete y el código no
implementa (cuotas por peer), y (2) **robustez operacional** (SSRF en
redirects, revocación no fail-closed, limpieza de sesiones/datos). Nada
de esto rompe el playback federado feliz, pero la respuesta a "¿está
todo perfecto?" es **no para producción con peers no plenamente
confiables** — sí para federar entre tus propios servidores / amigos.

---

## 🟠 Altos

### ✅ F-1 · SSRF por redirects no validados en el cliente saliente — RESUELTO (2026-06-12)
`internal/federation/manager.go:241` (`httpClt: &http.Client{Timeout: …}`)
+ `client.go:376,485` + `internal/api/handlers/me/me_peer_image.go`.
*(Hallado independientemente por mi lectura y por el sweep 3 — señal de
robustez.)*

El cliente HTTP de federación usa el transport por defecto: **sin
`CheckRedirect`**. `validatePeerURL` (`url.go:42`) solo valida la URL
**inicial**; los 3xx se siguen hasta 10 saltos sin re-validar. Un peer
emparejado hostil/comprometido responde a un fetch de póster / stream /
browse con `302 → http://169.254.169.254/…` (metadata link-local) o a un
servicio interno, y Server A le hace de proxy de vuelta al usuario.
Agravante: `validatePeerURL` **no bloquea RFC1918** a propósito
(`url.go:25-38`, para federación en LAN), así que ni siquiera la URL
pinada tiene por qué ser pública — los redirects lo componen.

**Impacto:** SSRF a la red interna de A / metadata cloud, exfiltrable al
usuario o a logs. **Fix aplicado:** `federationCheckRedirect`
(`url.go`) re-corre `validatePeerURL` en cada salto (bloquea loopback/
link-local/unspecified/multicast; RFC1918 sigue permitido para LAN) +
tope de 5 saltos, cableado en el único `m.httpClt` (`manager.go:241`)
→ cubre TODAS las llamadas salientes (browse/search/stream/póster/
handshake). Tests en `url_test.go` (loopback, metadata link-local, cap,
y refusal a través de un `http.Client` real). No es a prueba de
DNS-rebinding por sí solo (el lookup compite con el dial), pero cierra
el alcance a metadata/servicios internos que motivó el hallazgo.

### F-2 · Cuotas de recursos por peer: prometidas, no implementadas
`internal/federation/peer.go:32-52` (struct `Peer`) +
`federation_stream.go`. *(Sweeps 2 y 3; confirmado por grep.)*

`federation.md` §3 define `MaxConcurrentStreams`, `MaxConcurrentTranscodes`,
`DailyBytesQuota`, `MaxBandwidthMbps` como las defensas anti-DoS del
modelo de amenaza. **El struct `Peer` no tiene ninguno de esos campos;
el tipo `PeerPermissions` del doc no existe en el código.** Las sesiones
de stream federadas llaman a `stream.Manager.StartSession` sin scope de
peer → comparten el **cap global** de transcode con los usuarios
locales. Un solo peer hostil abre transcodes hasta agotar
`MaxTranscodeSessions` global → **starvation del playback de los
usuarios locales** (justo lo que la fila "transcode storms" del threat
model dice defender).

Matiz importante: los **scopes por share SÍ existen** (`CanBrowse` /
`CanPlay` / `CanDownload`, verificados en cada handler) — lo que falta
son los **techos de cantidad/ancho de banda**, no el control de acceso.

**Impacto:** DoS de recursos locales por un peer. **Fix:** campos de
cuota en `Peer` + schema, contados en el handler de stream antes de
spawnnear, y contador de bytes en el audit con corte diario.

---

## 🟡 Medios

### ✅ F-3 · El `exp` del JWT entrante no tiene techo — RESUELTO (2026-06-12)
`internal/federation/jwt.go:93-156` (`ValidatePeerToken`).

El receptor solo rechazaba tokens **ya expirados** (`ErrTokenExpired`);
no comprobaba que `exp` no estuviera absurdamente en el futuro. Un peer
firma sus propios JWT y controla `exp` → podía emitir un token válido
**un año**, rompiendo la garantía "utilidad acotada a 5 min".
**Fix aplicado:** tras parsear, `ValidatePeerToken` rechaza
(`ErrInvalidToken`) si `claims.ExpiresAt` es nil o `> now + peerTokenTTL
+ peerTokenSkew`. Test `TestValidatePeerToken_RejectsFarFutureExp`.

### ✅ F-4 · Revocación no es fail-closed ante error de refresh de cache — RESUELTO (2026-06-12)
`internal/federation/manager.go` (`RevokePeer`).

Si `refreshPeerCache` fallaba, solo se logueaba `Warn` y la cache
in-memory conservaba la entrada `Paired` stale → el peer revocado
seguía autorizado hasta el siguiente refresh exitoso. **Fix aplicado:**
`RevokePeer` resuelve el `ServerUUID` del peer antes de revocar; si el
refresh post-revoke falla, hace `delete(m.peerCache, serverUUID)`
directo bajo `m.mu` → el auth gate ve `ErrPeerNotFound` en el siguiente
request. Test `TestRevokePeer_FailClosedOnCacheRefreshError` (fuerza el
fallo de `ListPeers`).

### F-5 · Segmentos HLS sujetos al rate-limit pese a la exención del diseño
`internal/api/mount_federation.go:59-87` (grupo bajo `RequirePeerJWT`) +
`middleware.go:103-116`. *(Sweep 2; routing verificado.)*

`federation.md` §3 dice "streaming HLS segment endpoints are exempt from
request rate". El código aplica el token-bucket (default 60/min) a
**todas** las rutas `/peer/*`, segmentos incluidos. A ~2s/segmento
(~30 req/min por stream), **2 viewers concurrentes de un peer → 429**.
**Fix:** eximir las rutas de segmento del rate-limit por request (o
contar 1 token en `POST /session` y segmentos gratis durante el TTL).

### F-6 · Sesión de stream federada sin stop-on-disconnect
`internal/federation/stream.go:75-90` (`SweepStreamSessions`).

El sweep **solo borra el mapping de bookkeeping** (`delete(m.streamSessions,id)`)
— nunca llama a `stream.Manager.StopSession`. El ffmpeg subyacente vive
hasta el idle-reaper propio de `stream.Manager` (~5 min). No es leak
indefinido, pero **combinado con F-2** (sin cuota de transcode por peer)
un peer puede ocupar slots globales de transcode 5 min cada uno sin
reproducir nada. **Fix:** cancelar la sesión de `stream.Manager` al
barrer / al desconectar el peer; ligar al fix de F-2.

### F-7 · `EventPeerKeyMismatch` no se emite y los fallos de auth no se auditan
`internal/federation/middleware.go:64-72` + `manager.go:112`
(`EventPeerKeyMatch` existe; no hay `…Mismatch`).

En key mismatch / token inválido, el middleware loguea un `Warn` pero
**no registra entrada de audit** (el `peer` es nil al fallar la firma) y
**no dispara evento de alarma**, pese a que `federation.md` §3 promete
`EventPeerKeyMismatch`. La request **sí se rechaza** (fail-closed), así
que es un hueco de **observabilidad/forense**, no de acceso: un peer
martilleando con tokens malos no deja rastro en el audit log, solo en
logs. **Fix:** emitir el evento + audit entry en el path de rechazo
(atribuible por `claims.Issuer` aunque la firma no valide).

### F-8 · Revoke/borrado de peer no hace limpieza en cascada
`internal/federation/storage/` + `manager.go` (`RevokePeer` solo cambia
`status`). *(Sweeps 3 y 4.)*

Revocar deja huérfanos: `federation_library_shares`,
`…_pending_requests`, `…_items_cache`, `…_progress`. Las queries filtran
por `status='paired'`, así que **no es leak de acceso**, pero crece sin
cota y el progreso de items de un peer revocado sigue apareciendo en
"Continue Watching" del usuario (`manager_progress.go`, sweep 3 #10).
**Fix:** `DeletePeer` con cascada (o purge explícito post-revoke);
filtrar `ListContinueWatching` por status del peer.

---

## 🟢 Bajos / hardening

- **F-9 · Invite single-use no atómico con el insert del peer.**
  `manager_handshake.go:285-296`: `InsertPeer` y `MarkInviteUsed` no van
  en una transacción; si el segundo falla, el invite queda usable
  (mitigado en parte por el guard `GetPeerByServerUUID`). Ventana
  estrecha (error de DB justo entre dos writes). **Fix:** una tx, o
  marcar usado antes de insertar.
- **F-10 · `validatePeerURL` acepta credenciales embebidas.**
  `url.go:42` no rechaza `http://user:pass@host` → riesgo de fuga de
  secretos en logs de error. **Fix:** `if u.User != nil { reject }`.
- **F-11 · `advertised_url` confía en `X-Forwarded-Host`.**
  `federation_url.go:32-57` — un peer puede inducir a A a anunciarse con
  otro host (DoS de pairing / ayuda a phishing). Mitigado por la
  confirmación OOB de fingerprint. **Fix:** preferir `federation.public_url`
  configurado.
- **F-12 · Audiencia multivaluada tolerada.** `jwt.go:144-153` acepta el
  token si OUR uuid está entre varios `aud`. No es bypass (la firma sigue
  atando al emisor), pero un token bien formado debería tener `aud`
  único. Hardening.
- **F-13 · Goroutines de callback de pairing fire-and-forget.**
  `manager_pairing.go` usa `context.Background()` sin drain en shutdown
  (sweep 4 #1, no verificado a fondo) — posible leak/lentitud en
  SIGTERM. Confirmar antes de actuar.

---

## Gaps de diseño (no bugs — features no implementadas que el doc presenta como existentes)

- **Key rotation**: `federation.md` §3 describe `KeyRotationAnnouncement`
  con grace window como si existiera. El código lo marca explícitamente
  "Phase 2+" (`identity.go:28`). No implementado. **Acción:** ajustar el
  doc a "planned" o implementar antes de prometerlo.
- **Download to local server**: §9 del doc; sin `federation_download.go`.
  Phase 7. Cuando se implemente: path-traversal al escribir, whitelist
  de tipo, límite de tamaño, cuota por peer.

## Descartados (hallazgos de sweep que NO se sostienen al leer el código)

- "Nonce cache crece sin cota" (sweep 4 #5) — **FALSO**: `nonce.go:43-49`
  tiene sweep inline + cap de 10k + eviction por batch.
- "Timing attack en comparación de request token" (sweep 1) — el token
  es un valor aleatorio de 128 bits comparado entero; fuga de tiempo
  práctica ~nula. Negligible.
- "Sesgo en fingerprint words por módulo" (sweep 1) — la lista tiene 256
  palabras; distribución uniforme. Solo el comentario es impreciso.

---

## Estado de remediación

- ✅ **F-1, F-3, F-4 aplicados (2026-06-12)** con tests de regresión.
  Suite `./internal/federation/...` verde con `-race`.
- ⏳ **F-2 (cuotas por peer)** — pendiente. Es la mayor: requiere campos
  en `Peer` + migración de schema + conteo por peer en el handler de
  stream + UI admin. Es una feature, no un fix puntual; hacer aparte
  para no dejar una cuota a medias. Es el siguiente con más impacto.
- ⏳ **F-5, F-6, F-7, F-8** — correctness/operacional; F-6 se liga a F-2.
- ⏳ Bajos (F-9..F-13) + alinear el doc con lo implementado (rotation,
  download siguen siendo Phase 2/7).
