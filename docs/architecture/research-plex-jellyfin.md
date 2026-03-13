# Research: Plex & Jellyfin Architecture Analysis

## Purpose
Competitive analysis to inform HubPlay's architecture decisions. Extracted patterns to adopt and anti-patterns to avoid.

---

## Plex Architecture Summary

### What They Do Well
- **Scanner/Agent separation**: Scanners find files, Agents fetch metadata — clean SRP
- **Optimized media analysis**: Uses FFprobe for codec/resolution detection, caches results
- **Transcoding profiles**: Per-client capability profiles determine streaming method
- **Direct Play priority**: Always tries direct play first, then direct stream, then transcode
- **Automatic remote access**: Built-in relay/tunnel — zero-config for users

### What to Avoid
- **Closed source core**: Can't extend without reverse-engineering. We'll be open
- **Monolithic Python server**: Hard to scale individual components
- **Plex Pass paywalls**: Core features locked behind subscription
- **Phone-home authentication**: Requires internet for local server access
- **Plugin system removed**: Originally had plugins, then removed them — bad for ecosystem

---

## Jellyfin Architecture Summary

### Codebase Structure
- Multi-project .NET solution (~20 projects), descended from Emby 3.5.2
- Layered: `MediaBrowser.Model` (DTOs) → `MediaBrowser.Controller` (interfaces) → `Emby.Server.Implementations` / `Jellyfin.Server.Implementations`
- 67 ASP.NET Core API controllers
- SQLite default, experimental PostgreSQL via `IJellyfinDatabaseProvider` abstraction

### What They Do Well

**Library Scanning (Resolver Pipeline)**
- `IItemResolver` implementations registered with priority ordering
- First resolver returning non-null `BaseItem` wins
- `IMultiItemResolver` handles multi-file movies/split episodes
- `LibraryMonitor` pauses file watching during scans to prevent races
- Items cached in `FastConcurrentLru<Guid, BaseItem>`

**Metadata (Provider Chain)**
- `ILocalMetadataProvider` → `IRemoteMetadataProvider` → `ICustomMetadataProvider`
- Separate image provider hierarchy: `IImageProvider` / `IRemoteImageProvider` / `IDynamicImageProvider`
- `IProviderManager` orchestrates with priority queuing and configurable `MetadataRefreshMode`
- Type-specific info classes: `MovieInfo`, `SeriesInfo`, `EpisodeInfo`, etc.
- Supports NFO (Kodi), XML (native), and embedded tags

**Transcoding**
- FFmpeg/FFprobe wrapped with process lifecycle tracking
- Runtime GPU detection: VAAPI, VideoToolbox, QSV, RKMPP, Vulkan
- Per-encoder quality scale translation
- Concurrent encoding limited by semaphore based on CPU count
- `veryfast` preset for VOD, `superfast` for live

**Plugin System**
- `PluginLoadContext` per plugin — assembly isolation, clean unloading
- `meta.json` manifest with TargetAbi compatibility checking
- Path traversal protection on DLL loading
- Lifecycle states: Active → Disabled → Superseded → Deleted → Malfunctioned
- Extension points: resolvers, metadata providers, image providers, auth providers, notifications

**Streaming (HLS/Direct Play)**
- Adaptive bitrate master playlists
- On-demand segment transcoding with caching
- Gap detection: restarts transcoding if segment gap too large
- Container support: MPEG-TS and fMP4
- `QuickConnect` PIN-based pairing for TVs

**Other Good Patterns**
- `--nowebclient` flag for API-only mode
- EF Core with database provider abstraction
- Decoupled web client (separate repo, independent dev)

### Anti-Patterns to Avoid

| Problem | Jellyfin File | Size | Our Solution |
|---------|--------------|------|-------------|
| God-class composition root | `ApplicationHost.cs` | 39KB | Clean `main.go` that only wires modules |
| Monolithic persistence | `BaseItemRepository.cs` | 106KB | Separate repository per domain |
| Controller with business logic | `DynamicHlsController` | 115KB | Thin handlers + service layer |
| Dual namespace confusion | `MediaBrowser.*` + `Jellyfin.*` | — | Single `hubplay/` namespace from day 1 |
| Generated files in media dirs | Thumbnails mixed with source | — | Dedicated cache directory |
| Client-side dataset filtering | Large library performance issues | — | Server-side pagination/filtering |
| Manual remote access | No relay/tunnel | — | Built-in tunnel option (stretch goal) |

---

## Key Decisions for HubPlay

### Adopt from Jellyfin
1. **Resolver pipeline with priority** for library scanning
2. **Provider chain** (local → remote → custom) for metadata
3. **Plugin isolation** via separate process or Go plugin boundaries
4. **Database provider abstraction** — SQLite default, PostgreSQL optional
5. **QuickConnect-style** PIN pairing for limited-input devices
6. **Hardware acceleration detection** at startup

### Adopt from Plex
1. **Scanner/Agent separation** as clean SRP boundary
2. **Client transcoding profiles** for per-device optimization
3. **Direct Play → Direct Stream → Transcode** decision waterfall

### Improve Upon Both
1. **Go microservice-friendly architecture** — can split later without rewrite
2. **gRPC internal communication** between modules (not just HTTP)
3. **Event-driven scanning** with debounced file watchers
4. **Separated cache** — all generated content in `~/.hubplay/cache/`, never in media dirs
5. **Open plugin ecosystem** from day 1, not an afterthought
