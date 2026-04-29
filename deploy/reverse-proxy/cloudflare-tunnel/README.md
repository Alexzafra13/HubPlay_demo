# Cloudflare Tunnel — para CGNAT y privacidad de IP

Si tu ISP usa CGNAT (cada vez más común con FTTH), o no puedes / no quieres abrir puertos en el router, Cloudflare Tunnel te da un endpoint público sin exponer tu IP. Es **gratis para uso personal** y funciona detrás de cualquier red.

> **Importante**: Cloudflare Tunnel free tier tiene cap de respuesta de 100 segundos por defecto. Para descargas de federación de archivos grandes, configura el túnel con timeout extendido (abajo) o considera Tailscale si tus peers son sólo amigos en mesh privada.

## Cómo funciona

```
[Tu HubPlay 127.0.0.1:8096]
         ↓ outbound TLS (no inbound)
[Cloudflared daemon en tu host]
         ↓ outbound TLS persistente
[Cloudflare edge]
         ↓ HTTPS público
[hubplay.tu-dominio.com]
         ↓
[Visitantes / peers]
```

Tu host **NO** acepta inbound de internet. El daemon `cloudflared` hace una conexión outbound persistente al edge de Cloudflare. Cuando alguien visita tu dominio, Cloudflare proxea por ese túnel.

## Setup (Ubuntu/Debian)

```bash
# 1. Instalar cloudflared.
curl -L --output cloudflared.deb \
  https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64.deb
sudo dpkg -i cloudflared.deb

# 2. Login (abre browser para auth con tu cuenta CF).
cloudflared tunnel login

# 3. Crear el túnel.
cloudflared tunnel create hubplay
# Anota el UUID que devuelve.

# 4. Crear config /etc/cloudflared/config.yml (ver abajo).
sudo mkdir -p /etc/cloudflared
sudo nano /etc/cloudflared/config.yml

# 5. Apuntar tu DNS al túnel.
cloudflared tunnel route dns hubplay hubplay.tu-dominio.com

# 6. Arrancar como service.
sudo cloudflared service install
sudo systemctl enable --now cloudflared
```

## Config `/etc/cloudflared/config.yml`

```yaml
tunnel: TU-UUID-AQUI
credentials-file: /etc/cloudflared/TU-UUID-AQUI.json

# Timeouts extendidos para SSE y descargas de federación.
# El default 100s corta SSE keepalive y descargas grandes.
ingress:
  - hostname: hubplay.tu-dominio.com
    service: http://127.0.0.1:8096
    originRequest:
      # SSE — keepalive es 30s en HubPlay; este 600s deja margen.
      connectTimeout: 30s
      tlsTimeout:     30s
      # Cap del response — 24h para descargas de federación.
      noHappyEyeballs: false
      keepAliveConnections: 100
      keepAliveTimeout: 90s
      httpHostHeader: hubplay.tu-dominio.com
      # NO buffering — flushpackets equivalente.
      disableChunkedEncoding: false
  # Catch-all (devuelve 404).
  - service: http_status:404
```

## Verificación

```bash
# Status del túnel.
cloudflared tunnel info hubplay

# Status del service.
sudo systemctl status cloudflared

# Test desde fuera.
curl -I https://hubplay.tu-dominio.com/api/v1/health
```

## Federación con Cloudflare Tunnel

Funciona para todos los flujos **excepto descargas muy largas** en el plan free:

- ✅ Pairing (handshake): instantáneo, sin problema.
- ✅ Catalog browsing: requests cortas.
- ✅ Streaming HLS: segments individuales <30s, ningún cap se nota.
- ✅ Live TV peering: igual, segments cortos.
- ⚠️ Download de archivos grandes: el cap free de respuesta de 100s puede cortar. Workaround:
  - Plan Cloudflare Tunnel paid (sin cap) — desde $5/mes.
  - O cambia a Tailscale si los peers son sólo amigos en mesh privada.

## Hardening con Cloudflare delante

Cloudflare te da gratis lo que Caddy/nginx requieren plugins:

- **WAF rules**: bloquea bots conocidos, agentes maliciosos. Config en CF dashboard.
- **Rate limiting**: por IP, por path, por método. CF dashboard → Security → Rate Limiting Rules.
- **Geo-blocking**: si tu uso es regional, bloquea continentes enteros.
- **Bot Fight Mode**: gratis para enterprise plan, plan free tiene "Super Bot Fight Mode" reducido.

**Importante**: NO actives rate limiting en `/api/v1/peer/*` desde CF — los peers son trusted, el rate limit es a nivel app. Si lo activas, romperás federación.

## Troubleshooting

**Túnel no conecta**: `journalctl -u cloudflared -f`. Errores típicos: token expirado (re-login), DNS no apuntando al túnel, otro proceso ya escuchando en cloudflared TCP/UDP de control.

**SSE cuelga después de 100s**: el cap del free tier. Verifica con `curl -N -H 'Authorization: Bearer xxx' -o /dev/null --max-time 200 -w '%{http_code} %{time_total}s\n' https://...api/v1/me/events` — si cae antes de 200s sin keepalive recibido, es el cap.

**Descargas grandes se cortan**: idem, cap del free tier. Plan paid o Tailscale.

## Cuándo NO usar Cloudflare Tunnel

- Si quieres autonomía total sin proveedor cloud (federación con peers que también quieren autonomía total — Tailscale es mejor match).
- Si tu uso necesita >100s de stream continuo — paid plan o nginx+IP pública.
- Si manejas datos sensibles y no quieres que CF tenga visibilidad de plaintext (CF descifra TLS para WAF; si esto es un dealbreaker, montar Tailscale).
