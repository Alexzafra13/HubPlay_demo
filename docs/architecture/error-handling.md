# Error Handling & Structured Logging — Design Document

## Overview

Errores se manejan con el patrón estándar de Go: error values, wrapping con `%w`, y tipos de error para control de flujo. Logging con `log/slog` (stdlib desde Go 1.21). Sin frameworks externos.

---

## 1. Error Strategy por Capa

```
Handler Layer    →  Convierte errores internos a HTTP status + error code
                    (mapeo definido en error-codes.md)
    ▲
    │ domain errors (typed)
    │
Service Layer    →  Lógica de negocio. Retorna domain errors.
                    Wrappea errores de capas inferiores con contexto.
    ▲
    │ sql errors, io errors, etc.
    │
Repository Layer →  Convierte errores de DB a domain errors.
                    sql.ErrNoRows → ErrNotFound
    ▲
    │ driver-specific errors
    │
Database/FFmpeg  →  Errores raw del driver o proceso externo
```

---

## 2. Domain Errors (Sentinel + Typed)

```go
// internal/domain/errors.go
package domain

import "errors"

// ─── Sentinel Errors (para errors.Is) ───

var (
    // Resource errors
    ErrNotFound      = errors.New("not found")
    ErrAlreadyExists = errors.New("already exists")
    ErrConflict      = errors.New("conflict")

    // Auth errors
    ErrUnauthorized    = errors.New("unauthorized")
    ErrForbidden       = errors.New("forbidden")
    ErrInvalidToken    = errors.New("invalid token")
    ErrTokenExpired    = errors.New("token expired")
    ErrInvalidPassword = errors.New("invalid password")
    ErrAccountDisabled = errors.New("account disabled")

    // Validation
    ErrValidation = errors.New("validation error")

    // Streaming
    ErrTranscodeBusy   = errors.New("transcode slots full")
    ErrUnsupportedCodec = errors.New("unsupported codec")

    // Federation
    ErrPeerOffline     = errors.New("peer offline")
    ErrPeerUnauthorized = errors.New("peer not authorized")

    // Plugin
    ErrPluginCrashed   = errors.New("plugin crashed")
    ErrPluginTimeout   = errors.New("plugin timeout")
)
```

```go
// ─── Typed Error (para errors.As + datos extra) ───

// ValidationError contiene detalles de qué campos fallaron
type ValidationError struct {
    Fields map[string]string // field → message
}

func (e *ValidationError) Error() string {
    return fmt.Sprintf("validation failed: %v", e.Fields)
}

func (e *ValidationError) Unwrap() error {
    return ErrValidation
}

// ScanError para errores por archivo durante scan
type ScanError struct {
    Path    string
    Wrapped error
}

func (e *ScanError) Error() string {
    return fmt.Sprintf("scan error for %s: %v", e.Path, e.Wrapped)
}

func (e *ScanError) Unwrap() error {
    return e.Wrapped
}
```

---

## 3. Error Wrapping Pattern

Cada capa añade contexto al envolver errores:

```go
// Repository → wrappea errores de DB
func (r *ItemRepo) GetByID(ctx context.Context, id uuid.UUID) (*MediaItem, error) {
    item, err := r.q.GetItem(ctx, id.String())
    if err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            return nil, fmt.Errorf("item %s: %w", id, domain.ErrNotFound)
        }
        return nil, fmt.Errorf("querying item %s: %w", id, err)
    }
    return mapItem(item), nil
}

// Service → wrappea con contexto de negocio
func (s *Scanner) ScanLibrary(ctx context.Context, libID uuid.UUID) (*ScanResult, error) {
    lib, err := s.libraryRepo.GetByID(ctx, libID)
    if err != nil {
        return nil, fmt.Errorf("scanning library: %w", err)
    }

    for _, path := range lib.Paths {
        if err := s.scanPath(ctx, lib, path); err != nil {
            // Error por archivo → loguear y continuar, no abortar
            s.logger.Warn("scan error", "path", path, "error", err)
            result.Errors = append(result.Errors, &ScanError{Path: path, Wrapped: err})
        }
    }
    return result, nil
}

// Handler → mapea a HTTP
func (h *ItemHandler) GetItem(w http.ResponseWriter, r *http.Request) {
    id, err := uuid.Parse(chi.URLParam(r, "id"))
    if err != nil {
        respondError(w, http.StatusBadRequest, "INVALID_ID", "invalid item ID")
        return
    }

    item, err := h.items.GetByID(r.Context(), id)
    if err != nil {
        handleServiceError(w, err)
        return
    }

    respondJSON(w, http.StatusOK, item)
}
```

---

## 4. Error → HTTP Mapping

```go
// internal/api/errors.go
func handleServiceError(w http.ResponseWriter, err error) {
    switch {
    case errors.Is(err, domain.ErrNotFound):
        respondError(w, http.StatusNotFound, "NOT_FOUND", err.Error())

    case errors.Is(err, domain.ErrAlreadyExists):
        respondError(w, http.StatusConflict, "ALREADY_EXISTS", err.Error())

    case errors.Is(err, domain.ErrUnauthorized), errors.Is(err, domain.ErrInvalidToken):
        respondError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")

    case errors.Is(err, domain.ErrForbidden):
        respondError(w, http.StatusForbidden, "FORBIDDEN", "insufficient permissions")

    case errors.Is(err, domain.ErrValidation):
        var valErr *domain.ValidationError
        if errors.As(err, &valErr) {
            respondJSON(w, http.StatusBadRequest, map[string]any{
                "error":  "VALIDATION_ERROR",
                "fields": valErr.Fields,
            })
            return
        }
        respondError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())

    case errors.Is(err, domain.ErrTranscodeBusy):
        w.Header().Set("Retry-After", "30")
        respondError(w, http.StatusServiceUnavailable, "TRANSCODE_BUSY", "all transcode slots in use")

    case errors.Is(err, domain.ErrAccountDisabled):
        respondError(w, http.StatusForbidden, "ACCOUNT_DISABLED", "account is disabled")

    default:
        // Error inesperado — loguear internamente, no exponer al cliente
        slog.Error("unhandled error", "error", err)
        respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
    }
}

func respondError(w http.ResponseWriter, status int, code, message string) {
    respondJSON(w, status, map[string]string{
        "error":   code,
        "message": message,
    })
}

func respondJSON(w http.ResponseWriter, status int, data any) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(data)
}
```

---

## 5. Structured Logging con `slog`

### Por qué slog

- Stdlib desde Go 1.21 — sin dependencias externas
- Structured (key-value) — parseable por herramientas (Grafana, Loki)
- JSON handler en producción, text handler en desarrollo
- Log groups para contexto jerárquico
- Zero-allocation para niveles deshabilitados

### Setup

```go
// internal/logging/logging.go
package logging

func New(cfg LogConfig) *slog.Logger {
    var handler slog.Handler

    opts := &slog.HandlerOptions{
        Level: parseLevel(cfg.Level), // debug/info/warn/error
        ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
            // Redact sensitive fields
            if a.Key == "password" || a.Key == "token" || a.Key == "refresh_token" {
                return slog.String(a.Key, "[REDACTED]")
            }
            return a
        },
    }

    if cfg.Format == "json" {
        handler = slog.NewJSONHandler(os.Stdout, opts)
    } else {
        handler = slog.NewTextHandler(os.Stdout, opts)
    }

    return slog.New(handler)
}
```

### Uso por módulo

Cada servicio recibe un logger con contexto de módulo:

```go
// Constructor
func NewScanner(repos ..., logger *slog.Logger) *Scanner {
    return &Scanner{
        // ...,
        logger: logger.With("module", "scanner"),
    }
}

// En métodos
func (s *Scanner) ScanLibrary(ctx context.Context, libID uuid.UUID) (*ScanResult, error) {
    log := s.logger.With("library_id", libID)
    log.Info("scan started")

    // ... scanning ...

    log.Info("scan completed",
        "added", result.Added,
        "updated", result.Updated,
        "removed", result.Removed,
        "errors", len(result.Errors),
        "duration", result.Duration,
    )
    return result, nil
}
```

### Request Logging (Middleware)

```go
// internal/api/middleware.go
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            start := time.Now()
            requestID := middleware.GetReqID(r.Context())

            // Wrapper para capturar status code
            ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

            next.ServeHTTP(ww, r)

            logger.Info("request",
                "request_id", requestID,
                "method", r.Method,
                "path", r.URL.Path,
                "status", ww.Status(),
                "bytes", ww.BytesWritten(),
                "duration_ms", time.Since(start).Milliseconds(),
                "ip", r.RemoteAddr,
                "user_agent", r.UserAgent(),
            )
        })
    }
}
```

### Log Output Examples

**Desarrollo (text):**
```
time=2026-03-13T10:00:00Z level=INFO msg="server started" addr=:8096
time=2026-03-13T10:00:05Z level=INFO msg="scan started" module=scanner library_id=abc-123
time=2026-03-13T10:00:12Z level=INFO msg="scan completed" module=scanner library_id=abc-123 added=42 duration=7.2s
time=2026-03-13T10:00:13Z level=INFO msg=request request_id=req-456 method=GET path=/api/v1/items status=200 duration_ms=12
```

**Producción (JSON):**
```json
{"time":"2026-03-13T10:00:12Z","level":"INFO","msg":"scan completed","module":"scanner","library_id":"abc-123","added":42,"duration":"7.2s"}
```

### Qué se loguea y qué NO

| Se loguea | NO se loguea |
|-----------|-------------|
| Request method, path, status, duration | Passwords, tokens, refresh tokens |
| Scan results (added, errors, duration) | Request bodies (pueden tener datos sensibles) |
| Error messages con contexto | Stack traces en producción (solo en debug) |
| Plugin lifecycle (start, crash, restart) | Contenido de media files |
| Federation peer status changes | IPs si `logging.log_ips: false` |
| Auth events (login, logout, failed login) | JWT payloads |
| FFmpeg commands (en debug level) | API keys |

---

## 6. Configuration

```yaml
# hubplay.yaml
logging:
  level: "info"          # debug | info | warn | error
  format: "text"         # text (dev) | json (production)
  log_ips: true          # Log client IPs (disable for privacy)
```

---

## 7. Testing Errors

```go
// Verificar tipo de error
func TestGetItem_NotFound(t *testing.T) {
    repo := NewItemRepository(testutil.NewTestDB(t))
    _, err := repo.GetByID(context.Background(), uuid.New())

    require.Error(t, err)
    assert.True(t, errors.Is(err, domain.ErrNotFound))
}

// Verificar error de validación con campos
func TestCreateUser_InvalidUsername(t *testing.T) {
    svc := user.NewService(mockRepo, slog.Default())
    _, err := svc.Create(context.Background(), user.CreateRequest{
        Username: "ab",  // demasiado corto
        Password: "12345678",
    })

    require.Error(t, err)
    var valErr *domain.ValidationError
    require.True(t, errors.As(err, &valErr))
    assert.Contains(t, valErr.Fields, "username")
}
```
