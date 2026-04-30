# API documentation

The authoritative HubPlay API spec is **OpenAPI 3.0.3**, embedded into the
binary and served at `GET /api/v1/openapi.yaml`.

## Source of truth

The spec file lives in the Go package that serves it so `//go:embed` can
bundle it at compile time without path gymnastics:

[`internal/api/handlers/openapi.yaml`](../../internal/api/handlers/openapi.yaml)

A spec change ships with the binary it describes — impossible to drift
from a forgotten file mount.

## Consuming the spec

```bash
# From a running server
curl https://hubplay.example.com/api/v1/openapi.yaml -o openapi.yaml

# Generate a Kotlin client (Android TV target)
openapi-generator generate \
    -i openapi.yaml \
    -g kotlin \
    -o ./hubplay-kotlin-client \
    --additional-properties=library=jvm-okhttp4,serializationLibrary=kotlinx_serialization

# Or get JSON if your tooling prefers it
yq -o json openapi.yaml > openapi.json
```

## Scope

The spec covers the **consumer surface** — what a native client (Kotlin
TV, future iOS, integration scripts) needs to read a HubPlay catalogue
and stream from it:

- Auth (login, refresh, logout, RFC 8628 device-code flow)
- Identity (`/me`, `/me/preferences`)
- Browse (libraries, items, search, latest)
- Stream (HLS waterfall + direct play + subtitles + trickplay)
- User data (progress, played, favourites, continue-watching, next-up)
- User-scoped real-time events (SSE)
- People (cast/crew + thumbnails)
- Image serving
- Federation user surface (browse paired peers' shared libraries)
- Health

**Out of scope** (intentional, not "TODO"):

- Admin endpoints (`/admin/*`) — admin UI is browser-only.
- Setup wizard (`/setup/*`) — first-run web flow.
- Peer-to-peer endpoints (`/peer/*`) — server-to-server, authenticated by
  Ed25519 JWTs from a paired peer's keypair, not by user sessions.
  Documented in [`docs/architecture/federation.md`](../architecture/federation.md).
- IPTV (Live TV) — channel browse is in scope; the transmux / EPG
  management surface is admin-shaped and waits for the TV app to need it.

## Versioning

The spec's `info.version` field tracks the project version. The path
prefix is `/api/v1` and won't break compatibility within v1 — new fields
may appear, fields won't be removed or change type.

When v2 happens (no near-term plan), it ships at `/api/v2` alongside v1
during a deprecation window.
