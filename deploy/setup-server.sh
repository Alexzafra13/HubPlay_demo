#!/bin/bash
# setup-server.sh — Configuración inicial del servidor para HubPlay
#
# Este script:
#   1. Instala dependencias (nginx, certbot, docker)
#   2. Obtiene certificado SSL de Let's Encrypt
#   3. Configura nginx como reverse proxy
#   4. Configura renovación automática de certificados
#   5. Levanta HubPlay con Docker Compose
#
# Uso:
#   chmod +x setup-server.sh
#   sudo ./setup-server.sh
#
# Requisitos previos:
#   - Ubuntu/Debian con acceso root
#   - Puerto 80 y 443 abiertos en el router y apuntando a este servidor
#   - hubplay.duckdns.org apuntando a la IP pública del servidor
#   - Docker y Docker Compose instalados

set -euo pipefail

DOMAIN="hubplay.duckdns.org"
EMAIL="${CERTBOT_EMAIL:-}"     # Configura tu email o pásalo como variable de entorno
DEPLOY_DIR="$(cd "$(dirname "$0")" && pwd)"

# Colores para output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log()  { echo -e "${GREEN}[✓]${NC} $1"; }
warn() { echo -e "${YELLOW}[!]${NC} $1"; }
err()  { echo -e "${RED}[✗]${NC} $1"; exit 1; }

# ──────────────────────────────────────────────
# 0. Verificaciones previas
# ──────────────────────────────────────────────
if [[ $EUID -ne 0 ]]; then
    err "Este script debe ejecutarse como root (sudo ./setup-server.sh)"
fi

if ! command -v docker &>/dev/null; then
    err "Docker no está instalado. Instálalo primero: https://docs.docker.com/engine/install/"
fi

if ! docker compose version &>/dev/null; then
    err "Docker Compose v2 no está instalado. Instálalo: https://docs.docker.com/compose/install/"
fi

# ──────────────────────────────────────────────
# 1. Instalar nginx y certbot
# ──────────────────────────────────────────────
log "Instalando nginx y certbot..."
apt-get update -qq
apt-get install -y -qq nginx certbot python3-certbot-nginx

# ──────────────────────────────────────────────
# 2. Parar nginx temporalmente para certbot standalone
# ──────────────────────────────────────────────
log "Parando nginx para obtener certificado..."
systemctl stop nginx 2>/dev/null || true

# ──────────────────────────────────────────────
# 3. Obtener certificado SSL
# ──────────────────────────────────────────────
if [[ -d "/etc/letsencrypt/live/$DOMAIN" ]]; then
    warn "Ya existe un certificado para $DOMAIN, saltando..."
else
    log "Obteniendo certificado SSL para $DOMAIN..."

    CERTBOT_ARGS=(certonly --standalone -d "$DOMAIN" --non-interactive --agree-tos)

    if [[ -n "$EMAIL" ]]; then
        CERTBOT_ARGS+=(--email "$EMAIL")
    else
        CERTBOT_ARGS+=(--register-unsafely-without-email)
        warn "No se configuró email. Para recibir avisos de renovación, usa: CERTBOT_EMAIL=tu@email.com"
    fi

    certbot "${CERTBOT_ARGS[@]}" || err "Fallo al obtener certificado. Verifica que el puerto 80 está abierto y $DOMAIN apunta a este servidor."

    log "Certificado SSL obtenido correctamente"
fi

# ──────────────────────────────────────────────
# 4. Configurar nginx
# ──────────────────────────────────────────────
log "Configurando nginx..."

# Crear directorio para challenges de renovación
mkdir -p /var/www/certbot

# Copiar config de nginx
cp "$DEPLOY_DIR/nginx/hubplay.conf" /etc/nginx/sites-available/hubplay

# Desactivar config default, activar hubplay
rm -f /etc/nginx/sites-enabled/default
ln -sf /etc/nginx/sites-available/hubplay /etc/nginx/sites-enabled/hubplay

# Verificar config de nginx
nginx -t || err "Error en la configuración de nginx"

# Arrancar nginx
systemctl enable nginx
systemctl start nginx
log "Nginx configurado y arrancado"

# ──────────────────────────────────────────────
# 5. Configurar renovación automática de certificados
# ──────────────────────────────────────────────
log "Configurando renovación automática de certificados..."

# Certbot ya instala un timer de systemd, pero nos aseguramos de que
# recargue nginx después de renovar
cat > /etc/letsencrypt/renewal-hooks/deploy/reload-nginx.sh << 'HOOK'
#!/bin/bash
systemctl reload nginx
HOOK
chmod +x /etc/letsencrypt/renewal-hooks/deploy/reload-nginx.sh

# Verificar que el timer está activo
systemctl enable certbot.timer 2>/dev/null || true
systemctl start certbot.timer 2>/dev/null || true
log "Renovación automática configurada"

# ──────────────────────────────────────────────
# 6. Levantar HubPlay
# ──────────────────────────────────────────────
log "Levantando HubPlay..."
cd "$DEPLOY_DIR"
docker compose -f docker-compose.prod.yml up -d --build

# Esperar a que esté healthy
log "Esperando a que HubPlay arranque..."
for i in $(seq 1 30); do
    if docker inspect hubplay --format='{{.State.Health.Status}}' 2>/dev/null | grep -q healthy; then
        break
    fi
    sleep 2
done

if docker inspect hubplay --format='{{.State.Health.Status}}' 2>/dev/null | grep -q healthy; then
    log "HubPlay está funcionando correctamente"
else
    warn "HubPlay aún no reporta healthy. Revisa: docker compose -f docker-compose.prod.yml logs hubplay"
fi

# ──────────────────────────────────────────────
# 7. Verificación final
# ──────────────────────────────────────────────
echo ""
echo "════════════════════════════════════════════════════════════"
echo ""
log "Despliegue completado"
echo ""
echo "  HubPlay:  https://$DOMAIN"
echo "  Health:   https://$DOMAIN/api/v1/health"
echo ""
echo "  Comandos útiles:"
echo "    docker compose -f docker-compose.prod.yml logs -f     # Ver logs"
echo "    docker compose -f docker-compose.prod.yml restart      # Reiniciar"
echo "    docker compose -f docker-compose.prod.yml down          # Parar"
echo "    certbot renew --dry-run                                 # Probar renovación SSL"
echo ""
echo "════════════════════════════════════════════════════════════"
