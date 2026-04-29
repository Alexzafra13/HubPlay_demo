# Nginx — control manual + certbot

Si ya operas nginx en este host (o prefieres su madurez sobre Caddy), esta es la config refinada con soporte para federación incluido.

## Diferencias vs `deploy/nginx/hubplay.conf` (original)

La config bajo este directorio añade **una sola location**: `/api/v1/peer/` para el tráfico de federación. Sin rate limit WAN, sin body cap, timeout 24h para descargas largas. Si no usas federación todavía, la diferencia es invisible.

Migración desde el setup actual:
```bash
sudo cp hubplay.conf /etc/nginx/sites-available/hubplay
sudo nginx -t          # valida syntax
sudo nginx -s reload
```

Eso es todo. Hasta que actives federación en el admin de HubPlay, el nuevo `/api/v1/peer/` queda dormido.

## Setup desde cero

```bash
# 1. Instalar nginx + certbot
sudo apt update
sudo apt install -y nginx certbot python3-certbot-nginx

# 2. Copiar config
sudo cp hubplay.conf /etc/nginx/sites-available/hubplay
# Editar el server_name y los paths SSL al final del fichero
sudo nano /etc/nginx/sites-available/hubplay

# 3. Activar y desactivar la default
sudo ln -sf /etc/nginx/sites-available/hubplay /etc/nginx/sites-enabled/
sudo rm -f /etc/nginx/sites-enabled/default
sudo nginx -t

# 4. Obtener cert (parar nginx primero)
sudo systemctl stop nginx
sudo certbot certonly --standalone -d tu-dominio.com --email tu@email.com --agree-tos
sudo systemctl start nginx

# 5. Verificar renovación automática (certbot timer ya viene activado en Ubuntu 22.04+)
sudo certbot renew --dry-run
```

## Variantes de despliegue

### A) Nginx en el host, HubPlay en Docker

Es el patrón del `setup-server.sh` original. nginx escucha en 80/443 y proxea a `127.0.0.1:8096` donde está el HubPlay del contenedor (mapeado solo a localhost). Es el modo más sencillo.

### B) Nginx en Docker, HubPlay en Docker (mismo compose)

Útil si quieres todo containerizado. Pega ambos servicios en `docker-compose.prod.yml` (ya lo hace el del proyecto). El cert hay que montarlo read-only desde `/etc/letsencrypt/live/...:ro` para que nginx lo lea sin tocarlo.

### C) Nginx Proxy Manager (interfaz gráfica)

Si prefieres no tocar `.conf` a mano, [Nginx Proxy Manager](https://nginxproxymanager.com/) tiene UI web. Puedes copiar la `location /api/v1/peer/` específica al "Custom Nginx Configuration" del proxy host de HubPlay para conservar el soporte de federación.

## Verificación

```bash
# 1. Frontend accesible
curl -I https://tu-dominio.com/                                   # 200 OK

# 2. Health check
curl -I https://tu-dominio.com/api/v1/health                      # 200 OK

# 3. SSE no se corta (cancelar con Ctrl+C tras 5s)
curl -N -H "Authorization: Bearer xxx" https://tu-dominio.com/api/v1/me/events

# 4. Stream endpoint sin buffering (responde inmediatamente con 401 sin auth, no cuelga)
curl -I https://tu-dominio.com/api/v1/stream/probe

# 5. Peer endpoint (cuando habilites federación) — sin rate limit WAN
for i in {1..50}; do curl -s -o /dev/null -w "%{http_code}\n" https://tu-dominio.com/api/v1/peer/info; done
# Debe responder consistente (probablemente 401 o 200 según fase), NUNCA 503/429.
```

## Hardening adicional

Activa en producción seria:

```nginx
# Forzar TLS 1.3 only (deja fuera Android <7, iOS <12)
ssl_protocols TLSv1.3;

# HSTS preload (después de verificar que TODO funciona en HTTPS — un misclick aquí te bloquea durante meses)
add_header Strict-Transport-Security "max-age=63072000; includeSubDomains; preload" always;

# CSP estricta (requiere ajustar al inventario de orígenes externos: TMDb posters, Fanart, etc.)
# Mira docs/architecture/security.md antes de aplicar.
```

## Troubleshooting

**504 Gateway Timeout en SSE**: tu nginx tiene `proxy_read_timeout` heredado más bajo que el que pone esta config. Verifica con `nginx -T | grep proxy_read_timeout` que aparece `5m` en el `location /`.

**429 al hacer pairing entre servidores**: la `location /api/v1/peer/` no está aplicando — probablemente porque está después del `location /` en el orden. Nginx evalúa por orden y `location /` matchea todo si está antes. Asegura que las locations específicas (`/api/v1/peer/`, `/api/v1/stream/`, `/api/v1/ws`) van **antes** del `location /` catch-all en el fichero.

**Cert no renueva**: `sudo systemctl status certbot.timer` y `sudo journalctl -u certbot --since "1 month ago"`. El renewal hook que recarga nginx después de renovar está en `/etc/letsencrypt/renewal-hooks/deploy/`.
