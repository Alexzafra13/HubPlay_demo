# Error Codes & Handling — Design Document

Catálogo estandarizado de errores de la API. Cada error tiene un código máquina (`code`), un mensaje humano (`message`), y un HTTP status. Los clientes pueden usar `code` para i18n y lógica de retry.

---

## Error Response Format

```json
{
    "error": {
        "code": "ITEM_NOT_FOUND",
        "message": "Media item not found",
        "details": {                         // Optional, context-dependent
            "item_id": "uuid"
        }
    }
}
```

```go
// Go implementation
type APIError struct {
    Code    string         `json:"code"`
    Message string         `json:"message"`
    Details map[string]any `json:"details,omitempty"`
}

// Predefined errors
var (
    ErrInvalidCredentials = &APIError{Code: "INVALID_CREDENTIALS", Message: "Invalid username or password"}
    ErrItemNotFound       = &APIError{Code: "ITEM_NOT_FOUND", Message: "Media item not found"}
    // ... etc
)
```

---

## Error Catalog

### Authentication & Authorization

| Code | HTTP | Message | When | Client Action |
|------|------|---------|------|---------------|
| `INVALID_CREDENTIALS` | 401 | Invalid username or password | Login failed | Show error, clear password field |
| `TOKEN_EXPIRED` | 401 | Access token has expired | JWT expired | Auto-refresh using refresh token |
| `TOKEN_INVALID` | 401 | Invalid or malformed token | Bad JWT signature/format | Redirect to login |
| `REFRESH_TOKEN_EXPIRED` | 401 | Refresh token has expired or been revoked | Refresh token invalid | Redirect to login |
| `FORBIDDEN` | 403 | You don't have permission for this action | User role insufficient | Show "access denied" |
| `LIBRARY_ACCESS_DENIED` | 403 | You don't have access to this library | User not assigned to library | Hide library from UI |
| `REGISTRATION_DISABLED` | 403 | User registration is disabled | Self-registration off | Show "contact admin" |
| `RATE_LIMITED` | 429 | Too many requests. Try again in {n} minutes | Rate limit hit | Show retry timer, back off |

### Resources

| Code | HTTP | Message | When | Client Action |
|------|------|---------|------|---------------|
| `ITEM_NOT_FOUND` | 404 | Media item not found | Item ID doesn't exist | Show 404 page |
| `LIBRARY_NOT_FOUND` | 404 | Library not found | Library ID doesn't exist | Redirect to home |
| `USER_NOT_FOUND` | 404 | User not found | User ID doesn't exist | Show error |
| `SESSION_NOT_FOUND` | 404 | Session not found | Session ID doesn't exist | Remove from list |
| `PEER_NOT_FOUND` | 404 | Federation peer not found | Peer ID doesn't exist | Show error |
| `PLUGIN_NOT_FOUND` | 404 | Plugin not found | Plugin name doesn't exist | Show error |
| `CHANNEL_NOT_FOUND` | 404 | Channel not found | IPTV channel doesn't exist | Remove from guide |
| `SUBTITLE_NOT_FOUND` | 404 | Subtitle track not found | Stream index invalid | Fallback to no subtitles |

### Validation

| Code | HTTP | Message | When | Client Action |
|------|------|---------|------|---------------|
| `VALIDATION_ERROR` | 400 | Validation failed | General field validation | Highlight fields from `details.fields` |
| `INVALID_JSON` | 400 | Invalid or malformed JSON body | Can't parse request body | Fix request |
| `INVALID_QUERY_PARAM` | 400 | Invalid query parameter: {param} | Bad filter/sort/pagination | Fix request |
| `USERNAME_TAKEN` | 409 | Username already exists | Duplicate username on create | Show "already taken" on field |
| `LIBRARY_NAME_TAKEN` | 409 | A library with this name already exists | Duplicate library name | Show inline error |
| `PASSWORD_TOO_SHORT` | 400 | Password must be at least 8 characters | Password < 8 chars | Show inline error |
| `INVALID_PATH` | 400 | Invalid or inaccessible file path | Library path doesn't exist or no read permission | Show path error |

`VALIDATION_ERROR` details:
```json
{
    "error": {
        "code": "VALIDATION_ERROR",
        "message": "Validation failed",
        "details": {
            "fields": {
                "username": "Must be 3-32 characters, alphanumeric or underscore",
                "password": "Must be at least 8 characters"
            }
        }
    }
}
```

### Streaming

| Code | HTTP | Message | When | Client Action |
|------|------|---------|------|---------------|
| `STREAM_SESSION_EXPIRED` | 401 | Stream session has expired | Session token > 1 hour | Request new session |
| `TRANSCODE_FAILED` | 500 | Transcoding failed | FFmpeg error | Show "playback error", offer direct play |
| `TRANSCODE_QUEUE_FULL` | 503 | All transcode slots are in use | Max concurrent reached | Show "server busy", retry later |
| `FILE_UNAVAILABLE` | 404 | Media file not found on disk | File moved/deleted | Show "file unavailable" |
| `CODEC_UNSUPPORTED` | 422 | Cannot transcode this media format | Unsupported input codec | Show "format not supported" |

### IPTV

| Code | HTTP | Message | When | Client Action |
|------|------|---------|------|---------------|
| `CHANNEL_OFFLINE` | 502 | Channel stream is unavailable | Upstream provider down | Show "offline" badge |
| `EPG_UNAVAILABLE` | 502 | EPG data could not be loaded | EPG URL failed | Show channel list without EPG |
| `M3U_PARSE_ERROR` | 422 | Failed to parse M3U playlist | Malformed M3U | Show error to admin |

### Federation

| Code | HTTP | Message | When | Client Action |
|------|------|---------|------|---------------|
| `PEER_OFFLINE` | 502 | Federation peer is not reachable | Peer server down | Show "offline" badge |
| `PEER_REJECTED` | 403 | Peer rejected the request | Permission denied by peer | Show "not permitted" |
| `INVALID_INVITE_CODE` | 400 | Invalid or expired invite code | Bad code on accept | Show "invalid code" |
| `PEER_ALREADY_LINKED` | 409 | Already linked to this server | Duplicate link attempt | Show "already connected" |
| `PEER_BANDWIDTH_EXCEEDED` | 429 | Peer bandwidth limit reached | Too many streams from peer | Queue or deny, show "busy" |
| `FEDERATION_DISABLED` | 403 | Federation is not enabled on this server | Feature off | Hide federation UI |

### Plugins

| Code | HTTP | Message | When | Client Action |
|------|------|---------|------|---------------|
| `PLUGIN_INSTALL_FAILED` | 500 | Failed to install plugin | Download/validation failed | Show error, check URL |
| `PLUGIN_DISABLED` | 422 | Plugin is disabled | Action on disabled plugin | Show "enable first" |
| `PLUGIN_CRASHED` | 500 | Plugin has crashed and is restarting | Plugin process died | Show "restarting", auto-retry |
| `PLUGIN_INCOMPATIBLE` | 422 | Plugin is not compatible with this version | API version mismatch | Show "update plugin" |

### System

| Code | HTTP | Message | When | Client Action |
|------|------|---------|------|---------------|
| `INTERNAL_ERROR` | 500 | An unexpected error occurred | Unhandled error | Show generic error, check logs |
| `SERVICE_UNAVAILABLE` | 503 | Server is starting up | Request during startup | Retry with backoff |
| `SETUP_REQUIRED` | 503 | First-time setup has not been completed | No admin user yet | Redirect to `/setup` |
| `FFMPEG_NOT_FOUND` | 500 | FFmpeg is not installed or not in PATH | Missing FFmpeg | Show error to admin |
| `DISK_FULL` | 507 | Insufficient disk space | Cache/config disk full | Show "disk full" to admin |

---

## Client-Side Error Handling

### Recommended Strategy

```typescript
// api/client.ts
async function apiRequest<T>(url: string, options?: RequestInit): Promise<T> {
    const response = await fetch(url, {
        ...options,
        headers: {
            'Authorization': `Bearer ${getAccessToken()}`,
            'Content-Type': 'application/json',
            ...options?.headers,
        },
    });

    if (!response.ok) {
        const body = await response.json();
        const error = body.error;

        switch (error.code) {
            case 'TOKEN_EXPIRED':
                // Auto-refresh and retry
                await refreshAccessToken();
                return apiRequest<T>(url, options);

            case 'REFRESH_TOKEN_EXPIRED':
            case 'TOKEN_INVALID':
                // Session ended
                redirectToLogin();
                throw new SessionExpiredError();

            case 'RATE_LIMITED':
                // Retry after cooldown
                const retryAfter = response.headers.get('Retry-After');
                throw new RateLimitError(parseInt(retryAfter || '60'));

            default:
                throw new APIError(error.code, error.message, error.details);
        }
    }

    return response.json();
}
```

### Retry Strategy

| Error Code | Retry? | Strategy |
|------------|--------|----------|
| `TOKEN_EXPIRED` | Yes | Auto-refresh + immediate retry (1 attempt) |
| `RATE_LIMITED` | Yes | Wait `Retry-After` header seconds |
| `TRANSCODE_QUEUE_FULL` | Yes | Exponential backoff: 2s, 4s, 8s (max 3 retries) |
| `PEER_OFFLINE` | Yes | Background retry every 30s (federation catalog) |
| `SERVICE_UNAVAILABLE` | Yes | Backoff: 1s, 2s, 4s (startup race) |
| `INTERNAL_ERROR` | No | Show error, don't retry (likely a bug) |
| `4xx` (others) | No | Show error, user must fix input |

### Retry-After Header

Rate limit responses include standard `Retry-After` header:

```
HTTP/1.1 429 Too Many Requests
Retry-After: 120
Content-Type: application/json

{ "error": { "code": "RATE_LIMITED", "message": "Too many login attempts. Try again in 2 minutes" } }
```

---

## Go Error Mapping

```go
// internal/api/errors.go

// Render maps domain errors to API errors
func mapError(err error) (int, *APIError) {
    switch {
    case errors.Is(err, auth.ErrInvalidCredentials):
        return 401, &APIError{Code: "INVALID_CREDENTIALS", Message: "Invalid username or password"}
    case errors.Is(err, auth.ErrTokenExpired):
        return 401, &APIError{Code: "TOKEN_EXPIRED", Message: "Access token has expired"}
    case errors.Is(err, domain.ErrNotFound):
        return 404, &APIError{Code: "ITEM_NOT_FOUND", Message: "Media item not found"}
    case errors.Is(err, domain.ErrForbidden):
        return 403, &APIError{Code: "FORBIDDEN", Message: "You don't have permission for this action"}
    case errors.Is(err, streaming.ErrQueueFull):
        return 503, &APIError{Code: "TRANSCODE_QUEUE_FULL", Message: "All transcode slots are in use"}
    // ... etc
    default:
        // Log full error, return generic message (never leak internals)
        slog.Error("unhandled error", "err", err)
        return 500, &APIError{Code: "INTERNAL_ERROR", Message: "An unexpected error occurred"}
    }
}
```

Domain errors never leak to the client. Internal details (stack traces, SQL errors, file paths) are logged server-side only.
