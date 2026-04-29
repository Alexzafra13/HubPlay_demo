# Caddy — turnkey HTTPS

Caddy es la opción más rápida si **no tienes proxy todavía** y tu host es alcanzable desde internet en los puertos 80 y 443.

## Por qué Caddy

- Auto-Let's Encrypt sin certbot, sin renewal hooks, sin systemd timer.
- WebSocket y SSE funcionan sin configuración extra.
- Config humano-legible: 30 líneas vs ~150 de nginx para lo mismo.
- Renovación de certificados en segundo plano mientras corre.

## Instalación rápida (Ubuntu/Debian)

```bash
# 1. Instalar Caddy desde el repo oficial.
sudo apt install -y debian-keyring debian-archive-keyring apt-transport-https
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' \
  | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' \
  | sudo tee /etc/apt/sources.list.d/caddy-stable.list
sudo apt update
sudo apt install -y caddy

# 2. Copiar tu Caddyfile.
sudo cp Caddyfile /etc/caddy/Caddyfile

# 3. Editar dominio y email.
sudo nano /etc/caddy/Caddyfile
# (sustituye hubplay.example.com y admin@example.com)

# 4. Reload.
sudo systemctl reload caddy

# 5. Verifica.
curl -I https://tu-dominio.com/api/v1/health
# Debe responder 200 OK.
```

## Instalación con Docker

Si prefieres correr Caddy en contenedor (para igualar el patrón de tu HubPlay), usa el `docker-compose.example.yml` de este directorio:

```bash
cp docker-compose.example.yml /opt/caddy/docker-compose.yml
nano /opt/caddy/docker-compose.yml   # ajusta dominio + email
cd /opt/caddy
docker compose up -d
```

El contenedor Caddy guarda los certificados en un volumen persistente — sobreviven a `docker compose down/up`.

## Verificación post-instalación

Desde el propio host:
```bash
curl -I https://tu-dominio.com/api/v1/health      # 200 OK
curl -I https://tu-dominio.com/api/v1/me/events   # 401 (requiere auth, pero responde, no cuelga)
```

Desde fuera (móvil con datos del operador, no wifi de casa):
```bash
curl -I https://tu-dominio.com/api/v1/health      # 200 OK
```

Si el segundo curl falla, tu router no está port-forwarding 80/443 → mira [`../cloudflare-tunnel/`](../cloudflare-tunnel/) o [`../tailscale/`](../tailscale/).

## Habilitar federación (cuando llegue la fase 1)

Esta config ya está lista para federación:
- `flush_interval -1` deja pasar SSE (peer events stream).
- `read_timeout 24h` deja pasar descargas grandes.
- Sin rate limiting WAN-style — el server hace per-peer token buckets a nivel app.

No tienes que tocar nada cuando se active. Solo configura tus peers desde el admin de HubPlay.

## Caddy + WAF / fail2ban

Si quieres rate limiting estilo nginx, opciones por orden de complejidad:
1. Plugin oficial `caddy-ratelimit` — descomentar el bloque del Caddyfile.
2. fail2ban delante (puerto 80/443) leyendo `/var/log/caddy/hubplay.log` (formato JSON, ya configurado).
3. Cloudflare gratis delante con WAF rules — combina bien con Tunnel.

## Troubleshooting

**"too many redirects"**: tu HubPlay en el contenedor está intentando redirigir HTTP→HTTPS también, conflicto con Caddy. Asegura que `HUBPLAY_SERVER_TRUSTED_PROXIES=127.0.0.1` (o el IP del host Caddy) está en el environment del contenedor de HubPlay.

**Cert no se obtiene**: revisa que el puerto 80 esté libre antes de Caddy arrancar y que tu DNS A record apunta al host. `caddy validate` te dice si la config es OK; los logs (`journalctl -u caddy -f`) muestran el flujo Let's Encrypt.

**SSE conexiones se cortan**: confirma que `flush_interval -1` está dentro del bloque `reverse_proxy`. Si se queda fuera no aplica.
