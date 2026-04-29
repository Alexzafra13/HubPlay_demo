# Traefik — labels en docker-compose

Traefik es la opción si ya corres un homelab con varios servicios y prefieres routing por labels en lugar de ficheros .conf.

## Pre-requisitos

- Una instancia de Traefik corriendo en el mismo Docker host.
- Un `certresolver` configurado en tu Traefik (típicamente `letsencrypt` con DNS challenge o HTTP-01 challenge).
- Una red Docker externa que tu Traefik observe (típicamente `traefik_proxy`).

Si no tienes Traefik todavía, **es más esfuerzo que Caddy** — sólo merece la pena si vas a tener varios servicios detrás del mismo proxy.

## Uso

```bash
# 1. Copiar el override file al directorio de despliegue.
cp docker-compose.override.yml /opt/hubplay/

# 2. Editar el dominio y el certResolver.
nano /opt/hubplay/docker-compose.override.yml
# (sustituye hubplay.example.com y `letsencrypt` por tu certresolver)

# 3. Asegúrate de que la red traefik_proxy existe y tu Traefik la observa.
docker network ls | grep traefik_proxy   # debe aparecer
# Si no:
docker network create traefik_proxy

# 4. Levantar HubPlay con el override aplicado.
cd /opt/hubplay
docker compose -f docker-compose.prod.yml -f docker-compose.override.yml up -d
```

El servicio `nginx` del compose original queda con `profiles: [disabled]`, así que Docker Compose lo ignora — Traefik hace de proxy.

## Verificación

Desde tu Traefik dashboard (típicamente `https://traefik.tu-host.local`), debes ver:

- Router `hubplay@docker` apuntando a `http://hubplay:8096`.
- Router `hubplay-login@docker` con middleware `hubplay-loginlimit`.
- Middleware `hubplay-headers@docker` con todos los security headers.

Y desde fuera:
```bash
curl -I https://tu-dominio.com/api/v1/health   # 200 OK
```

## Federación

Las labels de este override están preparadas:

- **No hay rate limiting WAN** sobre `/api/v1/peer/` — sólo el middleware `hubplay-headers`. La aplicación hace per-peer token buckets internamente.
- **`flushInterval=100ms`** garantiza que los SSE de federación (`/peer/events`, cuando llegue la fase 2) flujen sin acumular.
- **Sin maxRequestBody cap** — Traefik por defecto no impone uno, perfecto para descargas grandes de federación.

## Hardening de producción

Añade middleware adicional para production seria:

```yaml
# rate limit GLOBAL para defensa contra L7 DDoS — más generoso que el de login,
# por encima de cualquier uso humano legítimo.
- "traefik.http.middlewares.hubplay-globallimit.ratelimit.average=600"
- "traefik.http.middlewares.hubplay-globallimit.ratelimit.period=1m"
- "traefik.http.middlewares.hubplay-globallimit.ratelimit.burst=200"

# Aplicar al router default (NO al de federación).
- "traefik.http.routers.hubplay.middlewares=hubplay-headers,hubplay-globallimit"
```

**No apliques rate limit al `PathPrefix(/api/v1/peer/)`** — los peers son trusted por Ed25519 y el rate limit lo hace el server a nivel app.

## Troubleshooting

**404 en `/`**: `traefik.docker.network` no coincide con la red que Traefik observa. Verifica con `docker inspect traefik` qué red(es) está observando vs lo que pones en el label.

**Cert no se obtiene**: tu certResolver de Traefik no está bien configurado. Mira los logs de Traefik (`docker logs traefik`). Si usas DNS challenge, necesitas las creds de tu DNS provider en el environment de Traefik.

**SSE se cortan a los 30s**: tu Traefik tiene `transport.respondingTimeouts.idleTimeout` bajo en la static config. Súbelo a `30m` mínimo, o `0s` (sin timeout) si confías en los keepalives del backend.
