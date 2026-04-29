# Apache 2.4 — para hosts LAMP existentes

Si ya corres Apache para otros servicios y prefieres no introducir un segundo proxy, esta config te da un vhost equivalente.

## Pre-requisitos

```bash
sudo apt install -y apache2 certbot python3-certbot-apache

# Módulos necesarios
sudo a2enmod proxy proxy_http proxy_wstunnel ssl headers rewrite

# El módulo proxy_wstunnel es CRÍTICO — sin él, los WebSockets no upgradean
# y la UI de HubPlay pierde los eventos en tiempo real.
```

## Setup

```bash
# 1. Copiar config
sudo cp hubplay.conf /etc/apache2/sites-available/hubplay.conf

# 2. Editar dominio
sudo nano /etc/apache2/sites-available/hubplay.conf

# 3. Activar el site, desactivar el default
sudo a2ensite hubplay
sudo a2dissite 000-default

# 4. Test syntax
sudo apache2ctl configtest

# 5. Obtener cert SSL
sudo systemctl stop apache2
sudo certbot certonly --standalone -d tu-dominio.com --email tu@email.com --agree-tos
sudo systemctl start apache2
```

## Verificación

```bash
curl -I https://tu-dominio.com/api/v1/health   # 200 OK
```

## Notas específicas de Apache para federación

1. **Orden de las directivas**: las `<Location>` específicas (`/api/v1/peer/`, `/api/v1/stream/`, etc.) tienen que ir **antes** del `ProxyPass /` catch-all. Apache evalúa en orden de aparición — un catch-all primero secuestra todo lo demás.

2. **WebSocket upgrade vía RewriteRule**: la sintaxis `RewriteRule ... [P]` con `RewriteCond` por header Upgrade es la única forma fiable en Apache 2.4 de hacer WebSocket pass-through. La alternativa `ProxyPassMatch` con `wss://` no respeta los Connection headers en algunas versiones.

3. **`proxy-sendchunked` para downloads**: Apache por defecto buffea la respuesta del backend antes de enviarla. Para descargas de federación que pueden ser multi-GB, este SetEnv del Location de peer fuerza chunked transfer encoding y streaming directo.

4. **`flushpackets=on` en SSE**: equivalente al `flush_interval` de Caddy / Traefik. Sin esto, Apache mantiene el output buffer por defecto y los eventos llegan en lotes en vez de inmediatamente.

## Hardening de producción

```apache
# Forzar TLS 1.3 only (deja fuera Android <7, iOS <12).
SSLProtocol -all +TLSv1.3

# HSTS preload — sólo después de verificar HTTPS extremo a extremo.
Header always set Strict-Transport-Security "max-age=63072000; includeSubDomains; preload"

# Server tokens — reduce el fingerprint de Apache en respuestas.
# (En /etc/apache2/conf-available/security.conf)
ServerTokens Prod
ServerSignature Off

# mod_evasive para rate limit primitivo (más simple que mod_security):
sudo apt install -y libapache2-mod-evasive
sudo a2enmod evasive
# Configurar /etc/apache2/mods-available/evasive.conf con DOSPageCount, DOSPageInterval, etc.
```

## Troubleshooting

**"Bad Gateway" en streaming**: probablemente `mod_proxy_http` no está cargado. `sudo a2enmod proxy_http && sudo systemctl restart apache2`.

**WebSockets no upgradean (HubPlay no recibe eventos en tiempo real)**: `mod_proxy_wstunnel` no está. `sudo a2enmod proxy_wstunnel`.

**SSE se cortan a los 60s**: tienes un timeout más bajo que el del Location matching. Verifica con `apache2ctl -S` qué vhost matchea y revisa el `Timeout` global en `apache2.conf` (default 300, suficiente para SSE keepalive cada 30s).

**Apache es N veces más lento que nginx en este caso de uso**: cierto. Apache es perfecto si ya corre Apache; si arrancas desde cero, plantéate Caddy o nginx.
