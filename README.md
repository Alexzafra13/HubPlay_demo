# HubPlay

Servidor de media self-hosted estilo Plex / Jellyfin: películas, series,
música, TV en vivo (IPTV M3U + EPG), transcodificación HLS con FFmpeg,
usuarios, federación entre servidores y panel de administración. Un solo
binario Go con la interfaz React embebida.

Se puede instalar **con Docker** o **sin Docker** (binario nativo); ambas
rutas son de primera clase y salen de la misma release.

## Instalación con Docker (recomendada)

Requisitos: Docker + Docker Compose. Incluye FFmpeg y un Postgres opcional.

```bash
git clone https://github.com/Alexzafra13/HubPlay_demo.git
cd HubPlay_demo
# Opcional: copia .env.example a .env y ajusta HUBPLAY_MEDIA_MOVIES /
# HUBPLAY_MEDIA_SERIES a tus carpetas (por defecto ./peliculas y ./series).
docker compose up -d
```

Abre `http://localhost:8097` y completa el asistente de primer arranque
(cuenta admin, bibliotecas, clave de TMDb, transcodificación).

- Config y base de datos SQLite: `./config`. Caché de transcodes: `./cache`.
- Postgres: el compose levanta uno; el panel admin ofrece el cambio SQLite →
  Postgres con un clic (ver `docs/operations/postgres.md`).
- Aceleración por hardware (VAAPI / NVENC, amd64): target `hwaccel` del
  `Dockerfile` y `docs/architecture/deployment-production.md`.
- Stack con descargas torrent (Prowlarr + FlareSolverr):
  `docker compose -f docker-compose.torrent.yml up -d`.
- Producción detrás de nginx con TLS: `deploy/docker-compose.prod.yml` +
  `deploy/setup-server.sh`.

## Instalación sin Docker (binario nativo)

Cada release publica binarios para Linux (amd64/arm64), Windows (amd64) y
macOS (amd64/arm64) con `ffmpeg` y `ffprobe` empaquetados:
<https://github.com/Alexzafra13/HubPlay_demo/releases>.

### Windows

Descarga `HubPlay-Setup-<versión>-windows-amd64.exe` y ejecútalo. Instala
en `C:\Program Files\HubPlay`, registra el servicio `hubplay` (NSSM) y
escucha en `http://localhost:8096`. La config está en
`C:\Program Files\HubPlay\hubplay.yaml`.

### Linux (systemd)

```bash
curl -fsSL https://github.com/Alexzafra13/HubPlay_demo/releases/latest/download/install.sh | sudo bash
```

Instala el binario en `/usr/local/bin`, la config en `/etc/hubplay` y los
datos en `/var/lib/hubplay`, y activa la unidad `hubplay.service`.

### Cualquier sistema, a mano

```bash
tar -xzf hubplay-<versión>-<os>-<arch>.tar.gz   # o unzip en Windows
cp hubplay.example.yaml hubplay.yaml            # ajusta puerto, rutas, etc.
./hubplay --config hubplay.yaml
```

FFmpeg/FFprobe deben estar en el `PATH` (van en el paquete). La base de
datos SQLite se crea junto a la config; no hace falta nada más.

## Desarrollo

Requisitos: Go 1.25, Node 22 + pnpm 10, FFmpeg.

```bash
cd web && pnpm install && pnpm build && cd ..   # SPA (se embebe en el binario)
go build -o bin/hubplay ./cmd/hubplay
./bin/hubplay --config hubplay.example.yaml      # http://localhost:8096
```

- `make dev` (Go con recarga) y `make web-dev` (Vite en :3000).
- Tests: `go test ./...` y `cd web && pnpm test`. Lint: `make lint`,
  `pnpm lint`, `pnpm knip`.
- Entorno de desarrollo con Docker (Postgres incluido):
  `docker compose -f docker-compose.dev.yml up --build` (puerto 8097).
- Notas para Windows (Go sin admin, pnpm sin TTY, rutas):
  `docs/memory/conventions.md` § "Desarrollo en Windows".

## Documentación

- `docs/architecture/` — diseño del sistema (30+ documentos).
- `docs/api/` y `internal/api/handlers/system/openapi.yaml` — contrato de la
  API REST (la usa la app de TV).
- `docs/memory/` — estado del proyecto, decisiones y auditorías vivas.
- `CLAUDE.md` — resumen del stack y convenciones.

## Licencia

Ver `LICENSE`.
