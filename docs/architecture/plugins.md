# Plugin System — Design Document

## Overview

HubPlay supports extensibility via **plugins as external processes** communicating over gRPC, plus **webhooks** for lightweight automation. This approach keeps the core stable while allowing the community to extend functionality in any programming language.

---

## 1. Architecture

```
┌─────────────────────────────────────────────┐
│              HubPlay Core                    │
│                                             │
│  ┌─────────────┐  ┌──────────────────────┐  │
│  │ Plugin      │  │ Webhook              │  │
│  │ Manager     │  │ Dispatcher           │  │
│  └──────┬──────┘  └──────────┬───────────┘  │
└─────────┼────────────────────┼──────────────┘
          │ gRPC               │ HTTP POST
          ▼                    ▼
  ┌───────────────┐    ┌──────────────┐
  │ Plugin A      │    │ External     │
  │ (Go binary)   │    │ Service      │
  ├───────────────┤    │ (Telegram,   │
  │ Plugin B      │    │  Discord,    │
  │ (Python)      │    │  Webhook.site│
  ├───────────────┤    │  etc.)       │
  │ Plugin C      │    └──────────────┘
  │ (Rust)        │
  └───────────────┘
```

---

## 2. gRPC Plugin System

### How It Works
1. Plugins are standalone executables placed in `~/.hubplay/plugins/`
2. Each plugin has a `plugin.yaml` manifest describing what it provides
3. On startup, HubPlay discovers plugins, starts them as child processes
4. Communication via gRPC over Unix sockets (fast, local-only)
5. If a plugin crashes, HubPlay logs the error and continues without it

### Plugin Manifest
```yaml
# ~/.hubplay/plugins/tmdb-anime/plugin.yaml
name: "tmdb-anime"
version: "1.0.0"
description: "Enhanced anime metadata from AniDB + TMDb"
author: "community"
license: "MIT"

# What this plugin provides
provides:
  - type: metadata_provider
    content_types: [movies, tvshows]
    priority: 15

# Binary to execute
executable: "./tmdb-anime"

# Minimum HubPlay version
min_version: "1.0.0"
```

### Plugin Types (Extension Points)

| Type | What It Does | Interface |
|------|-------------|-----------|
| `metadata_provider` | Provides metadata for items | Search + Fetch |
| `image_provider` | Provides artwork/images | FetchImages |
| `auth_provider` | External authentication (LDAP, SSO) | Authenticate + ValidateToken |
| `notification` | Send notifications on events | Notify |
| `subtitle_provider` | Find/download subtitles | Search + Download |
| `resolver` | Custom file naming/organization | CanResolve + Resolve |

### gRPC Service Definitions

```protobuf
// Metadata provider plugin
service MetadataProviderPlugin {
  rpc Search(SearchRequest) returns (SearchResponse);
  rpc Fetch(FetchRequest) returns (FetchResponse);
  rpc GetCapabilities(Empty) returns (Capabilities);
}

// Auth provider plugin
service AuthProviderPlugin {
  rpc Authenticate(AuthRequest) returns (AuthResponse);
  rpc ValidateToken(TokenRequest) returns (TokenResponse);
  rpc GetCapabilities(Empty) returns (Capabilities);
}

// Notification plugin
service NotificationPlugin {
  rpc Notify(NotifyRequest) returns (NotifyResponse);
  rpc GetCapabilities(Empty) returns (Capabilities);
}

// Health check (all plugins must implement)
service PluginHealth {
  rpc Ping(Empty) returns (PongResponse);
}
```

### Plugin Lifecycle

```
Discovery → Validate manifest → Start process → gRPC handshake → Health check
    │
    ├─ Success → Register with appropriate manager (metadata, auth, etc.)
    │
    └─ Failure → Log error, auto-restart with backoff (1s→2s→4s→max 30s)
                  After 5 consecutive failures → disable plugin, notify admin
```

```go
type PluginManager interface {
    // Discover and start all plugins
    LoadAll(ctx context.Context) error

    // List loaded plugins
    List() []PluginInfo

    // Enable/disable a plugin
    SetEnabled(ctx context.Context, name string, enabled bool) error

    // Restart a plugin
    Restart(ctx context.Context, name string) error

    // Install from a plugin repository URL
    Install(ctx context.Context, url string) error

    // Uninstall
    Uninstall(ctx context.Context, name string) error
}

type PluginInfo struct {
    Name        string
    Version     string
    Description string
    Status      PluginStatus  // Running, Stopped, Malfunctioned, Disabled
    Provides    []string      // ["metadata_provider", "image_provider"]
    PID         int           // OS process ID
    StartedAt   time.Time
    Memory      int64         // RSS in bytes
}
```

### Plugin SDK

We provide a Go SDK to make plugin development easy. Other languages use the raw gRPC proto files.

```go
// Example: creating a metadata provider plugin in Go
package main

import "github.com/hubplay/plugin-sdk-go"

type MyProvider struct{}

func (p *MyProvider) Search(ctx context.Context, req *pluginsdk.SearchRequest) (*pluginsdk.SearchResponse, error) {
    // Your search logic here
    return &pluginsdk.SearchResponse{Results: results}, nil
}

func (p *MyProvider) Fetch(ctx context.Context, req *pluginsdk.FetchRequest) (*pluginsdk.FetchResponse, error) {
    // Your fetch logic here
    return &pluginsdk.FetchResponse{Metadata: metadata}, nil
}

func main() {
    pluginsdk.Serve(&MyProvider{})
}
```

---

## 3. Webhooks

For users who just want to trigger actions on events without writing a full plugin.

### Configuration
```yaml
# hubplay.yaml
webhooks:
  - name: "Telegram notification"
    url: "https://api.telegram.org/bot{token}/sendMessage"
    events:
      - item.added
      - scan.completed
    method: POST
    headers:
      Content-Type: "application/json"
    body_template: |
      {"chat_id": "12345", "text": "New: {{.Item.Title}} ({{.Item.Year}})"}

  - name: "Discord notification"
    url: "https://discord.com/api/webhooks/{id}/{token}"
    events:
      - item.added
    method: POST
    body_template: |
      {"content": "Added: {{.Item.Title}}"}
```

### Webhook Dispatcher
```go
type WebhookDispatcher interface {
    // Register a webhook
    Register(config WebhookConfig) error

    // Remove a webhook
    Remove(name string) error

    // List webhooks
    List() []WebhookConfig

    // Test a webhook (send a test payload)
    Test(ctx context.Context, name string) error
}

type WebhookConfig struct {
    Name         string
    URL          string
    Events       []EventType
    Method       string            // POST, PUT
    Headers      map[string]string
    BodyTemplate string            // Go template
    RetryCount   int               // Default 3
    TimeoutSec   int               // Default 10
}
```

### Webhook Delivery
- Events are queued and delivered asynchronously (don't block the main flow)
- Retry on failure: 3 attempts with exponential backoff (1s, 5s, 30s)
- Delivery log stored in DB for debugging (last 100 per webhook)
- Template variables: `{{.Event.Type}}`, `{{.Item.Title}}`, `{{.Item.Year}}`, `{{.Library.Name}}`, etc.

---

## 4. Plugin Repository (Future)

A central JSON index where users can browse and install community plugins:

```json
{
  "plugins": [
    {
      "name": "anidb-metadata",
      "description": "Anime metadata from AniDB",
      "version": "1.2.0",
      "download_url": "https://github.com/user/anidb-metadata/releases/v1.2.0/plugin.tar.gz",
      "checksum": "sha256:abc123...",
      "provides": ["metadata_provider"],
      "min_hubplay_version": "1.0.0"
    }
  ]
}
```

Users can install via API or CLI:
```bash
hubplay plugin install https://repo.hubplay.dev/anidb-metadata
```

Not for v1 — but the architecture supports it from day one.

---

## 5. Security

- Plugins run as child processes with the same user as HubPlay (no privilege escalation)
- Communication via Unix socket (not exposed to network)
- Plugin directory is validated: no symlinks outside allowed paths
- Webhooks: user-provided URLs only, no SSRF mitigation needed (the admin controls the config)
- Plugin resource limits (future): memory/CPU caps via cgroups

---

## 6. Directory Structure

```
internal/
├── plugin/
│   ├── manager.go          # Plugin discovery, lifecycle, health monitoring
│   ├── loader.go           # Process startup, gRPC connection
│   ├── registry.go         # Maps plugin capabilities to core interfaces
│   └── manifest.go         # Plugin manifest parsing + validation
├── webhook/
│   ├── dispatcher.go       # Event → webhook delivery
│   ├── config.go           # Webhook configuration
│   └── template.go         # Body template rendering
└── proto/
    ├── metadata.proto      # Metadata provider plugin interface
    ├── auth.proto           # Auth provider plugin interface
    ├── notification.proto   # Notification plugin interface
    └── health.proto         # Common health check
```
