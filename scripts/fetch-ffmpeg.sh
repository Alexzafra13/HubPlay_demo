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
# El script verifica que los binarios resultantes son ejecutables y
# que `ffmpeg -version` devuelve 0 — no queremos shippear un archivo
# truncado por descarga interrumpida.

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

# dl: descarga con reintentos + backoff exponencial. Los assets viven en
# el CDN de releases de GitHub (BtbN) y evermeet.cx, que devuelven 5xx
# transitorios (504 gateway timeout, 503) bajo carga sin que la red local
# falle. `curl --retry` ya cubre los códigos transitorios (408/429/5xx) y
# los timeouts; `--retry-delay 2` arranca el backoff y curl lo escala
# solo. `--retry-connrefused` cubre el caso de un edge node reiniciando.
# Sin esto, un único 504 aborta todo el job de release.
dl() {
	local url="$1" out="$2"
	curl -fSL --no-progress-meter --retry 5 --retry-delay 2 \
		--retry-connrefused --connect-timeout 30 "$url" -o "$out"
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
	url="${BTBN_BASE}/ffmpeg-master-latest-linux64-lgpl.tar.xz"
	echo "→ downloading $url"
	dl "$url" "$tmpdir/ff.tar.xz"
	tar -xJf "$tmpdir/ff.tar.xz" -C "$tmpdir"
	# Estructura: ffmpeg-N-...-linux64-lgpl/bin/{ffmpeg,ffprobe}
	dir="$(find "$tmpdir" -maxdepth 1 -type d -name 'ffmpeg-*' | head -1)"
	cp "$dir/bin/ffmpeg" "$OUTDIR/ffmpeg"
	cp "$dir/bin/ffprobe" "$OUTDIR/ffprobe"
	chmod +x "$OUTDIR/ffmpeg" "$OUTDIR/ffprobe"
	;;
linux-arm64)
	url="${BTBN_BASE}/ffmpeg-master-latest-linuxarm64-lgpl.tar.xz"
	echo "→ downloading $url"
	dl "$url" "$tmpdir/ff.tar.xz"
	tar -xJf "$tmpdir/ff.tar.xz" -C "$tmpdir"
	dir="$(find "$tmpdir" -maxdepth 1 -type d -name 'ffmpeg-*' | head -1)"
	cp "$dir/bin/ffmpeg" "$OUTDIR/ffmpeg"
	cp "$dir/bin/ffprobe" "$OUTDIR/ffprobe"
	chmod +x "$OUTDIR/ffmpeg" "$OUTDIR/ffprobe"
	;;
windows-amd64)
	url="${BTBN_BASE}/ffmpeg-master-latest-win64-lgpl.zip"
	echo "→ downloading $url"
	dl "$url" "$tmpdir/ff.zip"
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
