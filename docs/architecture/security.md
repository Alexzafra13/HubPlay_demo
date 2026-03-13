# Security Architecture — Design Document

Self-hosted application: the admin owns the server, the network, and the data. Security design prioritizes protecting user data and media access without requiring external services or phone-home auth.

---

## 1. Threat Model

### What We Protect
- User credentials and personal data (watch history, preferences)
- Media library access (only authorized users see their assigned libraries)
- Admin panel (only admins can manage server)
- Federation trust (peer servers can't escalate privileges)
- Plugin isolation (third-party code can't compromise the host)

### Trust Boundaries

```
┌─────────────────────────────────────────────────┐
│ INTERNET (untrusted)                            │
│                                                 │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐     │
│  │ Browser  │  │ TV App   │  │ Peer     │     │
│  │ Client   │  │ Client   │  │ Server   │     │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘     │
│       │              │              │           │
└───────┼──────────────┼──────────────┼───────────┘
        │              │              │
   ┌────▼──────────────▼──────────────▼────┐
   │ REVERSE PROXY (nginx/caddy)           │  ← TLS termination
   │ Rate limiting, HTTPS redirect         │
   └────────────────┬──────────────────────┘
                    │
   ┌────────────────▼──────────────────────┐
   │ HUBPLAY SERVER (trusted)              │
   │                                       │
   │  API layer → Auth middleware → Handler │
   │                                       │
   │  ┌─────────┐  ┌─────────┐            │
   │  │ SQLite  │  │ Plugins │            │
   │  │ (local) │  │ (child  │            │
   │  │         │  │ process)│            │
   │  └─────────┘  └─────────┘            │
   └───────────────────────────────────────┘
```

### Assumptions
- Server runs on a machine the admin controls (home server, VPS, NAS)
- Admin is trusted — they have full access to config, database, and filesystem
- Network exposure varies: LAN-only, VPN, or public internet via reverse proxy
- Plugins are installed by admin only — user trust decision, not sandboxed at OS level (yet)

---

## 2. Authentication Security

### Password Storage
- **bcrypt** with cost factor 12 (default, configurable)
- Passwords never logged, never returned in API responses
- Failed login attempts: **rate limited** at 5 attempts per username per 15 minutes
- After lockout: 15-minute cooldown, no account lockout (prevents DoS on usernames)

### JWT Tokens
- Access token: **15 minutes**, signed with HMAC-SHA256
- Refresh token: **30 days**, opaque random string, stored hashed (SHA-256) in `sessions` table
- JWT secret: auto-generated on first run (32 bytes from `crypto/rand`), stored in config
- Token rotation: new refresh token issued on each refresh (old one invalidated)
- Logout: refresh token deleted from DB → access token expires naturally in ≤15 min

### Session Management
- Sessions tracked in `sessions` table with device info, IP, last active timestamp
- Admin can revoke any user's sessions
- User can revoke their own sessions ("log out everywhere")
- Inactive sessions (no refresh in 30 days) auto-cleaned by background job

### QuickConnect (TV/Device Pairing)
- 6-character alphanumeric code, valid for **5 minutes**
- Codes are single-use: consumed on authorization
- TV polls every 2s — rate limited to prevent brute force (6^6 = ~46K combinations is low entropy, but 5-min window + rate limit makes it acceptable for LAN use)
- Code stored in memory only (not DB), expires on server restart

---

## 3. API Security

### Middleware Stack (applied in order)

```go
// router.go — middleware chain
r.Use(middleware.RealIP)           // Trust X-Forwarded-For from reverse proxy
r.Use(middleware.RequestID)        // Trace ID for every request
r.Use(middleware.Logger)           // Structured logging (no sensitive data)
r.Use(middleware.Recoverer)        // Panic recovery → 500 instead of crash
r.Use(corsMiddleware)              // CORS policy
r.Use(rateLimitMiddleware)         // Global + per-endpoint rate limiting
r.Use(middleware.Timeout(30s))     // Request timeout (streaming endpoints exempt)

// Protected routes
r.Group(func(r chi.Router) {
    r.Use(authMiddleware)          // JWT validation
    // ... all authenticated endpoints
})
```

### CORS Policy

```go
cors.Options{
    AllowedOrigins:   configuredOrigins,  // Admin configures allowed origins
    AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "PATCH"},
    AllowedHeaders:   []string{"Authorization", "Content-Type"},
    ExposedHeaders:   []string{"X-Request-ID"},
    AllowCredentials: true,
    MaxAge:           86400,  // Preflight cache: 24h
}
```

Default: `AllowedOrigins: ["*"]` for LAN setups. Admin should restrict for public-facing servers.

### Rate Limiting

| Endpoint | Limit | Window | Why |
|----------|-------|--------|-----|
| `POST /auth/login` | 5 req | 15 min (per IP+username) | Brute force protection |
| `POST /auth/quickconnect/*` | 10 req | 1 min (per IP) | PIN brute force |
| `POST /users` (register) | 3 req | 1 hour (per IP) | Spam prevention |
| Global (authenticated) | 100 req | 1 min (per user) | General abuse prevention |
| Streaming endpoints | No limit | — | Would break playback |
| Image/thumbnail endpoints | 200 req | 1 min (per IP) | Crawler protection |

Implementation: in-memory token bucket (Go `rate.Limiter`). No Redis needed for single-instance.

### Input Validation

All user input validated at the handler layer before reaching services:

```go
// Validation rules
- Usernames: 3-32 chars, alphanumeric + underscore, no SQL special chars
- Passwords: 8-128 chars, no restrictions on character type (length > complexity)
- Library names: 1-100 chars, trimmed
- Paths: must be absolute, resolved with filepath.Clean(), no symlink traversal outside allowed dirs
- UUIDs: parsed with google/uuid, rejected if malformed
- Pagination: page ≥ 1, limit 1-100 (default 20)
- Sort fields: whitelist only (no raw SQL column injection)
- Search queries: passed to FTS5 parameterized (never interpolated into SQL)
```

### SQL Injection Prevention
- **All queries use parameterized statements** (`?` placeholders, never string concatenation)
- FTS5 queries: user input passed as parameters to `MATCH`, not interpolated
- Sort/order: column whitelist validated in Go, not from user input

### Path Traversal Prevention
- Library paths: admin-configured, validated at creation
- Media file access: path must be within a configured library path
- Subtitle/image files: resolved relative to media file directory
- Plugin paths: validated against plugin install directory, no symlinks outside
- `filepath.Clean()` + prefix check on every file operation

---

## 4. HTTPS / TLS

HubPlay **does not terminate TLS itself** (by design). Self-hosted pattern:

### Recommended: Reverse Proxy

```
Internet → Caddy/nginx (TLS + certs) → localhost:8096 (HubPlay)
```

**Caddy** (recommended for self-hosters — auto HTTPS with Let's Encrypt):
```caddyfile
hubplay.example.com {
    reverse_proxy localhost:8096
}
```

**nginx:**
```nginx
server {
    listen 443 ssl http2;
    server_name hubplay.example.com;

    ssl_certificate     /etc/letsencrypt/live/hubplay.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/hubplay.example.com/privkey.pem;

    # WebSocket support
    location /api/v1/ws {
        proxy_pass http://localhost:8096;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }

    # Streaming — disable buffering for HLS
    location /api/v1/stream/ {
        proxy_pass http://localhost:8096;
        proxy_buffering off;
        proxy_request_buffering off;
    }

    location / {
        proxy_pass http://localhost:8096;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

### LAN-Only Deployment
- No TLS needed for `localhost` or LAN-only access behind firewall
- HubPlay warns in logs if accessed over HTTP from non-private IP
- Config flag: `server.warn_no_tls: true` (default)

### Why Not Built-in TLS?
- Reverse proxies handle certs, renewal, HTTP/2, and security headers better
- Self-hosters often already have Caddy/nginx/Traefik for other services
- Keeps HubPlay binary simple — one responsibility

---

## 5. Federation Security

Covered in detail in [federation.md](federation.md). Summary:

- **Ed25519 key pairs** generated per server instance
- **Signed JWTs** (5-minute expiry) for every server-to-server request
- **Admin-controlled permissions** per peer: browse, stream, download
- **No transitive trust**: if A trusts B and B trusts C, A does NOT trust C
- **Catalog caching**: peer data stored locally, admin can wipe at any time
- **Bandwidth limits**: `max_bandwidth_mbps` and `max_concurrent_streams` per peer
- **Enforcement**: hard limit — stream refused if peer exceeds concurrent limit; bandwidth throttled at the Go `io.Reader` level

---

## 6. Plugin Security

Covered in detail in [plugins.md](plugins.md). Summary:

- Plugins run as **child processes** (same OS user as HubPlay)
- Communication via **Unix domain sockets** (not network-exposed)
- Plugin directory validated: no symlinks outside `plugins/` directory
- Plugins can only access data through gRPC API — no direct DB access
- **Crash handling**: auto-restart with exponential backoff (1s → 2s → 4s → max 30s). After 5 consecutive failures, plugin is disabled and admin is notified
- Webhooks: admin-configured URLs only. Outbound requests only (no SSRF risk since admin controls config)
- **Future**: cgroups-based resource limits (memory/CPU caps per plugin)

---

## 7. Frontend Security

### XSS Prevention
- React's JSX auto-escapes all rendered values by default
- `dangerouslySetInnerHTML` never used for user-provided content
- Subtitle rendering: ASS/SSA rendered on Canvas (SubtitlesOctopus), not injected into DOM
- Metadata from external APIs (TMDb): sanitized before rendering
- CSP header (via reverse proxy): `default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'` (Tailwind needs inline styles)

### Token Storage
- Access token: in-memory only (Zustand store, lost on page reload)
- Refresh token: `httpOnly` cookie (preferred) or `localStorage` with `Secure` + `SameSite=Strict` flags
- On page load: silent refresh using refresh token → new access token
- On logout: refresh token revoked server-side + cleared client-side

### Content Security
- Media URLs include session-scoped tokens (not user's JWT)
- Stream session tokens: short-lived (1 hour), tied to specific media item + user
- Image/thumbnail URLs: no auth required (public within the server, no sensitive data)

---

## 8. Data Protection

### What's Stored
- User credentials (bcrypt hashed passwords)
- Watch history, favorites, preferences (per-user)
- Media file paths and metadata
- Session tokens (hashed)
- Federation peer keys and cached catalogs

### What's NOT Stored
- Plaintext passwords (ever)
- External service credentials in DB (TMDb API key is in config file only)
- User analytics or telemetry (no phone-home, no tracking)

### Database Security
- SQLite: file permissions `0600` (owner read/write only)
- PostgreSQL: connection via local socket or TLS, credentials in config file
- Config file: should have `0600` permissions (warned at startup if not)
- Backups: admin responsibility. Recommend periodic `sqlite3 .backup` or pg_dump

### Sensitive Data in Logs
- Passwords: NEVER logged
- JWT tokens: NEVER logged (only `[REDACTED]`)
- User IPs: logged for security audit (configurable: `logging.log_ips: true/false`)
- Media paths: logged (for debugging scanner issues)
- Request IDs: always logged (for tracing)

---

## 9. Security Configuration

```yaml
# hubplay.yaml — security-related config

server:
  bind: "0.0.0.0"                    # Listen address (use 127.0.0.1 if behind reverse proxy)
  port: 8096
  warn_no_tls: true                  # Warn if accessed over HTTP from public IP
  trusted_proxies:                   # IPs allowed to set X-Forwarded-For
    - "127.0.0.1"
    - "172.16.0.0/12"

auth:
  jwt_secret: ""                     # Auto-generated on first run. Don't change after setup
  bcrypt_cost: 12                    # Password hashing cost (10-14 recommended)
  access_token_ttl: "15m"
  refresh_token_ttl: "720h"          # 30 days
  max_sessions_per_user: 10          # 0 = unlimited
  registration_enabled: false        # Default: admin creates users manually

rate_limit:
  enabled: true
  login_attempts: 5                  # Per 15-min window per IP+username
  global_rpm: 100                    # Requests per minute per authenticated user

cors:
  allowed_origins:                   # Empty = allow all (LAN default)
    # - "https://hubplay.example.com"

logging:
  log_ips: true                      # Log client IPs for security audit
  log_level: "info"                  # debug/info/warn/error
```

---

## 10. Security Checklist for Self-Hosters

### Minimum (LAN only)
- [ ] Change default admin password after first login
- [ ] Keep `registration_enabled: false` — create users manually

### Recommended (exposed to internet)
- [ ] Reverse proxy with TLS (Caddy for auto-HTTPS)
- [ ] Set `server.bind: 127.0.0.1` (only accept connections from reverse proxy)
- [ ] Configure `trusted_proxies` to your reverse proxy IP
- [ ] Set specific `cors.allowed_origins`
- [ ] Enable firewall: only expose 443 (HTTPS)
- [ ] Regular backups of `hubplay.db` and `hubplay.yaml`
- [ ] Monitor logs for failed login attempts

### Optional Hardening
- [ ] Run HubPlay as dedicated non-root user
- [ ] Use Docker with read-only root filesystem + named volumes
- [ ] Restrict plugin installation (don't install untrusted plugins on public servers)
- [ ] Set `max_sessions_per_user` to a reasonable number (e.g., 5-10)
- [ ] Use PostgreSQL for better concurrent access control on busy servers
