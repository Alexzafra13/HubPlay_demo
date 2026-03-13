# Setup Wizard (Primera Instalación) — Design Document

## Overview

Cuando HubPlay arranca por primera vez (DB vacía, sin usuarios), muestra un wizard de configuración inicial. El wizard crea el admin, configura las libraries, y deja el servidor listo para usar.

**Inspirado en Jellyfin**, simplificado para HubPlay.

---

## 1. Detección de Primera Ejecución

```go
// internal/setup/setup.go
func NeedsSetup(db *sql.DB) (bool, error) {
    // Si no hay usuarios en la DB → es primera ejecución
    count, err := queries.CountUsers(context.Background())
    if err != nil {
        return false, err
    }
    return count == 0, nil
}
```

**Flag adicional en config** (por seguridad):

```go
// internal/config/config.go
type Config struct {
    // ...
    IsSetupCompleted bool `yaml:"setup_completed" json:"setup_completed"`
}
```

Doble check: `setup_completed == false` **Y** `count(users) == 0`. Esto previene que alguien borre la DB y vuelva a acceder al wizard.

### Middleware de protección

```go
// internal/api/middleware.go
func SetupGuard(needsSetup bool) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Si necesita setup → solo permitir /api/v1/setup/* y assets estáticos
            if needsSetup && !isSetupRoute(r.URL.Path) && !isStaticAsset(r.URL.Path) {
                // Redirigir al wizard
                if isAPIRoute(r.URL.Path) {
                    respondError(w, http.StatusForbidden, "SETUP_REQUIRED", "server needs initial setup")
                    return
                }
                http.Redirect(w, r, "/setup", http.StatusTemporaryRedirect)
                return
            }

            // Si setup ya completado → bloquear /api/v1/setup/*
            if !needsSetup && isSetupRoute(r.URL.Path) {
                respondError(w, http.StatusForbidden, "SETUP_COMPLETED", "setup already completed")
                return
            }

            next.ServeHTTP(w, r)
        })
    }
}
```

---

## 2. Pasos del Wizard

```
┌─────────────────────────────────────────────────────┐
│  ┌───┐  ┌───┐  ┌───┐  ┌───┐  ┌───┐  ┌───┐         │
│  │ 1 │──│ 2 │──│ 3 │──│ 4 │──│ 5 │──│ 6 │         │
│  └───┘  └───┘  └───┘  └───┘  └───┘  └───┘         │
│  Lang   Admin  Libraries Remote Settings  Done!    │
└─────────────────────────────────────────────────────┘
```

### Paso 1: Idioma y Región

**Qué se configura:**
- Idioma de la interfaz (UI language)
- País para metadatos (TMDb busca en ese idioma/región)

```
┌─────────────────────────────────────────┐
│         Welcome to HubPlay              │
│                                         │
│  Display Language:   [English ▼]        │
│                                         │
│  Metadata Country:   [Spain ▼]          │
│  Metadata Language:  [Español ▼]        │
│                                         │
│  (This determines how movie titles,     │
│   descriptions and images are fetched)  │
│                                         │
│                          [Next →]       │
└─────────────────────────────────────────┘
```

**API:**
```
POST /api/v1/setup/language
{
    "ui_language": "es",
    "metadata_country": "ES",
    "metadata_language": "es"
}
```

### Paso 2: Crear Cuenta Admin

**Qué se configura:**
- Username del administrador
- Contraseña
- (Opcional) Display name

```
┌─────────────────────────────────────────┐
│       Create Admin Account              │
│                                         │
│  Username:      [admin          ]       │
│  Password:      [••••••••       ]       │
│  Confirm:       [••••••••       ]       │
│                                         │
│  Display Name:  [Alex           ]       │
│  (optional)                             │
│                                         │
│  ⚠ This is the main administrator.     │
│    You can create more users later.     │
│                                         │
│                  [← Back]  [Next →]     │
└─────────────────────────────────────────┘
```

**Validación:**
- Username: 3-50 chars, alfanumérico + guiones
- Password: mínimo 8 chars
- Confirm: debe coincidir

**API:**
```
POST /api/v1/setup/user
{
    "username": "admin",
    "password": "supersecret123",
    "display_name": "Alex"
}
→ 200 { "user_id": "uuid", "access_token": "jwt..." }
```

A partir de aquí el wizard usa el JWT del admin para las siguientes llamadas.

### Paso 3: Libraries (Media Folders)

**Qué se configura:**
- Nombre de cada library
- Tipo de contenido (Movies / TV Shows)
- Rutas del filesystem donde están los archivos

```
┌─────────────────────────────────────────────────────┐
│         Add Your Media Libraries                     │
│                                                      │
│  ┌─────────────────────────────────────────────────┐ │
│  │ 🎬  Movies                                      │ │
│  │  Content Type:  [Movies ▼]                      │ │
│  │  Folder:        [/media/movies          ] [📁]  │ │
│  │                 [+ Add another folder]           │ │
│  └─────────────────────────────────────────────────┘ │
│                                                      │
│  ┌─────────────────────────────────────────────────┐ │
│  │ 📺  TV Shows                                    │ │
│  │  Content Type:  [TV Shows ▼]                    │ │
│  │  Folder:        [/media/tv              ] [📁]  │ │
│  └─────────────────────────────────────────────────┘ │
│                                                      │
│  [+ Add Library]                                     │
│                                                      │
│  ⓘ You can add more libraries later in Settings.    │
│  ⓘ You can skip this step and add them later.       │
│                                                      │
│                       [← Back]  [Skip]  [Next →]    │
└─────────────────────────────────────────────────────┘
```

**Explorador de carpetas (server-side):**
El botón 📁 abre un file browser que lista directorios del servidor:

```
POST /api/v1/setup/browse
{ "path": "/media" }
→ {
    "current": "/media",
    "parent": "/",
    "directories": [
        { "name": "movies", "path": "/media/movies" },
        { "name": "tv", "path": "/media/tv" },
        { "name": "music", "path": "/media/music" }
    ]
}
```

**Validación:**
- La ruta debe existir en el servidor
- La ruta debe ser accesible (permisos de lectura)
- Advertir si la ruta está vacía (pero permitirlo)

**API:**
```
POST /api/v1/setup/libraries
{
    "libraries": [
        {
            "name": "Movies",
            "content_type": "movies",
            "paths": ["/media/movies"]
        },
        {
            "name": "TV Shows",
            "content_type": "tvshows",
            "paths": ["/media/tv"]
        }
    ]
}
```

### Paso 4: Acceso Remoto

**Qué se configura:**
- Permitir conexiones remotas (fuera de LAN)
- Puerto del servidor
- UPnP auto-mapping (opcional)

```
┌─────────────────────────────────────────┐
│         Remote Access                    │
│                                         │
│  [✓] Allow remote connections           │
│                                         │
│  Server Port:  [8096]                   │
│                                         │
│  [ ] Enable automatic port mapping      │
│      (UPnP)                             │
│                                         │
│  ⓘ If you're behind a reverse proxy    │
│    (Nginx, Caddy), leave UPnP off       │
│    and configure it there.              │
│                                         │
│                  [← Back]  [Next →]     │
└─────────────────────────────────────────┘
```

**API:**
```
POST /api/v1/setup/remote-access
{
    "allow_remote": true,
    "port": 8096,
    "enable_upnp": false
}
```

### Paso 5: Ajustes Adicionales (Opcional)

**Qué se configura:**
- API Key de TMDb (opcional — sin ella no habrá metadata automática)
- Transcodificación: habilitar/deshabilitar
- Hardware acceleration detectado

```
┌─────────────────────────────────────────────────────┐
│         Additional Settings                          │
│                                                      │
│  ── Metadata ──                                      │
│  TMDb API Key:   [________________________] [?]      │
│  (Free at themoviedb.org — required for              │
│   automatic movie/show metadata)                     │
│                                                      │
│  ── Transcoding ──                                   │
│  FFmpeg:  ✅ Found at /usr/bin/ffmpeg                │
│                                                      │
│  [✓] Enable transcoding                             │
│                                                      │
│  Hardware Acceleration:                              │
│    ✅ VAAPI detected (Intel GPU)                     │
│    ( ) NVENC (not available)                          │
│    (•) VAAPI (recommended)                           │
│    ( ) Software only                                 │
│                                                      │
│                       [← Back]  [Skip]  [Next →]    │
└─────────────────────────────────────────────────────┘
```

**Detección automática:**
- FFmpeg path se detecta al arrancar
- Hardware acceleration se detecta con `ffmpeg -hwaccels`
- Si no hay FFmpeg → mostrar warning y desactivar transcoding

**API:**
```
POST /api/v1/setup/settings
{
    "tmdb_api_key": "abc123...",
    "transcoding_enabled": true,
    "hw_accel": "vaapi"
}
```

### Paso 6: Resumen y Finalización

```
┌─────────────────────────────────────────────────────┐
│         Setup Complete!                              │
│                                                      │
│  ✅ Admin account created (admin)                   │
│  ✅ 2 libraries configured                          │
│     • Movies → /media/movies                        │
│     • TV Shows → /media/tv                          │
│  ✅ Remote access enabled (port 8096)               │
│  ✅ TMDb metadata configured                        │
│  ✅ Transcoding enabled (VAAPI)                     │
│                                                      │
│  ┌───────────────────────────────────────────────┐   │
│  │ [✓] Start scanning libraries now              │   │
│  │     (This may take a while depending on       │   │
│  │      your media collection size)              │   │
│  └───────────────────────────────────────────────┘   │
│                                                      │
│                          [← Back]  [Finish →]       │
└─────────────────────────────────────────────────────┘
```

**API:**
```
POST /api/v1/setup/complete
{ "start_scan": true }
→ 200 { "ok": true }
```

Al llamar `/complete`:
1. Se marca `setup_completed = true` en config
2. Se persiste la config al disco
3. Si `start_scan: true` → se lanza scan de todas las libraries en background
4. El middleware deja de redirigir al wizard
5. Redirige al dashboard principal

---

## 3. API del Setup Wizard

```
POST /api/v1/setup/language        ← Paso 1
POST /api/v1/setup/user            ← Paso 2 (devuelve JWT)
POST /api/v1/setup/browse          ← File browser (usado en paso 3)
POST /api/v1/setup/libraries       ← Paso 3
POST /api/v1/setup/remote-access   ← Paso 4
POST /api/v1/setup/settings        ← Paso 5
POST /api/v1/setup/complete        ← Paso 6 (finaliza wizard)

GET  /api/v1/setup/status          ← ¿El wizard ya se completó?
GET  /api/v1/setup/ffmpeg-detect   ← Detectar FFmpeg y HW accel
```

**Seguridad:**
- Todos los endpoints de `/setup/*` solo funcionan si `setup_completed == false`
- Después de crear el admin (paso 2), los pasos 3-6 requieren el JWT
- Una vez completado, los endpoints devuelven `403 SETUP_COMPLETED`

---

## 4. Frontend — React Router

```tsx
// web/src/pages/setup/SetupWizard.tsx
const STEPS = [
    { path: "language",      component: LanguageStep },
    { path: "account",       component: AccountStep },
    { path: "libraries",     component: LibrariesStep },
    { path: "remote-access", component: RemoteAccessStep },
    { path: "settings",      component: SettingsStep },
    { path: "complete",      component: CompleteStep },
];

function SetupWizard() {
    const [currentStep, setCurrentStep] = useState(0);

    // Stepper visual arriba
    // Botones Back/Next abajo
    // Cada step maneja su propio state y API call
    // Al completar un step → setCurrentStep(prev + 1)
}
```

**Rutas:**
```
/setup              → Redirige a /setup/language
/setup/language     → Paso 1
/setup/account      → Paso 2
/setup/libraries    → Paso 3
/setup/remote       → Paso 4
/setup/settings     → Paso 5
/setup/complete     → Paso 6
```

---

## 5. File Browser Component

Para seleccionar carpetas del servidor (paso 3):

```tsx
// web/src/components/setup/FolderBrowser.tsx
function FolderBrowser({ onSelect }: { onSelect: (path: string) => void }) {
    const [currentPath, setCurrentPath] = useState("/");
    const [directories, setDirectories] = useState<Directory[]>([]);

    // Navegar: click en carpeta → POST /api/v1/setup/browse { path }
    // Seleccionar: click en "Select this folder" → onSelect(currentPath)
    // Subir: click en ".." → navegar al parent

    return (
        <div className="folder-browser">
            <div className="current-path">{currentPath}</div>
            <div className="directory-list">
                {currentPath !== "/" && (
                    <div onClick={() => navigate(parent)}>📁 ..</div>
                )}
                {directories.map(dir => (
                    <div key={dir.path} onClick={() => navigate(dir.path)}>
                        📁 {dir.name}
                    </div>
                ))}
            </div>
            <button onClick={() => onSelect(currentPath)}>
                Select this folder
            </button>
        </div>
    );
}
```

**Seguridad del file browser:**
- Solo lista directorios, nunca archivos
- Nunca expone contenido de archivos
- Filtra paths peligrosos (`/etc`, `/root`, etc.) opcionalmente
- Solo disponible durante setup o para admins autenticados

---

## 6. Docker: Carpetas Mapeadas

En Docker, el usuario mapea sus carpetas de media al contenedor:

```yaml
# docker-compose.yml
services:
  hubplay:
    image: hubplay/hubplay:latest
    ports:
      - "8096:8096"
    volumes:
      - ./config:/config              # Config + DB persistente
      - /mnt/media/movies:/media/movies  # Películas
      - /mnt/media/tv:/media/tv          # Series
      - /mnt/media/music:/media/music    # Música (futuro)
    environment:
      - PUID=1000
      - PGID=1000
```

En el wizard, el file browser muestra las carpetas **dentro del contenedor** (`/media/movies`, `/media/tv`), que son los volúmenes mapeados.

---

## 7. Comparación con Jellyfin

| Paso | Jellyfin | HubPlay | Diferencias |
|------|---------|---------|-------------|
| 1 | Language | Language + Metadata region | Combinamos UI lang y metadata lang |
| 2 | Admin account | Admin account | Igual |
| 3 | Media libraries | Media libraries | Igual, con file browser |
| 4 | Metadata settings | Moved to step 5 | TMDb key con settings |
| 5 | Remote access | Remote access | Igual |
| 6 | — | Settings (FFmpeg, HW accel) | Jellyfin lo detecta automático, nosotros mostramos qué se detectó |
| 7 | Done | Done + start scan | Añadimos opción de escanear inmediatamente |

**Simplificaciones vs Jellyfin:**
- Jellyfin tiene opciones de metadata plugin por library — nosotros usamos TMDb siempre
- Jellyfin permite configurar subtítulos en setup — nosotros lo dejamos para después
- Nuestro wizard es más limpio y con menos opciones (progressive disclosure)
