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

# Versión del build de FFmpeg-Builds a usar. release/N7 es la línea
# estable; releases concretos en https://github.com/BtbN/FFmpeg-Builds/releases.
# "latest" es un alias que GitHub redirige automáticamente — aceptable
# para CI porque las releases son inmutables una vez publicadas.
BTBN_RELEASE="latest"

case "$GOOS-$GOARCH" in
linux-amd64)
	url="https://github.com/BtbN/FFmpeg-Builds/releases/${BTBN_RELEASE}/download/ffmpeg-master-latest-linux64-lgpl.tar.xz"
	echo "→ downloading $url"
	curl -fsSL "$url" -o "$tmpdir/ff.tar.xz"
	tar -xJf "$tmpdir/ff.tar.xz" -C "$tmpdir"
	# Estructura: ffmpeg-N-...-linux64-lgpl/bin/{ffmpeg,ffprobe}
	dir="$(find "$tmpdir" -maxdepth 1 -type d -name 'ffmpeg-*' | head -1)"
	cp "$dir/bin/ffmpeg" "$OUTDIR/ffmpeg"
	cp "$dir/bin/ffprobe" "$OUTDIR/ffprobe"
	chmod +x "$OUTDIR/ffmpeg" "$OUTDIR/ffprobe"
	;;
linux-arm64)
	url="https://github.com/BtbN/FFmpeg-Builds/releases/${BTBN_RELEASE}/download/ffmpeg-master-latest-linuxarm64-lgpl.tar.xz"
	echo "→ downloading $url"
	curl -fsSL "$url" -o "$tmpdir/ff.tar.xz"
	tar -xJf "$tmpdir/ff.tar.xz" -C "$tmpdir"
	dir="$(find "$tmpdir" -maxdepth 1 -type d -name 'ffmpeg-*' | head -1)"
	cp "$dir/bin/ffmpeg" "$OUTDIR/ffmpeg"
	cp "$dir/bin/ffprobe" "$OUTDIR/ffprobe"
	chmod +x "$OUTDIR/ffmpeg" "$OUTDIR/ffprobe"
	;;
windows-amd64)
	url="https://github.com/BtbN/FFmpeg-Builds/releases/${BTBN_RELEASE}/download/ffmpeg-master-latest-win64-lgpl.zip"
	echo "→ downloading $url"
	curl -fsSL "$url" -o "$tmpdir/ff.zip"
	unzip -q "$tmpdir/ff.zip" -d "$tmpdir"
	dir="$(find "$tmpdir" -maxdepth 1 -type d -name 'ffmpeg-*' | head -1)"
	cp "$dir/bin/ffmpeg.exe" "$OUTDIR/ffmpeg.exe"
	cp "$dir/bin/ffprobe.exe" "$OUTDIR/ffprobe.exe"
	;;
darwin-amd64 | darwin-arm64)
	# evermeet sirve binarios universales en su zip individual.
	echo "→ downloading evermeet ffmpeg + ffprobe (universal)"
	curl -fsSL "https://evermeet.cx/ffmpeg/getrelease/zip" -o "$tmpdir/ffmpeg.zip"
	curl -fsSL "https://evermeet.cx/ffmpeg/getrelease/ffprobe/zip" -o "$tmpdir/ffprobe.zip"
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
