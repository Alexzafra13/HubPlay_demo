# Multi-platform Deployment — Design Document

## Overview

HubPlay es un binario Go puro (CGO_ENABLED=0) con el frontend React
embebido y SQLite pure-Go integrado.  El binario compila y arranca
en **Linux, macOS y Windows** con cero dependencias dinámicas
salvo `ffmpeg` y `ffprobe`, que son binarios externos que el
operador instala en el host.

Recomendación: **Docker (Linux) sigue siendo el camino default**.
Esta doc cubre los casos en que el operador prefiere binario nativo
en Mac o Windows — uso doméstico mono-host, principalmente.

---

## 1. Compilación cruzada

```bash
# Linux (mismo host de build)
go build -trimpath -o hubplay ./cmd/hubplay

# Windows AMD64
GOOS=windows GOARCH=amd64 go build -trimpath -o hubplay.exe ./cmd/hubplay

# macOS Apple Silicon
GOOS=darwin GOARCH=arm64 go build -trimpath -o hubplay-mac-arm ./cmd/hubplay

# macOS Intel
GOOS=darwin GOARCH=amd64 go build -trimpath -o hubplay-mac-intel ./cmd/hubplay
```

`-trimpath` quita los paths absolutos del filesystem del operador
del binario — útil si compartes el binario sin querer publicar
"/home/usuario/build/...".

El binario es **estático**: no necesita libsqlite3, libssl, ni
ninguna otra DLL/SO. Lleva todo dentro.

---

## 2. Dependencias externas: ffmpeg + ffprobe

Las dos únicas dependencias que NO se pueden embeber en Go
(licencia LGPL/GPL + tamaño 200 MB+ cada una).  El binario las
ejecuta como sub-procesos via `os/exec`.

### Linux

```bash
sudo apt install ffmpeg                    # Debian/Ubuntu
sudo dnf install ffmpeg-free               # Fedora (free codecs)
# o FFmpeg static builds: https://johnvansickle.com/ffmpeg/
```

### macOS

```bash
brew install ffmpeg
```

### Windows

1. Descarga el "essentials" build de
   [gyan.dev](https://www.gyan.dev/ffmpeg/builds/).
2. Descomprime en `C:\ffmpeg\`.
3. Añade `C:\ffmpeg\bin` al PATH del sistema (Panel de Control →
   System → Environment Variables).
4. Verifica:

   ```powershell
   ffmpeg -version
   ffprobe -version
   ```

Si NO quieres tocar el PATH, configura las rutas absolutas en
`hubplay.yaml`:

```yaml
# en hubplay.yaml
ffmpeg_path: "C:/ffmpeg/bin/ffmpeg.exe"
ffprobe_path: "C:/ffmpeg/bin/ffprobe.exe"
```

### Sin ffmpeg

El servidor arranca, sirve la UI, gestiona usuarios y librerías,
pero:

- Los uploads de vídeo fallan en la fase `probing` con
  `ffprobe: exec: "ffprobe": executable file not found`.
- El transcoding queda deshabilitado (direct-play sigue
  funcionando para clientes que aceptan el codec nativo).
- IPTV transmux falla.

Los subtítulos sí se pueden subir aunque no haya ffmpeg — la
pipeline salta `probing` para `KindSubtitle`.

---

## 3. Caveats por plataforma

### Windows

**Aceleración por hardware**:
- NVIDIA → NVENC funciona con el ffmpeg de gyan.dev.
- Intel iGPU → QSV funciona; necesitas el ffmpeg con `--enable-libmfx`.
- AMD → AMF (NO VA-API en Windows). El proyecto soporta NVENC y QSV
  hoy; AMF requeriría una extensión menor del detector en
  `internal/stream/hwaccel.go`.

**Permisos de filesystem**:
- El binario se ejecuta normalmente como usuario interactivo, no
  como Service.  Si quieres autorestart, usa
  [NSSM](https://nssm.cc/) para registrarlo como servicio.

**Antivirus**:
- Windows Defender puede marcar binarios Go sin firma como
  sospechosos al primer arranque.  Excluir la carpeta de instalación
  o firmar el binario con un certificado de code-signing.

**Paths**:
- Usa siempre rutas con barra `/` o doble backslash `\\` en el YAML.
  Backslash simple Go lo interpreta como escape.

### macOS

**Aceleración por hardware**:
- VideoToolbox funciona out-of-the-box con `brew install ffmpeg`.

**Gatekeeper**:
- El binario sin firma muestra "no se puede abrir porque proviene
  de un desarrollador no identificado".  Soluciones:
  1. Click derecho → Abrir → Confirma una vez (sólo la primera).
  2. `xattr -d com.apple.quarantine hubplay` para quitar el flag.
  3. Firmar con tu Apple Developer Certificate (`codesign`).

### Linux nativo (sin Docker)

Es el camino más probado (lo que la imagen Docker hace internamente).
Sin sorpresas.  Recomendado registrar como systemd unit:

```ini
# /etc/systemd/system/hubplay.service
[Unit]
Description=HubPlay
After=network.target

[Service]
ExecStart=/opt/hubplay/hubplay --config /etc/hubplay/hubplay.yaml
Restart=on-failure
User=hubplay
Group=hubplay

[Install]
WantedBy=multi-user.target
```

---

## 4. Almacenamiento de datos

El binario por defecto pone DB y staging junto al `--config`:

```
<config dir>/
├── hubplay.yaml
├── hubplay.db              # SQLite si driver=sqlite
├── uploads/staging/        # blobs en vuelo (PR2 uploads)
└── avatars/                # avatares subidos por usuarios
```

**Recomendación**: en Windows usa `%APPDATA%\hubplay\` o
`C:\ProgramData\hubplay\`; en macOS `~/Library/Application
Support/hubplay/`.  En Linux nativo `~/.config/hubplay/` o
`/var/lib/hubplay/`.

---

## 5. CORS para deploys cross-origin

Si despliegas el frontend en otro host (ej. SPA en
`https://app.example.com`, API en `https://api.example.com`):

1. Edita `hubplay.yaml`:

   ```yaml
   server:
     base_url: "https://api.example.com"
   ```

   El `base_url` queda como origen estático permitido.

2. Para añadir orígenes EXTRA sin restart (ej. una preview de
   Cloudflare Pages, otro dominio interno), entra como owner a
   **`/admin/system`** → sección **"Orígenes CORS permitidos"** →
   Añadir.

3. **tus uploads** funcionan cross-origin sin modificación adicional
   porque CORS ya incluye `PATCH` + headers `Tus-*` (PR4).

---

## 6. Multi-arch en Docker

El `Dockerfile` actual es amd64-first (incluye `hwaccel` target con
VAAPI/NVENC).  Para ARM (Raspberry Pi 5, etc.) la build es:

```dockerfile
FROM --platform=$BUILDPLATFORM golang:1.25 AS build
ARG TARGETOS TARGETARCH
ENV CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH
# ... resto igual
```

Y `docker buildx`:

```bash
docker buildx build --platform linux/amd64,linux/arm64 \
  -t hubplay:latest --push .
```

En ARM no hay VAAPI ni NVENC.  V4L2 M2M (Pi Hardware Codec) tiene
soporte experimental — out of scope hasta que llegue la demanda.
