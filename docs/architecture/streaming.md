# Streaming Module — Design Document

## Overview

The streaming module delivers media content to clients. It decides the optimal delivery method per client, handles transcoding when necessary, and manages HLS adaptive streaming for remote playback.

---

## 1. Streaming Decision Waterfall

For every playback request, the server evaluates in order:

```
Client requests playback
    │
    ▼
┌─ 1. Direct Play? ──────────────────────────────┐
│  Client supports container + video + audio codec │
│  AND bandwidth is sufficient                     │
│  → Serve file as-is (zero server load)           │
└──────────────┬───────────────────────────────────┘
               │ No
               ▼
┌─ 2. Direct Stream (Remux)? ─────────────────────┐
│  Client supports codecs but NOT container        │
│  → Re-package into compatible container (MP4)    │
│  → Minimal server load                           │
└──────────────┬───────────────────────────────────┘
               │ No
               ▼
┌─ 3. Transcode ─────────────────────────────────┐
│  Client doesn't support codec, resolution,      │
│  or bitrate exceeds bandwidth                   │
│  → FFmpeg re-encodes in real-time               │
│  → Heavy server load (CPU or GPU)               │
└─────────────────────────────────────────────────┘
```

### Decision Logic
```go
type PlaybackDecision struct {
    Method       PlaybackMethod  // DirectPlay, DirectStream, Transcode
    VideoCodec   string          // Target video codec (or "copy")
    AudioCodec   string          // Target audio codec (or "copy")
    Container    string          // Target container (mp4, ts)
    MaxBitrate   int64           // Target bitrate limit
    Resolution   Resolution      // Target resolution (if downscaling)
    SubtitleMode SubtitleMode    // Embed, burn-in, or external delivery
}

type PlaybackMethod string
const (
    DirectPlay   PlaybackMethod = "direct_play"
    DirectStream PlaybackMethod = "direct_stream"
    Transcode    PlaybackMethod = "transcode"
)

func DecidePlayback(item *MediaItem, profile *ClientProfile) *PlaybackDecision
```

---

## 2. Client Profiles

Each client registers its capabilities. The server stores common profiles.

```go
type ClientProfile struct {
    ID               string
    Name             string          // "Chrome", "Samsung TV 2023", "iOS Safari"
    SupportedCodecs  CodecSupport
    MaxResolution    Resolution
    MaxBitrate       int64           // bps
    SupportsHLS      bool
    SupportsDASH     bool
    SupportsDirectPlay bool
}

type CodecSupport struct {
    VideoCodecs    []string  // ["h264", "hevc", "vp9", "av1"]
    AudioCodecs    []string  // ["aac", "ac3", "eac3", "opus", "flac"]
    Containers     []string  // ["mp4", "mkv", "webm", "ts"]
    SubtitleFormats []string // ["srt", "vtt", "ass", "pgs"]
}

type Resolution struct {
    Width  int
    Height int
}
```

### Built-in Profiles

| Profile | Video | Audio | Container | Max Res |
|---------|-------|-------|-----------|---------|
| Web (Chrome/Firefox) | H.264, VP9, AV1 | AAC, Opus | MP4, WebM | 4K |
| iOS Safari | H.264, HEVC | AAC, AC3 | MP4, TS | 4K |
| Android | H.264, HEVC, VP9 | AAC, Opus | MP4, WebM | 4K |
| Smart TV (generic) | H.264, HEVC | AAC, AC3, EAC3 | MP4, TS | 4K |
| Chromecast | H.264, VP9, HEVC | AAC, Opus, AC3 | MP4, WebM | 4K |

Clients can also self-report capabilities dynamically during session setup.

---

## 3. Transcoding Engine (FFmpeg)

### Architecture
```
┌─────────────────────────────────────────────┐
│           TranscodingManager                 │
│  (Session lifecycle, resource management)    │
├──────────┬──────────────┬───────────────────┤
│ Session  │  FFmpeg      │  Hardware         │
│ Tracker  │  Command     │  Accelerator      │
│          │  Builder     │  Detector         │
└──────────┴──────────────┴───────────────────┘
```

### Transcoding Session
```go
type TranscodeSession struct {
    ID           string
    ItemID       uuid.UUID
    UserID       uuid.UUID
    Process      *os.Process
    OutputDir    string          // Temp directory for HLS segments
    StartedAt    time.Time
    LastAccessed time.Time       // For idle cleanup
    Decision     *PlaybackDecision
    Progress     float64         // 0.0 - 1.0
}

type TranscodingManager interface {
    // Start or resume a transcoding session
    GetOrCreateSession(ctx context.Context, req TranscodeRequest) (*TranscodeSession, error)

    // Get a specific HLS segment (blocks until ready)
    GetSegment(ctx context.Context, sessionID string, segmentIndex int) (io.ReadCloser, error)

    // Seek: kill current session, start new one from position
    SeekTo(ctx context.Context, sessionID string, position time.Duration) error

    // Cleanup idle sessions
    CleanupIdle(maxIdle time.Duration)

    // Kill all sessions (shutdown)
    StopAll()
}
```

### FFmpeg Command Builder
Constructs FFmpeg commands based on the playback decision:

```go
type FFmpegBuilder interface {
    // Build command for HLS output
    BuildHLS(input string, decision *PlaybackDecision, startTime time.Duration) *exec.Cmd

    // Build command for progressive download (direct stream/remux)
    BuildRemux(input string, decision *PlaybackDecision) *exec.Cmd

    // Build command for thumbnail extraction
    BuildThumbnail(input string, timestamp time.Duration, width int) *exec.Cmd
}
```

**Example generated commands:**

Direct Stream (remux MKV → MP4):
```bash
ffmpeg -i input.mkv -c:v copy -c:a copy -movflags +faststart -f mp4 pipe:1
```

Transcode (HEVC 4K → H.264 1080p):
```bash
ffmpeg -i input.mkv -c:v libx264 -preset veryfast -crf 23 \
  -vf "scale=1920:1080" -c:a aac -b:a 192k \
  -f hls -hls_time 6 -hls_list_size 0 -hls_segment_type mpegts \
  output/stream.m3u8
```

Transcode with hardware acceleration (VAAPI):
```bash
ffmpeg -hwaccel vaapi -hwaccel_output_format vaapi \
  -i input.mkv -c:v h264_vaapi -qp 23 \
  -vf "scale_vaapi=w=1920:h=1080" -c:a aac -b:a 192k \
  -f hls -hls_time 6 output/stream.m3u8
```

### Hardware Acceleration Detection
```go
type HWAccelerator interface {
    // Detect available GPU capabilities at startup
    Detect(ctx context.Context) (*HWCapabilities, error)

    // Get FFmpeg flags for the best available encoder
    EncoderFlags(codec string, caps *HWCapabilities) []string
}

type HWCapabilities struct {
    VAAPI      bool    // Linux AMD/Intel
    QSV        bool    // Intel Quick Sync
    NVENC      bool    // NVIDIA
    VideoToolbox bool  // macOS
    Encoders   []string // Available hardware encoders
    Decoders   []string // Available hardware decoders
}
```

Detection runs once at startup:
1. Run `ffmpeg -hwaccels` to list available APIs
2. For each, try a test encode to verify it actually works
3. Cache results — used by FFmpegBuilder when constructing commands

---

## 4. HLS Adaptive Streaming

For remote/transcoded playback, we serve HLS (HTTP Live Streaming).

### Endpoints
```
GET /api/v1/stream/{itemId}/master.m3u8       → Master playlist (multi-quality)
GET /api/v1/stream/{itemId}/{quality}/index.m3u8  → Quality-specific playlist
GET /api/v1/stream/{itemId}/{quality}/{segment}.ts → Individual segment
GET /api/v1/stream/{itemId}/direct              → Direct play (progressive download)
GET /api/v1/stream/{itemId}/subtitles/{id}.vtt  → Subtitle track
```

### Master Playlist (Adaptive Bitrate)
The master playlist offers multiple quality levels. The client (hls.js) picks the best one based on bandwidth.

```m3u8
#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=8000000,RESOLUTION=3840x2160
/api/v1/stream/{id}/2160p/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=4000000,RESOLUTION=1920x1080
/api/v1/stream/{id}/1080p/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=2000000,RESOLUTION=1280x720
/api/v1/stream/{id}/720p/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=800000,RESOLUTION=854x480
/api/v1/stream/{id}/480p/index.m3u8
```

### Segment Management
- Segment duration: **6 seconds** (standard HLS, good balance between latency and efficiency)
- Format: MPEG-TS (`.ts`) — universal compatibility
- Segments are generated on-demand: FFmpeg starts transcoding and writes segments to a temp dir
- When client requests segment N, the server either serves from cache or waits for FFmpeg to produce it
- **Idle cleanup**: sessions with no segment requests for 5 minutes are killed

### Seek Handling
When user seeks to a new position:
1. Check if the target position has already been transcoded (segment exists)
2. If yes → serve cached segment
3. If no → kill current FFmpeg process, start new one from the seek position using `-ss` flag

---

## 5. Subtitles

### Delivery Modes
| Subtitle Type | Delivery | Notes |
|---------------|----------|-------|
| SRT, VTT | External (sidecar) | Client fetches and renders — best quality |
| ASS/SSA | Burn-in during transcode | Complex styling can't be rendered client-side |
| PGS (Blu-ray) | Burn-in during transcode | Bitmap-based, must be burned in |
| Embedded SRT | Extract and serve as VTT | FFmpeg extracts, we convert to WebVTT |

### Subtitle Extraction
```go
type SubtitleService interface {
    // List available subtitles for an item
    ListSubtitles(ctx context.Context, itemID uuid.UUID) ([]SubtitleTrack, error)

    // Extract and convert a subtitle track to WebVTT
    ExtractToVTT(ctx context.Context, itemID uuid.UUID, streamIndex int) (io.ReadCloser, error)
}

type SubtitleTrack struct {
    Index    int
    Language string
    Title    string
    Format   string   // srt, vtt, ass, pgs
    IsForced bool
    IsExternal bool   // From sidecar file vs embedded
}
```

---

## 6. Trickplay (Preview Thumbnails)

Timeline preview thumbnails — when user hovers over the progress bar, they see a thumbnail of that moment.

### Generation
- Triggered by `item.added` event (background task)
- FFmpeg extracts one frame every N seconds (configurable, default 10s)
- Frames are combined into sprite sheets (grid of thumbnails in a single image)
- A BIF or WebVTT file maps timestamps to sprite positions

### Configuration
```go
type TrickplayConfig struct {
    Enabled   bool
    Interval  time.Duration  // One frame every 10s
    Width     int            // Thumbnail width (160px default)
    Columns   int            // Sprites per row in sheet (10)
    MaxWorkers int           // Concurrent generation jobs
}
```

Stored in `~/.hubplay/cache/trickplay/{itemId}/` — never in media directories.

---

## 7. Resource Management

### Concurrent Transcoding Limits
```go
type ResourceLimits struct {
    MaxTranscodeSessions int   // Default: 2 (adjustable based on CPU/GPU)
    MaxBandwidthMbps     int   // Total outbound bandwidth limit
    TranscodeThrottle    bool  // Limit transcode speed to 1.5x playback speed
}
```

- When all slots are full, new transcode requests queue with a timeout
- Direct Play has no limit (it's just file serving)
- Admin can configure limits based on their hardware

### Temp File Cleanup
- HLS segments stored in `~/.hubplay/cache/transcode/{sessionId}/`
- Cleanup triggers:
  - Session idle > 5 minutes → kill FFmpeg + delete segments
  - Server startup → clean all stale transcode dirs
  - Cache size exceeds limit → delete oldest sessions first

---

## 8. Directory Structure

```
internal/
├── streaming/
│   ├── manager.go          # TranscodingManager implementation
│   ├── session.go          # TranscodeSession lifecycle
│   ├── decision.go         # Playback decision logic
│   ├── profiles.go         # Built-in client profiles
│   ├── hls.go              # HLS playlist generation + segment serving
│   ├── direct.go           # Direct play / progressive download
│   └── subtitle.go         # Subtitle extraction + conversion
├── ffmpeg/
│   ├── builder.go          # FFmpeg command construction
│   ├── hwaccel.go          # Hardware acceleration detection
│   ├── process.go          # FFmpeg process wrapper + lifecycle
│   └── probe.go            # FFprobe media analysis
└── trickplay/
    ├── generator.go        # Background trickplay generation
    └── sprites.go          # Sprite sheet creation
```

---

## 9. Configuration

```yaml
# hubplay.yaml (streaming section)
streaming:
  segment_duration: 6        # HLS segment length in seconds
  max_transcode_sessions: 2  # Concurrent transcodes
  transcode_preset: veryfast # FFmpeg preset
  default_audio_bitrate: 192k

  hardware_acceleration:
    enabled: true            # Auto-detect and use GPU if available
    preferred: auto          # auto | vaapi | qsv | nvenc | videotoolbox

  trickplay:
    enabled: true
    interval: 10s
    width: 160

  bandwidth_limit: 0         # 0 = unlimited, otherwise Mbps
```
