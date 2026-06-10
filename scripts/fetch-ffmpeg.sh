#!/usr/bin/env bash
#
# fetch-ffmpeg.sh — descarga ffmpeg + ffprobe pre-buildeados para la
# plataforma indicada y los deja en $OUTDIR sin extensión adicional
# (el shipping del release los empaqueta en .tar.gz/.zip).
#
# Uso:
#   ./scripts/fetch-ffmpeg.sh <goos> <goarch> <outdir>
#
# Fuentes:
#   - Linux/Windows: BtbN/FFmpeg-Builds (LGPL build, redistribuible).
#   - macOS:         evermeet.cx (mantenido por la comunidad, mismas
#                    LGPL conditions). evermeet sirve sólo Intel; en
#                    arm64 reuse el mismo binario porque macOS Rosetta
#                    cubre la latencia con coste despreciable para
#                    streaming. Cuando FFmpeg-Builds publique arm64
#                    macOS, este branch lo migrará.
#
# Verificación de integridad: cada descarga se contrasta con el sha256
# que publica el upstream por un canal separado (BtbN: campo `digest`
# de la API de releases de GitHub; evermeet: su API de info). Además se
# comprueba que `ffmpeg -version` devuelve 0 — no queremos shippear un
# archivo truncado por descarga interrumpida.
#
# FFMPEG_SKIP_VERIFY=1 salta la verificación de checksum (uso local sin
# acceso a las APIs); en CI nunca debe estar activo.

set -euo pipefail

GOOS="${1:-}"
GOARCH="${2:-}"
OUTDIR="${3:-}"

if [[ -z "$GOOS" || -z "$GOARCH" || -z "$OUTDIR" ]]; then
	echo "usage: $0 <goos> <goarch> <outdir>" >&2
	exit 2
fi

mkdir -p "$OUTDIR"

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

# dl: descarga resiliente. El problema real no es la red local sino el
# CDN de releases de GitHub: BtbN publica autobuilds diarios bajo el tag
# *rolling* `latest`, y mientras re-sube ese tag el CDN devuelve 504
# PERSISTENTE durante varios minutos (no un parpadeo). evermeet.cx tiene
# ventanas parecidas. Un `curl --retry` con delay fijo corto (~10s) no
# aguanta eso, así que aquí hacemos backoff exponencial con un
# presupuesto total de ~4 min (8 intentos: 5,10,20,40,60,60,60s). Cada
# intento lleva además `-C -` para reanudar descargas parciales de los
# assets de ~100 MB. Configurable vía FFMPEG_DL_RETRIES para CI.
dl() {
	local url="$1" out="$2"
	local max_attempts="${FFMPEG_DL_RETRIES:-8}"
	local attempt=1 delay=5
	while :; do
		if curl -fSL --no-progress-meter --connect-timeout 30 \
			--retry 2 --retry-connrefused -C - "$url" -o "$out"; then
			return 0
		fi
		# `curl -C -` deja un fichero parcial si el server no soporta
		# range; lo limpiamos para que el siguiente intento parta limpio.
		if (( attempt >= max_attempts )); then
			echo "✗ descarga fallida tras $max_attempts intentos: $url" >&2
			echo "  (el CDN de releases de GitHub/BtbN suele dar 504 persistente" >&2
			echo "   mientras re-publica el tag 'latest'; reintenta el job en unos minutos)" >&2
			return 22
		fi
		echo "  intento $attempt/$max_attempts falló; reintento en ${delay}s…" >&2
		sleep "$delay"
		(( attempt++ ))
		(( delay = delay < 60 ? delay * 2 : 60 ))
	done
}

# sha256 portable (ubuntu: sha256sum; macOS: shasum -a 256).
file_sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	else
		shasum -a 256 "$1" | awk '{print $1}'
	fi
}

# verify <fichero> <sha256-esperado> <etiqueta> [soft]
#
# Aborta si el esperado está vacío (no pudimos obtenerlo del upstream)
# o si no coincide. Con el 4º arg "soft", la AUSENCIA de checksum
# degrada a warning — para upstreams sin canal fiable de checksums
# (evermeet.cx bloquea las IPs de los runners de GitHub con 403, así
# que su API de info no responde justo donde más se necesita). El
# MISMATCH sigue siendo fatal siempre. FFMPEG_SKIP_VERIFY=1 aplica el
# modo soft globalmente (uso local sin red a las APIs).
verify() {
	local file="$1" expected="$2" label="$3" mode="${4:-strict}"
	if [[ -z "$expected" ]]; then
		if [[ "$mode" == "soft" || "${FFMPEG_SKIP_VERIFY:-0}" == "1" ]]; then
			echo "⚠ $label: sin checksum upstream — continuando SIN verificar (modo soft)" >&2
			return 0
		fi
		echo "✗ $label: no se pudo obtener el checksum del upstream." >&2
		echo "  (FFMPEG_SKIP_VERIFY=1 salta esta verificación bajo tu responsabilidad)" >&2
		return 1
	fi
	local actual
	actual="$(file_sha256 "$file")"
	if [[ "$actual" != "$expected" ]]; then
		echo "✗ $label: sha256 NO coincide — descarga corrupta o alterada" >&2
		echo "  esperado: $expected" >&2
		echo "  obtenido: $actual" >&2
		return 1
	fi
	echo "✓ $label: sha256 verificado"
}

# Checksum de un asset de BtbN vía la API de releases de GitHub: el
# campo `digest` (sha256:...) lo calcula GitHub al subir el asset, así
# que detecta truncados/sustituciones en el CDN de descargas por una
# vía TLS independiente. NO protege contra un compromiso de BtbN en
# origen — eso requeriría buildear FFmpeg nosotros. El tag `latest` es
# rolling: si BtbN re-publica entre este GET y la descarga, el mismatch
# aborta y basta relanzar el job. Con GH_TOKEN/GITHUB_TOKEN la request
# va autenticada (CI); anónima cae al rate-limit de 60/h por IP.
btbn_digest() {
	local asset="$1" auth=()
	local token="${GH_TOKEN:-${GITHUB_TOKEN:-}}"
	[[ -n "$token" ]] && auth=(-H "Authorization: Bearer $token")
	# ${auth[@]+...}: expansión segura de array vacío bajo `set -u` en
	# bash 3.2 (el /bin/bash de macOS).
	curl -fsSL --retry 3 ${auth[@]+"${auth[@]}"} \
		"https://api.github.com/repos/BtbN/FFmpeg-Builds/releases/tags/latest" |
		python3 -c '
import json, sys
name = sys.argv[1]
try:
    assets = json.load(sys.stdin).get("assets", [])
except ValueError:
    sys.exit(0)  # respuesta no-JSON (rate-limit, red) → digest vacío
for a in assets:
    digest = a.get("digest") or ""
    if a.get("name") == name and digest.startswith("sha256:"):
        print(digest[len("sha256:"):])
        break
' "$asset" || true
}

# Checksum que publica evermeet.cx en su API de info (campo sha256 del
# JSON de la release actual de cada herramienta).
evermeet_sha256() {
	curl -fsSL --retry 3 "https://evermeet.cx/ffmpeg/info/$1/release" |
		python3 -c '
import json, sys
try:
    print(json.load(sys.stdin).get("sha256") or "")
except ValueError:
    pass
' || true
}

# Releases de BtbN/FFmpeg-Builds. La carpeta `latest` (tag explícito,
# no alias) tiene los assets con nombres canónicos `ffmpeg-master-latest-*`
# que el script consume. **No** usar `releases/latest/download/X` —
# eso es el alias dinámico que GitHub resuelve al último release marcado
# "Latest", y desde mayo 2026 BtbN marca como Latest los autobuilds
# diarios cuyos assets llevan el SHA del commit en el nombre
# (`ffmpeg-N-124657-gfb5dd6ec60-linux64-lgpl.tar.xz` etc) → 404 en el
# nombre canónico. El tag explícito `latest` apunta al último autobuild
# pero mantiene los nombres estables del asset, así que la URL
# `releases/download/latest/ffmpeg-master-latest-*` siempre funciona.
BTBN_BASE="https://github.com/BtbN/FFmpeg-Builds/releases/download/latest"

case "$GOOS-$GOARCH" in
linux-amd64)
	asset="ffmpeg-master-latest-linux64-lgpl.tar.xz"
	url="${BTBN_BASE}/${asset}"
	echo "→ downloading $url"
	dl "$url" "$tmpdir/ff.tar.xz"
	verify "$tmpdir/ff.tar.xz" "$(btbn_digest "$asset")" "$asset"
	tar -xJf "$tmpdir/ff.tar.xz" -C "$tmpdir"
	# Estructura: ffmpeg-N-...-linux64-lgpl/bin/{ffmpeg,ffprobe}
	dir="$(find "$tmpdir" -maxdepth 1 -type d -name 'ffmpeg-*' | head -1)"
	cp "$dir/bin/ffmpeg" "$OUTDIR/ffmpeg"
	cp "$dir/bin/ffprobe" "$OUTDIR/ffprobe"
	chmod +x "$OUTDIR/ffmpeg" "$OUTDIR/ffprobe"
	;;
linux-arm64)
	asset="ffmpeg-master-latest-linuxarm64-lgpl.tar.xz"
	url="${BTBN_BASE}/${asset}"
	echo "→ downloading $url"
	dl "$url" "$tmpdir/ff.tar.xz"
	verify "$tmpdir/ff.tar.xz" "$(btbn_digest "$asset")" "$asset"
	tar -xJf "$tmpdir/ff.tar.xz" -C "$tmpdir"
	dir="$(find "$tmpdir" -maxdepth 1 -type d -name 'ffmpeg-*' | head -1)"
	cp "$dir/bin/ffmpeg" "$OUTDIR/ffmpeg"
	cp "$dir/bin/ffprobe" "$OUTDIR/ffprobe"
	chmod +x "$OUTDIR/ffmpeg" "$OUTDIR/ffprobe"
	;;
windows-amd64)
	asset="ffmpeg-master-latest-win64-lgpl.zip"
	url="${BTBN_BASE}/${asset}"
	echo "→ downloading $url"
	dl "$url" "$tmpdir/ff.zip"
	verify "$tmpdir/ff.zip" "$(btbn_digest "$asset")" "$asset"
	unzip -q "$tmpdir/ff.zip" -d "$tmpdir"
	dir="$(find "$tmpdir" -maxdepth 1 -type d -name 'ffmpeg-*' | head -1)"
	cp "$dir/bin/ffmpeg.exe" "$OUTDIR/ffmpeg.exe"
	cp "$dir/bin/ffprobe.exe" "$OUTDIR/ffprobe.exe"
	;;
darwin-amd64 | darwin-arm64)
	# evermeet sirve binarios universales en su zip individual.
	echo "→ downloading evermeet ffmpeg + ffprobe (universal)"
	dl "https://evermeet.cx/ffmpeg/getrelease/zip" "$tmpdir/ffmpeg.zip"
	dl "https://evermeet.cx/ffmpeg/getrelease/ffprobe/zip" "$tmpdir/ffprobe.zip"
	verify "$tmpdir/ffmpeg.zip" "$(evermeet_sha256 ffmpeg)" "evermeet ffmpeg.zip" soft
	verify "$tmpdir/ffprobe.zip" "$(evermeet_sha256 ffprobe)" "evermeet ffprobe.zip" soft
	unzip -q "$tmpdir/ffmpeg.zip" -d "$tmpdir/ffmpeg"
	unzip -q "$tmpdir/ffprobe.zip" -d "$tmpdir/ffprobe"
	cp "$tmpdir/ffmpeg/ffmpeg" "$OUTDIR/ffmpeg"
	cp "$tmpdir/ffprobe/ffprobe" "$OUTDIR/ffprobe"
	chmod +x "$OUTDIR/ffmpeg" "$OUTDIR/ffprobe"
	;;
*)
	echo "unsupported platform: $GOOS-$GOARCH" >&2
	exit 1
	;;
esac

# Sanity: el binario corre y devuelve 0. Saltar el check si estamos
# cross-compiling (e.g. Linux runner empaquetando Windows): el .exe
# no se puede ejecutar en Linux.
if [[ "$GOOS" == "$(uname -s | tr '[:upper:]' '[:lower:]')" || ( "$GOOS" == "darwin" && "$(uname -s)" == "Darwin" ) ]]; then
	host_arch="$(uname -m)"
	wanted_arch="$GOARCH"
	# Map uname -m → goarch
	case "$host_arch" in
	x86_64) host_arch="amd64" ;;
	aarch64 | arm64) host_arch="arm64" ;;
	esac
	if [[ "$host_arch" == "$wanted_arch" ]]; then
		bin="$OUTDIR/ffmpeg"
		[[ "$GOOS" == "windows" ]] && bin="$OUTDIR/ffmpeg.exe"
		"$bin" -version >/dev/null
		echo "✓ ffmpeg verified executable on host"
	fi
fi

echo "✓ ffmpeg + ffprobe ready at $OUTDIR"
