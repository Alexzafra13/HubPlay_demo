#!/usr/bin/env bash
#
# HubPlay — installer para servidores Linux (systemd).
#
# Uso:
#   curl -fsSL https://github.com/Alexzafra13/HubPlay_demo/releases/latest/download/install.sh | sudo bash
#
# Variables soportadas:
#   HUBPLAY_VERSION   versión a instalar (tag GitHub, e.g. v0.1.0 o "nightly").
#                     Default: "latest" → resuelve el último release estable.
#   HUBPLAY_PREFIX    prefijo de instalación. Default: /usr/local/bin para el
#                     binario; /etc/hubplay para config; /var/lib/hubplay para datos.
#   HUBPLAY_USER      usuario sistema bajo el que correr. Default: hubplay.
#   HUBPLAY_PORT      puerto a anunciar en el mensaje final (no cambia el yaml).
#                     Default: 8096.
#   HUBPLAY_SKIP_START   si "1", no arranca el servicio al terminar. Útil si
#                        quieres editar el yaml antes.
#   HUBPLAY_SKIP_VERIFY  si "1", permite instalar aunque el .sha256 del
#                        release no se pueda descargar. Por defecto la
#                        verificación es obligatoria (fail-closed).
#
# Idempotente: ejecutarlo dos veces es seguro. Detecta versión instalada y
# hace upgrade in-place (descarga, para servicio, sustituye binarios, arranca).
#
# Plataformas soportadas:
#   - Cualquier Linux x86_64 / aarch64 con systemd y glibc.
#   - NO Synology, Unraid, QNAP — usa Docker desde el Package Center.
#   - NO Alpine (musl libc) — usa Docker.
#
# El script falla cerrado: si algo va mal a mitad, NO deja un estado roto.
# Cualquier paso destructivo es idempotente.

set -euo pipefail

# ─── Configuración por defecto ────────────────────────────────────────
REPO="${HUBPLAY_REPO:-Alexzafra13/HubPlay_demo}"
VERSION="${HUBPLAY_VERSION:-latest}"
BIN_DIR="/usr/local/bin"
CONF_DIR="/etc/hubplay"
DATA_DIR="/var/lib/hubplay"
LOG_DIR="/var/log/hubplay"
SVC_USER="${HUBPLAY_USER:-hubplay}"
SVC_NAME="hubplay"
PORT="${HUBPLAY_PORT:-8096}"
SKIP_START="${HUBPLAY_SKIP_START:-0}"

# ─── UI helpers ───────────────────────────────────────────────────────
# Colores sólo si stdout es TTY. Curl-pipe-bash NO es TTY, así que en ese
# caso el script imprime plano sin ANSI — más limpio en logs.
if [[ -t 1 ]]; then
	BOLD=$(printf '\033[1m'); DIM=$(printf '\033[2m'); RESET=$(printf '\033[0m')
	GREEN=$(printf '\033[32m'); RED=$(printf '\033[31m'); YELLOW=$(printf '\033[33m')
	BLUE=$(printf '\033[34m')
else
	BOLD=""; DIM=""; RESET=""; GREEN=""; RED=""; YELLOW=""; BLUE=""
fi

ok()   { printf "${GREEN}✓${RESET} %s\n" "$*"; }
warn() { printf "${YELLOW}⚠${RESET} %s\n" "$*"; }
err()  { printf "${RED}✗${RESET} %s\n" "$*" >&2; }
step() { printf "${BLUE}→${RESET} %s\n" "$*"; }
note() { printf "${DIM}  %s${RESET}\n" "$*"; }

# ─── Plataforma & arquitectura ────────────────────────────────────────
detect_arch() {
	local m
	m="$(uname -m)"
	case "$m" in
		x86_64|amd64)  echo "amd64" ;;
		aarch64|arm64) echo "arm64" ;;
		*)
			err "Arquitectura no soportada: $m"
			err "HubPlay soporta amd64 (x86_64) y arm64 (aarch64)."
			exit 1
			;;
	esac
}

# Detecta plataformas que NO soportamos nativamente y guía al usuario
# a Docker. Es MEJOR negarse explícito que romper a mitad de instalación.
check_unsupported_platforms() {
	# Synology DSM
	if [[ -f /etc/synoinfo.conf ]] || [[ -d /usr/syno ]]; then
		err "Detectado Synology DSM."
		note "HubPlay no tiene paquete nativo SPK todavía."
		note "Instala Docker desde Package Center y usa la imagen:"
		note ""
		note "  docker pull ghcr.io/alexzafra13/hubplay_demo:latest"
		note ""
		note "Más info: https://github.com/${REPO}#docker"
		exit 1
	fi

	# Unraid
	if [[ -f /etc/unraid-version ]]; then
		err "Detectado Unraid."
		note "Instala HubPlay desde Community Applications → Docker."
		note "La imagen es: ghcr.io/alexzafra13/hubplay_demo:latest"
		exit 1
	fi

	# QNAP
	if [[ -d /share/CACHEDEV1_DATA ]] || [[ -f /etc/config/uLinux.conf ]]; then
		err "Detectado QNAP."
		note "Instala Container Station desde App Center y usa Docker."
		note "Imagen: ghcr.io/alexzafra13/hubplay_demo:latest"
		exit 1
	fi

	# TrueNAS Core/Scale
	if [[ -f /etc/version ]] && grep -qi "truenas" /etc/version 2>/dev/null; then
		err "Detectado TrueNAS."
		note "Usa el plugin/app oficial cuando esté disponible, o Docker via Apps."
		exit 1
	fi

	# Alpine (musl)
	if [[ -f /etc/alpine-release ]]; then
		err "Detectado Alpine Linux."
		note "Nuestros binarios usan glibc; no compatible con musl."
		note "Usa la imagen Docker: ghcr.io/alexzafra13/hubplay_demo:latest"
		exit 1
	fi
}

# Verifica systemd disponible. Sin él no podemos registrar el servicio.
check_systemd() {
	if ! command -v systemctl >/dev/null 2>&1; then
		err "systemctl no está disponible — necesitamos systemd."
		note "Si tu distro usa OpenRC, SysV init o runit, abre un issue:"
		note "  https://github.com/${REPO}/issues"
		note "Mientras tanto, puedes correr hubplay manualmente desde el .tar.gz."
		exit 1
	fi
	if [[ ! -d /run/systemd/system ]]; then
		err "El sistema no está corriendo systemd como PID 1."
		exit 1
	fi
}

# Verifica que el script corre con privilegios suficientes. Necesitamos
# poder escribir a /usr/local/bin, /etc, crear usuario, registrar servicio.
check_root() {
	if [[ $EUID -ne 0 ]]; then
		err "Este script debe correr como root (o con sudo)."
		note "Ejemplo:"
		note "  curl -fsSL https://github.com/${REPO}/releases/latest/download/install.sh | sudo bash"
		exit 1
	fi
}

# Herramientas básicas del script. curl + tar + (sha256sum|shasum) son
# universalmente disponibles. Listamos las que faltan en vez de pedirlas
# de una en una — mejor UX si el operador tiene que instalar varias.
check_required_tools() {
	local missing=()
	for cmd in curl tar getent useradd; do
		command -v "$cmd" >/dev/null 2>&1 || missing+=("$cmd")
	done
	# sha256: aceptamos cualquiera de los dos
	if ! command -v sha256sum >/dev/null 2>&1 && ! command -v shasum >/dev/null 2>&1; then
		missing+=("sha256sum o shasum")
	fi
	if (( ${#missing[@]} > 0 )); then
		err "Faltan comandos requeridos: ${missing[*]}"
		note "Instálalos primero (en Debian/Ubuntu: apt install curl tar coreutils passwd)."
		exit 1
	fi
}

# Devuelve la URL del tarball + la del sha256 para la versión solicitada.
# Si VERSION=latest, GitHub redirige solo desde /releases/latest/download/...
# a la versión real más reciente — no tenemos que llamar a la API.
build_download_urls() {
	local arch="$1"
	local tarball_name sha_name base
	if [[ "$VERSION" == "latest" ]]; then
		base="https://github.com/${REPO}/releases/latest/download"
		# El nombre del tarball incluye la versión, que en "latest" no
		# conocemos a priori. GitHub no expone wildcards; usamos la API
		# para resolver el tag exacto. Una sola llamada, anónima.
		local tag
		tag="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
		      | grep -oE '"tag_name":[[:space:]]*"[^"]+"' \
		      | head -1 \
		      | sed -E 's/.*"([^"]+)"$/\1/')"
		if [[ -z "$tag" ]]; then
			err "No se pudo resolver la versión 'latest' desde GitHub API."
			err "Reintenta con HUBPLAY_VERSION=v0.X.Y explícito."
			exit 1
		fi
		VERSION="$tag"
	fi
	tarball_name="hubplay-${VERSION}-linux-${arch}.tar.gz"
	sha_name="${tarball_name}.sha256"
	echo "https://github.com/${REPO}/releases/download/${VERSION}/${tarball_name}"
	echo "https://github.com/${REPO}/releases/download/${VERSION}/${sha_name}"
	echo "$tarball_name"
}

# Verifica sha256 portable (sha256sum o shasum -a 256).
verify_sha256() {
	local file="$1" sha_file="$2"
	if command -v sha256sum >/dev/null 2>&1; then
		# Los .sha256 generados por el workflow incluyen el filename
		# tras el hash. Reescribirlos al filename actual (puede haber
		# diferencias de path si descargamos a /tmp) y verificar.
		( cd "$(dirname "$file")" && \
		  awk '{print $1}' "$sha_file" \
		  | sed "s|\$| $(basename "$file")|" \
		  | sha256sum -c --status - )
	else
		( cd "$(dirname "$file")" && \
		  awk '{print $1}' "$sha_file" \
		  | sed "s|\$| $(basename "$file")|" \
		  | shasum -a 256 -c --status - )
	fi
}

# Crea usuario sistema sin login si no existe. -r = system user, -s
# /usr/sbin/nologin = no shell, -d = home en DATA_DIR para que ahí caigan
# los archivos por defecto. Idempotente: si ya existe, no hace nada.
ensure_user() {
	if getent passwd "$SVC_USER" >/dev/null 2>&1; then
		ok "Usuario sistema '$SVC_USER' ya existe"
	else
		useradd --system \
			--home-dir "$DATA_DIR" \
			--shell /usr/sbin/nologin \
			--comment "HubPlay media server" \
			"$SVC_USER"
		ok "Usuario sistema '$SVC_USER' creado"
	fi
}

# Crea directorios con permisos correctos. /etc/hubplay queda 755 (config
# legible para auditar). /var/lib/hubplay queda 750 (datos privados del
# servicio). /var/log/hubplay queda 750.
ensure_dirs() {
	install -d -m 755 -o root -g root "$CONF_DIR"
	install -d -m 750 -o "$SVC_USER" -g "$SVC_USER" "$DATA_DIR"
	install -d -m 750 -o "$SVC_USER" -g "$SVC_USER" "$LOG_DIR"
}

# Renderiza /etc/hubplay/hubplay.yaml desde el ejemplo del tarball SI no
# existe ya. Upgrades respetan el yaml editado por el operador — no
# pisamos jamás un yaml en uso.
ensure_config() {
	local source_yaml="$1"
	local target="$CONF_DIR/hubplay.yaml"
	if [[ -f "$target" ]]; then
		ok "Config ya existente en $target — no se modifica"
		return
	fi
	# Editar el yaml de ejemplo para apuntar a los dirs del sistema
	# en lugar de los relativos del .tar.gz (./data, ./cache, etc.).
	# Cambios mínimos: data_dir / cache_dir absolutos, addr 0.0.0.0:PORT.
	# El operador puede tunear más tarde — esto es sólo el primer arranque.
	sed \
		-e "s|^\([[:space:]]*data_dir:\).*|\1 \"${DATA_DIR}/data\"|" \
		-e "s|^\([[:space:]]*cache_dir:\).*|\1 \"${DATA_DIR}/cache\"|" \
		-e "s|^\([[:space:]]*image_dir:\).*|\1 \"${DATA_DIR}/images\"|" \
		-e "s|^\([[:space:]]*path:\).*hubplay.db.*|\1 \"${DATA_DIR}/hubplay.db\"|" \
		"$source_yaml" > "$target"
	chmod 640 "$target"
	chown root:"$SVC_USER" "$target"
	ok "Config inicial creada en $target"
}

# Instala el unit file de systemd. Sustituye placeholders del unit por
# los valores reales de las variables (USER, paths).
install_systemd_unit() {
	local source_unit="$1"
	local target="/etc/systemd/system/${SVC_NAME}.service"
	# Si existe y es idéntico, no tocar.
	if [[ -f "$target" ]] && cmp -s "$source_unit" "$target"; then
		ok "Unit de systemd ya actualizado"
		return
	fi
	sed \
		-e "s|@HUBPLAY_USER@|${SVC_USER}|g" \
		-e "s|@HUBPLAY_BIN@|${BIN_DIR}/hubplay|g" \
		-e "s|@HUBPLAY_CONF@|${CONF_DIR}/hubplay.yaml|g" \
		-e "s|@HUBPLAY_DATA@|${DATA_DIR}|g" \
		"$source_unit" > "$target"
	chmod 644 "$target"
	systemctl daemon-reload
	ok "Unit systemd instalado en $target"
}

# Detecta versión instalada (si la hay) parseando `hubplay --version`.
# Devuelve cadena vacía si no hay binario o no soporta --version.
installed_version() {
	if [[ -x "${BIN_DIR}/hubplay" ]]; then
		"${BIN_DIR}/hubplay" --version 2>/dev/null | head -1 || true
	fi
}

# ─── Main flow ────────────────────────────────────────────────────────
main() {
	printf "${BOLD}HubPlay installer${RESET}\n"
	printf "${DIM}  https://github.com/${REPO}${RESET}\n\n"

	check_root
	check_unsupported_platforms
	check_systemd
	check_required_tools

	local arch
	arch="$(detect_arch)"
	ok "Detectado: Linux $arch con systemd"

	# Versión previa, si la hay.
	local prev
	prev="$(installed_version)"
	if [[ -n "$prev" ]]; then
		note "Versión instalada: $prev"
	fi

	# Resolver URLs.
	step "Resolviendo última versión..."
	local urls
	urls="$(build_download_urls "$arch")"
	local tarball_url sha_url tarball_name
	tarball_url="$(sed -n '1p' <<<"$urls")"
	sha_url="$(sed -n '2p' <<<"$urls")"
	tarball_name="$(sed -n '3p' <<<"$urls")"
	ok "Objetivo: $VERSION"

	# Descargar a temp.
	local tmp
	tmp="$(mktemp -d)"
	trap 'rm -rf "$tmp"' EXIT

	step "Descargando $tarball_name..."
	curl -fsSL --progress-bar -o "$tmp/$tarball_name" "$tarball_url" || {
		err "Descarga falló — comprueba que la versión $VERSION existe en releases."
		exit 1
	}
	# El .sha256 ausente es FATAL: este script corre como root vía
	# `curl | sudo bash`, así que instalar un tarball sin verificar es
	# exactamente el escenario que un atacante con el CDN/red a su
	# favor querría. HUBPLAY_SKIP_VERIFY=1 existe como escape explícito
	# (mirrors propios, releases antiguos sin sidecar) — opt-in y bajo
	# responsabilidad del operador, nunca el default.
	if ! curl -fsSL -o "$tmp/$tarball_name.sha256" "$sha_url"; then
		if [[ "${HUBPLAY_SKIP_VERIFY:-0}" == "1" ]]; then
			warn "No se pudo descargar el .sha256 — HUBPLAY_SKIP_VERIFY=1, continúo SIN verificar."
		else
			err "No se pudo descargar el .sha256 — abortando (no instalo binarios sin verificar)."
			err "Si sabes lo que haces: HUBPLAY_SKIP_VERIFY=1 salta esta comprobación."
			exit 1
		fi
	fi

	if [[ -f "$tmp/$tarball_name.sha256" ]]; then
		step "Verificando sha256..."
		if verify_sha256 "$tmp/$tarball_name" "$tmp/$tarball_name.sha256"; then
			ok "Checksum válido"
		else
			err "Checksum NO coincide — descarga corrupta o comprometida."
			exit 1
		fi
	fi

	step "Extrayendo..."
	tar -xzf "$tmp/$tarball_name" -C "$tmp"
	# Estructura: tmp/hubplay-vX.Y.Z-linux-arch/{hubplay,ffmpeg,ffprobe,...}
	local extracted
	extracted="$(find "$tmp" -maxdepth 1 -type d -name 'hubplay-*-linux-*' | head -1)"
	if [[ -z "$extracted" ]] || [[ ! -f "$extracted/hubplay" ]]; then
		err "Tarball con estructura inesperada — abortando."
		exit 1
	fi
	ok "Extraído"

	# Si ya estaba instalado, parar el servicio antes de sobreescribir.
	if systemctl is-active --quiet "$SVC_NAME"; then
		step "Parando servicio para actualizar binarios..."
		systemctl stop "$SVC_NAME"
		ok "Servicio parado"
	fi

	ensure_user
	ensure_dirs

	# Copiar binarios. install(1) gestiona permisos + atomicidad.
	step "Instalando binarios en $BIN_DIR..."
	install -m 755 "$extracted/hubplay"  "$BIN_DIR/hubplay"
	install -m 755 "$extracted/ffmpeg"   "$BIN_DIR/ffmpeg"
	install -m 755 "$extracted/ffprobe"  "$BIN_DIR/ffprobe"
	ok "Binarios instalados"

	# Config + unit. El unit lo trae el tarball junto con un yaml-example.
	if [[ -f "$extracted/hubplay.example.yaml" ]]; then
		ensure_config "$extracted/hubplay.example.yaml"
	fi

	# El unit lo descargamos suelto del release (lo subimos junto con el
	# install.sh como asset). Lo bajamos aquí para que el script tenga
	# todo lo que necesita sin depender del contenido del tarball.
	step "Descargando unit de systemd..."
	curl -fsSL -o "$tmp/hubplay.service" \
		"https://github.com/${REPO}/releases/download/${VERSION}/hubplay.service" || {
		err "No se pudo descargar hubplay.service del release."
		exit 1
	}
	install_systemd_unit "$tmp/hubplay.service"

	# Arrancar (o no, si SKIP_START=1).
	if [[ "$SKIP_START" == "1" ]]; then
		warn "HUBPLAY_SKIP_START=1 → no arranco el servicio."
		note "Cuando quieras: sudo systemctl enable --now hubplay"
	else
		step "Habilitando y arrancando servicio..."
		systemctl enable "$SVC_NAME" >/dev/null 2>&1
		systemctl start "$SVC_NAME"
		sleep 2
		if systemctl is-active --quiet "$SVC_NAME"; then
			ok "Servicio activo"
		else
			err "El servicio no arrancó. Mira los logs:"
			err "  journalctl -u hubplay -n 50 --no-pager"
			exit 1
		fi
	fi

	# Final.
	printf "\n${BOLD}${GREEN}HubPlay instalado correctamente.${RESET}\n\n"
	printf "  ${BOLD}Accede en:${RESET}    http://%s:%s\n" "$(hostname -I 2>/dev/null | awk '{print $1}' || echo 'localhost')" "$PORT"
	printf "  ${BOLD}Logs:${RESET}         journalctl -u hubplay -f\n"
	printf "  ${BOLD}Restart:${RESET}      sudo systemctl restart hubplay\n"
	printf "  ${BOLD}Config:${RESET}       %s/hubplay.yaml\n" "$CONF_DIR"
	printf "  ${BOLD}Datos:${RESET}        %s\n" "$DATA_DIR"
	printf "  ${BOLD}Actualizar:${RESET}   vuelve a ejecutar este install.sh\n"
	printf "  ${BOLD}Desinstalar:${RESET}  sudo systemctl disable --now hubplay && \\\\\n"
	printf "                rm /etc/systemd/system/hubplay.service && \\\\\n"
	printf "                rm /usr/local/bin/{hubplay,ffmpeg,ffprobe}\n\n"
}

main "$@"
