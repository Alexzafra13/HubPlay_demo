# Testing Strategy — Design Document

## Overview

Testing pyramid: muchos unit tests rápidos en la base, integration tests en el medio, pocos E2E tests lentos arriba. Cada capa tiene herramientas y patrones específicos.

```
        ╱╲
       ╱E2E╲          Playwright (frontend) + httptest full server (backend)
      ╱──────╲         Pocos, lentos, validan flujos completos
     ╱Integration╲     SQLite in-memory, httptest handlers, MSW
    ╱──────────────╲   Medianos, validan capas conectadas
   ╱   Unit Tests    ╲  go test, Vitest — lógica pura, sin I/O
  ╱════════════════════╲ Muchos, rápidos, table-driven
```

---

## 1. Backend (Go)

### 1.1 Unit Tests

Tests de lógica pura sin dependencias externas.

| Módulo | Qué se testea | Patrón |
|--------|--------------|--------|
| `resolver/movie.go` | Parsing de `Title (Year).ext` | Table-driven: muchos inputs → expected output |
| `resolver/tv.go` | Parsing de `SxxExx` patterns | Table-driven con edge cases (multi-episode, specials) |
| `streaming/decision.go` | Playback decision waterfall | Combinaciones de codecs × profiles |
| `ffmpeg/builder.go` | Comandos FFmpeg generados | Verificar args del command, no ejecutar |
| `iptv/m3u.go` | Parser M3U | Fixtures con playlists reales + malformadas |
| `iptv/epg.go` | Parser XMLTV | Fixtures XML |
| `auth/jwt.go` | Token creation + validation | Claims, expiry, signing |
| `federation/crypto.go` | Ed25519 sign + verify | Key pairs, JWT federation tokens |
| `webhook/template.go` | Template rendering | Variables de eventos |
| `event/bus.go` | Pub/sub, concurrencia | Goroutines, channels, race detector |

```go
// Ejemplo: resolver/movie_test.go — table-driven
func TestMovieResolver(t *testing.T) {
    tests := []struct {
        name     string
        path     string
        wantTitle string
        wantYear  int
        wantErr   bool
    }{
        {
            name:      "standard format",
            path:      "/movies/Inception (2010)/Inception (2010).mkv",
            wantTitle: "Inception",
            wantYear:  2010,
        },
        {
            name:      "no year",
            path:      "/movies/Inception/Inception.mkv",
            wantTitle: "Inception",
            wantYear:  0,
        },
        {
            name:      "with edition tag",
            path:      "/movies/Blade Runner (1982)/Blade Runner (1982) - Director's Cut.mkv",
            wantTitle: "Blade Runner",
            wantYear:  1982,
        },
        {
            name:    "empty path",
            path:    "",
            wantErr: true,
        },
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            r := NewMovieResolver()
            got, err := r.Resolve(context.Background(), tt.path)
            if tt.wantErr {
                require.Error(t, err)
                return
            }
            require.NoError(t, err)
            assert.Equal(t, tt.wantTitle, got.Title)
            assert.Equal(t, tt.wantYear, got.Year)
        })
    }
}
```

```go
// Ejemplo: ffmpeg/builder_test.go — verificar comando generado
func TestBuildHLS_Transcode1080p(t *testing.T) {
    b := NewFFmpegBuilder("/usr/bin/ffmpeg")
    decision := &PlaybackDecision{
        Method:     Transcode,
        VideoCodec: "libx264",
        AudioCodec: "aac",
        Resolution: Resolution{Width: 1920, Height: 1080},
    }

    cmd := b.BuildHLS("/media/movie.mkv", decision, 0)

    args := cmd.Args
    assert.Contains(t, args, "-c:v")
    assert.Contains(t, args, "libx264")
    assert.Contains(t, args, "-vf")
    assert.Contains(t, args, "scale=1920:1080")
    assert.Contains(t, args, "-f")
    assert.Contains(t, args, "hls")
}
```

### 1.2 Integration Tests — Repository Layer

Queries SQL reales contra SQLite in-memory. **No mocks de DB** — los queries deben ejecutarse contra el motor real.

```go
// internal/testutil/db.go — helper compartido
func NewTestDB(t *testing.T) *sql.DB {
    t.Helper()
    db, err := sql.Open("sqlite3", ":memory:")
    require.NoError(t, err)
    t.Cleanup(func() { db.Close() })

    // Ejecutar migraciones
    err = goose.Up(db, "../../migrations/sqlite")
    require.NoError(t, err)

    return db
}
```

```go
// internal/db/item_repo_test.go
func TestItemRepo_CreateAndGet(t *testing.T) {
    db := testutil.NewTestDB(t)
    repo := NewItemRepository(db)

    item := &MediaItem{
        ID:        uuid.New(),
        LibraryID: testutil.CreateLibrary(t, db, "Movies", "movies"),
        Type:      ItemMovie,
        Title:     "Inception",
        SortTitle: "inception",
        Year:      2010,
        Path:      "/movies/Inception (2010)/Inception (2010).mkv",
    }

    err := repo.Create(context.Background(), item)
    require.NoError(t, err)

    got, err := repo.GetByID(context.Background(), item.ID)
    require.NoError(t, err)
    assert.Equal(t, "Inception", got.Title)
    assert.Equal(t, 2010, got.Year)
}

func TestItemRepo_Search(t *testing.T) {
    db := testutil.NewTestDB(t)
    repo := NewItemRepository(db)
    libID := testutil.CreateLibrary(t, db, "Movies", "movies")

    // Seed items
    testutil.CreateItem(t, db, libID, "Inception", 2010)
    testutil.CreateItem(t, db, libID, "Interstellar", 2014)
    testutil.CreateItem(t, db, libID, "The Dark Knight", 2008)

    // FTS5 search
    results, total, err := repo.Search(context.Background(), "inception", ListOptions{Limit: 10})
    require.NoError(t, err)
    assert.Equal(t, 1, total)
    assert.Equal(t, "Inception", results[0].Title)
}
```

**Importante**: cada test usa su propio `:memory:` DB — tests paralelos sin conflictos.

### 1.3 Integration Tests — Service Layer

Services con repositorios reales (SQLite in-memory) + mocks de dependencias externas.

```go
// internal/library/scanner_test.go
func TestScanner_FullScan(t *testing.T) {
    db := testutil.NewTestDB(t)

    // Crear filesystem temporal con estructura de películas
    mediaDir := t.TempDir()
    testutil.CreateMediaFile(t, mediaDir, "Inception (2010)/Inception (2010).mkv")
    testutil.CreateMediaFile(t, mediaDir, "The Matrix (1999)/The Matrix (1999).mkv")

    // Mock de MediaAnalyzer — no ejecuta FFprobe real
    analyzer := &MockAnalyzer{
        Result: &AnalysisResult{
            Container: "mkv",
            Duration:  2*time.Hour + 28*time.Minute,
            Streams: []MediaStream{
                {Type: StreamVideo, Codec: "h264", Width: 1920, Height: 1080},
                {Type: StreamAudio, Codec: "aac", Channels: 6, Language: "eng"},
            },
        },
    }

    // Mock de MetadataManager — no llama a TMDb
    metadataMgr := &MockMetadataManager{}

    scanner := NewScanner(
        NewItemRepository(db),
        NewLibraryRepository(db),
        analyzer,
        metadataMgr,
        event.NewBus(),
    )

    lib := testutil.CreateLibraryWithPath(t, db, "Movies", "movies", mediaDir)
    result, err := scanner.ScanLibrary(context.Background(), lib.ID)

    require.NoError(t, err)
    assert.Equal(t, 2, result.Added)
    assert.Equal(t, 0, result.Errors)
}
```

### 1.4 Integration Tests — HTTP Handlers

`httptest` con handlers reales, services mock.

```go
// internal/api/handlers/items_test.go
func TestGetItems_Paginated(t *testing.T) {
    db := testutil.NewTestDB(t)
    libID := testutil.SeedLibraryWithItems(t, db, 50)

    router := api.NewRouter(api.Dependencies{
        ItemRepo:    db.NewItemRepository(db),
        LibraryRepo: db.NewLibraryRepository(db),
        Auth:        testutil.MockAuth(testutil.AdminUser),
    })

    req := httptest.NewRequest("GET", "/api/v1/items?library_id="+libID.String()+"&limit=20&offset=0", nil)
    req.Header.Set("Authorization", "Bearer "+testutil.ValidToken)
    w := httptest.NewRecorder()

    router.ServeHTTP(w, req)

    assert.Equal(t, http.StatusOK, w.Code)

    var resp struct {
        Items []map[string]any `json:"items"`
        Total int              `json:"total"`
    }
    json.NewDecoder(w.Body).Decode(&resp)
    assert.Equal(t, 20, len(resp.Items))
    assert.Equal(t, 50, resp.Total)
}
```

### 1.5 Mocking Strategy

Interfaces definen los contratos, mocks los implementan para tests.

```go
// internal/testutil/mocks.go

// MockAnalyzer — sustituye FFprobe en tests
type MockAnalyzer struct {
    Result *AnalysisResult
    Err    error
}

func (m *MockAnalyzer) Analyze(ctx context.Context, path string) (*AnalysisResult, error) {
    return m.Result, m.Err
}

// MockMetadataManager — sustituye TMDb/Fanart en tests
type MockMetadataManager struct {
    RefreshCalled int
    RefreshErr    error
}

func (m *MockMetadataManager) RefreshItem(ctx context.Context, itemID uuid.UUID, mode RefreshMode) error {
    m.RefreshCalled++
    return m.RefreshErr
}

// MockClock — para tests dependientes del tiempo (JWT expiry, EPG, sessions)
type MockClock struct {
    Now time.Time
}

func (c *MockClock) CurrentTime() time.Time { return c.Now }
func (c *MockClock) Advance(d time.Duration) { c.Now = c.Now.Add(d) }
```

**Regla**: No usar frameworks de mock (mockgen, mockery). Las interfaces son pequeñas — escribir mocks a mano es más claro y mantenible.

### 1.6 Clock Interface (Time-Dependent Tests)

Muchos módulos dependen del tiempo: JWT, sessions, EPG, scan scheduling. Inyectar un reloj testeable:

```go
// internal/clock/clock.go
type Clock interface {
    Now() time.Time
}

type RealClock struct{}
func (RealClock) Now() time.Time { return time.Now() }

// Usado en tests:
// clock := &testutil.MockClock{Now: time.Date(2026, 1, 1, 20, 0, 0, 0, time.UTC)}
// authService := auth.NewService(repo, clock)
// clock.Advance(16 * time.Minute) // JWT debería haber expirado
```

### 1.7 Test Tags y Organización

```go
// Tests rápidos (unit) — se ejecutan siempre
func TestMovieResolver(t *testing.T) { ... }

// Tests de integración — requieren migraciones
//go:build integration
func TestItemRepo_CreateAndGet(t *testing.T) { ... }
```

```makefile
# Makefile
test:              ## Unit tests (rápidos, sin DB)
	go test ./... -short -race -count=1

test-integration:  ## Integration tests (con SQLite in-memory)
	go test ./... -race -count=1 -tags=integration

test-all:          ## Todos los tests
	go test ./... -race -count=1 -tags=integration

test-cover:        ## Coverage report
	go test ./... -race -coverprofile=coverage.out -tags=integration
	go tool cover -html=coverage.out -o coverage.html
```

---

## 2. Frontend (React + TypeScript)

### 2.1 Unit Tests (Vitest)

Tests de componentes aislados y hooks.

```tsx
// web/src/components/media/PosterCard.test.tsx
import { render, screen } from "@testing-library/react";
import { PosterCard } from "./PosterCard";

describe("PosterCard", () => {
  it("renders title and year", () => {
    render(<PosterCard title="Inception" year={2010} posterUrl="/img.jpg" />);
    expect(screen.getByText("Inception")).toBeInTheDocument();
    expect(screen.getByText("2010")).toBeInTheDocument();
  });

  it("shows watched badge when completed", () => {
    render(<PosterCard title="Inception" year={2010} completed={true} />);
    expect(screen.getByTestId("watched-badge")).toBeInTheDocument();
  });

  it("shows progress bar for partially watched", () => {
    render(<PosterCard title="Inception" year={2010} progress={0.45} />);
    const bar = screen.getByRole("progressbar");
    expect(bar).toHaveAttribute("aria-valuenow", "45");
  });
});
```

### 2.2 Hook Tests (Vitest + renderHook)

```tsx
// web/src/hooks/useProgress.test.ts
import { renderHook, act } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useProgress } from "./useProgress";

const wrapper = ({ children }) => (
  <QueryClientProvider client={new QueryClient()}>
    {children}
  </QueryClientProvider>
);

describe("useProgress", () => {
  it("debounces progress updates to every 10 seconds", async () => {
    const { result } = renderHook(() => useProgress("item-123"), { wrapper });

    // Múltiples updates rápidos
    act(() => result.current.updatePosition(1000));
    act(() => result.current.updatePosition(2000));
    act(() => result.current.updatePosition(3000));

    // Solo debería haber enviado 1 request (debounced)
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledTimes(1);
    });
  });
});
```

### 2.3 API Mocking (MSW)

Mock Service Worker intercepta requests a nivel de Service Worker — realista y funciona tanto en tests como en dev.

```tsx
// web/src/test/handlers.ts
import { http, HttpResponse } from "msw";

export const handlers = [
  // Auth
  http.post("/api/v1/auth/login", async ({ request }) => {
    const body = await request.json();
    if (body.username === "admin" && body.password === "password") {
      return HttpResponse.json({
        access_token: "test-jwt-token",
        refresh_token: "test-refresh",
        user: { id: "u1", username: "admin", role: "admin" },
      });
    }
    return HttpResponse.json({ error: "invalid credentials" }, { status: 401 });
  }),

  // Items
  http.get("/api/v1/items", ({ request }) => {
    const url = new URL(request.url);
    const limit = parseInt(url.searchParams.get("limit") || "20");
    return HttpResponse.json({
      items: generateMockItems(limit),
      total: 150,
    });
  }),

  // Streaming — devolver playlist HLS fake
  http.get("/api/v1/stream/:itemId/master.m3u8", () => {
    return new HttpResponse(MOCK_HLS_PLAYLIST, {
      headers: { "Content-Type": "application/vnd.apple.mpegurl" },
    });
  }),

  // Progress
  http.put("/api/v1/me/progress/:itemId", () => {
    return HttpResponse.json({ ok: true });
  }),
];
```

```tsx
// web/src/test/setup.ts
import { setupServer } from "msw/node";
import { handlers } from "./handlers";

export const server = setupServer(...handlers);

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());
```

### 2.4 Player Tests

hls.js no puede ejecutarse en jsdom (no hay MediaSource API). Estrategia:

```tsx
// web/src/components/player/VideoPlayer.test.tsx
import { vi } from "vitest";

// Mock de hls.js a nivel de módulo
vi.mock("hls.js", () => ({
  default: vi.fn().mockImplementation(() => ({
    loadSource: vi.fn(),
    attachMedia: vi.fn(),
    on: vi.fn(),
    off: vi.fn(),
    destroy: vi.fn(),
    levels: [{ height: 1080 }, { height: 720 }, { height: 480 }],
    currentLevel: 0,
  })),
  Events: {
    MANIFEST_PARSED: "hlsManifestParsed",
    ERROR: "hlsError",
    LEVEL_SWITCHED: "hlsLevelSwitched",
  },
}));

describe("VideoPlayer", () => {
  it("initializes hls.js with the stream URL", () => {
    render(<VideoPlayer src="/api/v1/stream/123/master.m3u8" />);
    expect(Hls).toHaveBeenCalled();
    expect(Hls.mock.results[0].value.loadSource).toHaveBeenCalledWith(
      "/api/v1/stream/123/master.m3u8"
    );
  });

  it("renders play/pause controls", async () => {
    render(<VideoPlayer src="/api/v1/stream/123/master.m3u8" />);
    const playBtn = screen.getByRole("button", { name: /play/i });
    expect(playBtn).toBeInTheDocument();
  });

  it("shows skip intro button when segment data exists", () => {
    render(
      <VideoPlayer
        src="/api/v1/stream/123/master.m3u8"
        segments={[{ type: "intro", start: 30, end: 90 }]}
      />
    );
    // Simular posición en el rango de intro
    // ...
  });
});
```

### 2.5 E2E Tests (Playwright)

Flujos completos en navegador real contra un backend mock o real.

```ts
// web/e2e/auth.spec.ts
import { test, expect } from "@playwright/test";

test.describe("Authentication", () => {
  test("login flow", async ({ page }) => {
    await page.goto("/");
    // Redirige a login
    await expect(page).toHaveURL("/login");

    await page.fill('[name="username"]', "admin");
    await page.fill('[name="password"]', "password");
    await page.click('button[type="submit"]');

    // Redirige a home
    await expect(page).toHaveURL("/");
    await expect(page.getByText("Continue Watching")).toBeVisible();
  });

  test("invalid credentials show error", async ({ page }) => {
    await page.goto("/login");
    await page.fill('[name="username"]', "admin");
    await page.fill('[name="password"]', "wrong");
    await page.click('button[type="submit"]');

    await expect(page.getByText(/invalid/i)).toBeVisible();
  });
});
```

```ts
// web/e2e/library.spec.ts
test.describe("Library browsing", () => {
  test.beforeEach(async ({ page }) => {
    // Login helper
    await loginAs(page, "admin", "password");
  });

  test("browse movies with infinite scroll", async ({ page }) => {
    await page.goto("/movies");
    // Verificar que se renderizan posters
    const cards = page.locator('[data-testid="poster-card"]');
    await expect(cards.first()).toBeVisible();
    expect(await cards.count()).toBeGreaterThan(0);
  });

  test("search filters results", async ({ page }) => {
    await page.goto("/movies");
    await page.fill('[data-testid="search-input"]', "inception");
    // Debounce 300ms
    await page.waitForTimeout(400);
    const cards = page.locator('[data-testid="poster-card"]');
    // Debería filtrar
    const titles = await cards.allTextContents();
    expect(titles.some((t) => t.includes("Inception"))).toBe(true);
  });
});
```

### 2.6 E2E con Backend Real (Opcional)

Para tests de confianza máxima, levantar el backend Go:

```ts
// web/e2e/playwright.config.ts
export default defineConfig({
  webServer: [
    {
      // Backend Go con DB de test
      command: "go run ./cmd/hubplay --config testdata/test.yaml",
      port: 8096,
      reuseExistingServer: !process.env.CI,
    },
    {
      // Frontend Vite dev
      command: "npm run dev",
      port: 5173,
      reuseExistingServer: !process.env.CI,
    },
  ],
});
```

---

## 3. Test Fixtures & Helpers

### 3.1 Backend Fixtures

```
testdata/
├── media/                       # Archivos media mínimos para tests
│   ├── sample.mkv               # Archivo MKV válido (~100KB, 1 segundo)
│   └── sample.mp4               # Archivo MP4 válido (~100KB, 1 segundo)
├── playlists/
│   ├── valid.m3u                 # Playlist M3U de ejemplo
│   ├── malformed.m3u             # M3U con errores (líneas faltantes, URLs rotas)
│   └── large.m3u                 # 500 canales para tests de rendimiento
├── epg/
│   ├── valid.xml                 # XMLTV EPG de ejemplo
│   └── malformed.xml             # XML inválido
├── config/
│   └── test.yaml                 # Config mínima para tests de integración
└── golden/                       # Golden files para snapshot testing
    ├── hls_master.m3u8           # Expected HLS master playlist
    └── ffmpeg_transcode_1080p.txt # Expected FFmpeg command args
```

### 3.2 Backend Test Helpers

```go
// internal/testutil/helpers.go
package testutil

// Fixtures de usuarios para tests
var (
    AdminUser = &User{ID: uuid.MustParse("..."), Username: "admin", Role: RoleAdmin}
    RegularUser = &User{ID: uuid.MustParse("..."), Username: "user1", Role: RoleUser}
    ValidToken = "eyJhbGciOiJIUzI1NiIs..." // JWT pre-generado para AdminUser
)

// CreateLibrary crea una library en la DB de test y devuelve su ID
func CreateLibrary(t *testing.T, db *sql.DB, name, contentType string) uuid.UUID { ... }

// CreateItem crea un item en la DB y devuelve el objeto completo
func CreateItem(t *testing.T, db *sql.DB, libID uuid.UUID, title string, year int) *MediaItem { ... }

// SeedLibraryWithItems crea una library con N items para tests de paginación
func SeedLibraryWithItems(t *testing.T, db *sql.DB, count int) uuid.UUID { ... }

// CreateMediaFile crea un archivo vacío con la estructura de directorios correcta
func CreateMediaFile(t *testing.T, baseDir, relativePath string) string { ... }

// MockAuth retorna un middleware que inyecta el usuario dado sin validar JWT
func MockAuth(user *User) func(http.Handler) http.Handler { ... }
```

### 3.3 Frontend Test Utilities

```tsx
// web/src/test/utils.tsx
import { render } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";

// Wrapper con todos los providers necesarios
export function renderWithProviders(
  ui: React.ReactElement,
  { route = "/", ...options } = {}
) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false }, // No retry en tests
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[route]}>{ui}</MemoryRouter>
    </QueryClientProvider>,
    options
  );
}

// Generar datos mock
export function generateMockItems(count: number) {
  return Array.from({ length: count }, (_, i) => ({
    id: `item-${i}`,
    title: `Movie ${i}`,
    year: 2020 + (i % 5),
    type: "movie",
    poster_url: `/img/poster-${i}.jpg`,
  }));
}
```

---

## 4. Coverage Goals

| Capa | Target | Justificación |
|------|--------|---------------|
| Resolvers (file naming) | **95%+** | Core del scanner, muchos edge cases, fácil de testear |
| Repository (SQL) | **90%+** | Queries son el corazón del sistema, tests con DB real |
| Services (business logic) | **80%+** | Lógica compleja, mocks de dependencias |
| HTTP Handlers | **70%+** | Validation + routing, service logic ya testeada |
| FFmpeg builder | **90%+** | Errores en commands son difíciles de debuggear |
| Frontend components | **70%+** | UX critical, visual regressions |
| Frontend hooks | **80%+** | State management, API integration |
| E2E flows | — | No medir coverage, medir flujos críticos cubiertos |

**Flujos E2E críticos:**
1. Login → home → browse → play → progress saved
2. Admin → create library → scan → items appear
3. Search → filter → detail → play
4. Live TV → channel switch → EPG browse
5. Settings → change preferences → verify applied

---

## 5. CI Pipeline (Tests)

```yaml
# Qué corre en cada PR
lint:        golangci-lint + eslint
test-unit:   go test ./... -short (< 30s)
test-integ:  go test ./... -tags=integration (< 2min)
test-front:  npm run test (Vitest, < 1min)
test-e2e:    npm run test:e2e (Playwright, < 5min)
build:       go build + npm run build (verifica compilación)
```

Detalle completo del pipeline en [ci-cd.md](./ci-cd.md).

---

## 6. Directory Structure (Test Files)

```
# Backend — tests junto al código
internal/
├── library/
│   ├── scanner.go
│   ├── scanner_test.go          # Unit + integration tests
│   ├── watcher.go
│   └── watcher_test.go
├── db/
│   ├── item_repo.go
│   └── item_repo_test.go        # Integration (SQLite in-memory)
├── testutil/
│   ├── db.go                    # NewTestDB helper
│   ├── mocks.go                 # Mock implementations
│   ├── helpers.go               # Seed data, fixtures
│   └── clock.go                 # MockClock
└── ...

# Frontend — tests junto al código + E2E aparte
web/src/
├── components/
│   ├── media/
│   │   ├── PosterCard.tsx
│   │   └── PosterCard.test.tsx
│   └── player/
│       ├── VideoPlayer.tsx
│       └── VideoPlayer.test.tsx
├── hooks/
│   ├── useProgress.ts
│   └── useProgress.test.ts
├── test/
│   ├── setup.ts                 # MSW setup, global mocks
│   ├── handlers.ts              # MSW request handlers
│   └── utils.tsx                # renderWithProviders, generators
└── ...

web/e2e/
├── auth.spec.ts
├── library.spec.ts
├── player.spec.ts
├── live-tv.spec.ts
└── admin.spec.ts

testdata/                         # Fixtures compartidas backend
├── media/
├── playlists/
├── epg/
└── golden/
```
