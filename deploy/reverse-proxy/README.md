# Reverse-proxy plug-and-play para HubPlay

HubPlay corre en `127.0.0.1:8096` por defecto y necesita un reverse-proxy delante para servir HTTPS. Este directorio te da seis opciones — todas implementan los mismos contratos del protocolo, elige la que ya operas o la que más cómoda te resulte.

> **¿Por qué importa esto ahora?** A partir de la fase de **federación entre servidores**, tu HubPlay tiene que ser alcanzable por TLS desde otros HubPlay. La configuración del proxy deja de ser opcional; se convierte en parte del modelo de seguridad del protocolo de peering.

---

## Picker — elige tu camino

| Tu situación | Opción recomendada | Esfuerzo |
|---|---|---|
| No tengo proxy todavía, quiero lo más rápido posible | **[Caddy](caddy/)** — auto-Let's Encrypt, sin certbot | 5 min |
| Ya uso nginx en este host | **[nginx](nginx/)** — config refinada, certbot integrado | 10 min |
| Tengo un Traefik del homelab gestionando otros servicios | **[Traefik](traefik/)** — labels en docker-compose | 5 min |
| Tengo un Apache LAMP corriendo, prefiero no añadir otro server | **[Apache](apache/)** — vhost equivalente | 10 min |
| Estoy detrás de CGNAT / no puedo abrir puertos en el router | **[Cloudflare Tunnel](cloudflare-tunnel/)** — gratis, sin abrir nada | 15 min |
| Sólo quiero compartir con amigos en mesh privada, sin internet público | **[Tailscale](tailscale/)** — `https://hostname.ts.net` automático | 10 min |

Si no estás seguro y tienes IP pública (la mayoría de FTTH residenciales en España), **Caddy** es lo más rápido. Si ya tienes un setup con `deploy/nginx/hubplay.conf` corriendo, no tienes que migrar nada — la versión bajo `nginx/` aquí es la actualización con soporte de federación cuando llegue ese feature.

---

## Contratos que TODA opción tiene que cumplir

Independientemente del proxy, estos cinco invariantes son no-negociables:

1. **TLS terminación** con certificado válido (CA pública, o pinning self-signed para Tailscale).
2. **WebSocket upgrade** pass-through en `/api/v1/ws` (eventos en tiempo real).
3. **SSE-friendly buffering** desactivado y timeouts altos en:
   - `/api/v1/me/events` — sync de progreso cross-device
   - `/api/v1/events` — eventos globales (scans, M3U imports)
   - `/api/v1/peer/events` — eventos de federación (cuando llegue la fase 1)
4. **Sin límite de body** en endpoints de streaming y download:
   - `/api/v1/stream/` — segments HLS, contenido grande
   - `/api/v1/peer/` — descargas de federación (películas multi-GB)
5. **Sin rate limiting WAN-style** en `/api/v1/peer/`. Los peers son servidores de confianza identificados por Ed25519 — el rate limit lo hace HubPlay a nivel aplicación con token bucket per-peer.

Cada README per-proxy verifica estos cinco puntos.

---

## Tablas de comparación

### Por capacidad

| Capacidad | Caddy | nginx | Traefik | Apache | Cloudflare T. | Tailscale |
|---|---|---|---|---|---|---|
| Auto-LE | ✅ built-in | ⚠️ certbot | ✅ built-in | ⚠️ certbot | ✅ via CF | ✅ MagicDNS |
| WebSocket | ✅ default | ✅ explícito | ✅ default | ⚠️ módulo | ✅ default | ✅ default |
| SSE keepalive | ✅ default | ✅ ajuste timeout | ✅ default | ⚠️ flushpackets | ✅ default | ✅ default |
| Funciona detrás de CGNAT | ❌ | ❌ | ❌ | ❌ | ✅ | ✅ |
| Expone IP pública | ✅ | ✅ | ✅ | ✅ | ❌ (CF la oculta) | ❌ |
| Rate limiting nativo | ⚠️ plugin | ✅ | ✅ middleware | ✅ módulo | ✅ CF dashboard | ❌ (no aplica) |

### Por uso de federación

| Capacidad federación | Caddy | nginx | Traefik | Apache | Cloudflare T. | Tailscale |
|---|---|---|---|---|---|---|
| Peers públicos pueden conectar | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ (sólo en tu tailnet) |
| Apto para mesh amigos-only | ⚠️ ok | ⚠️ ok | ⚠️ ok | ⚠️ ok | ✅ | ✅ ideal |
| Long-lived peer downloads | ✅ 24h | ✅ 24h ajustable | ✅ ajustable | ✅ timeout config | ⚠️ CF cap 100s default | ✅ no cap |

> **Cloudflare Tunnel free tier**: tiene un cap por defecto de 100s en respuestas. Si planeas hacer descargas de federación de archivos grandes, configura el tunnel con `connectTimeout: 24h` y considera un plan paid si los downloads se cortan.

---

## Migrar desde el setup actual (`deploy/nginx/hubplay.conf` + `deploy/setup-server.sh`)

Si ya tienes el setup actual corriendo:

1. Tu config existente en `deploy/nginx/hubplay.conf` sigue funcionando — **no la borres**.
2. Cuando vayas a habilitar federación (fase 1+), copia el contenido de `deploy/reverse-proxy/nginx/hubplay.conf` encima del que ya tienes. Sobrescribe.
3. Reload nginx: `nginx -s reload`.

La diferencia entre las dos versiones es la `location /api/v1/peer/` que tolera long-lived requests sin rate limiting. Hasta que actives federación, no se nota la diferencia.
