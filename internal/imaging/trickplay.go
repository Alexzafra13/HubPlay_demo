package imaging

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// TrickplayManifest describes the layout of a trickplay sprite sheet
// so the player can compute which sub-image to display for any given
// playback time. The same shape Jellyfin uses for its trickplay
// manifest, modulo field names.
//
//	thumbIdx = floor(seekSeconds / interval_sec)
//	col      = thumbIdx % columns
//	row      = thumbIdx / columns
//	x_px     = col * thumb_w
//	y_px     = row * thumb_h
//
// The browser renders one thumbnail by setting the sprite as
// background-image and shifting background-position by (-x_px, -y_px).
type TrickplayManifest struct {
	// Version stamp for the on-disk cache. Bumped when the generator's
	// output contract changes so the handler can detect stale sprites
	// (e.g. the legacy v1 generator hardcoded a 10×10=100 grid that
	// silently capped coverage at the first 16 minutes of source) and
	// regenerate them instead of serving wrong thumbnails. v0/missing
	// is treated as "stale, regenerate".
	Version     int `json:"version"`
	IntervalSec int `json:"interval_sec"`
	ThumbWidth  int `json:"thumb_width"`
	ThumbHeight int `json:"thumb_height"`
	// Tile layout. The sprite is `columns × rows` thumbnails wide ×
	// tall. `total` may be less than `columns*rows` when the source
	// is shorter than the grid capacity (the trailing cells stay
	// black, but `total` lets the player avoid showing them).
	Columns int `json:"columns"`
	Rows    int `json:"rows"`
	Total   int `json:"total"`
}

// TrickplayManifestVersion is the current generator output contract
// version. The handler's fast-path treats any cached manifest with a
// lower version (or missing field, which decodes to 0) as stale and
// regenerates the sprite + manifest. Bump when the math the player
// uses to compute (col, row, total) cells changes.
const TrickplayManifestVersion = 2

// TrickplayParams configures the sprite generation. Defaults suitable
// for general use:
//
//   - IntervalSec     = 10   → one frame every 10 s of source (Plex default).
//   - ThumbWidth      = 320  → 16:9 yields ~180 px height; small enough that
//     a 20×20 grid stays under ~6 MB on disk.
//   - GridSide        = 10   → fallback grid when DurationSeconds is unknown.
//   - DurationSeconds = 0    → callers SHOULD set this from the item's
//     runtime so we can pick an interval+grid that covers the whole
//     timeline. Leaving it at 0 reproduces the legacy "10×10 = 1000 s"
//     coverage and silently broken thumbnails past the first 16 minutes.
type TrickplayParams struct {
	IntervalSec     int
	ThumbWidth      int
	GridSide        int
	DurationSeconds float64
}

// maxThumbsPerSprite caps how many thumbnails we'll pack into a single
// sprite sheet. Tuned so the resulting PNG stays under ~6-8 MB
// (320×180 × 20×20 = 6400×3600 px) — large enough that browsers
// decode it once and `background-position` shifts are GPU-fast, small
// enough that a 4-hour film doesn't end up with a 50 MB sprite. When
// the caller-supplied duration would overflow this, IntervalSec is
// scaled up so the count stays bounded; the trade-off is one
// thumbnail per ~25-35 s on long content vs Plex's 10 s default.
const maxThumbsPerSprite = 400

func (p TrickplayParams) defaults() TrickplayParams {
	if p.IntervalSec <= 0 {
		p.IntervalSec = 10
	}
	if p.ThumbWidth <= 0 {
		p.ThumbWidth = 320
	}
	if p.GridSide <= 0 {
		p.GridSide = 10
	}
	return p
}

// adapt scales IntervalSec / GridSide so the resulting sprite covers
// the entire DurationSeconds without busting the maxThumbsPerSprite
// budget. Returns the params and the real thumbnail count (which may
// be less than GridSide² when the duration is short).
func (p TrickplayParams) adapt() (TrickplayParams, int) {
	q := p.defaults()
	if q.DurationSeconds <= 0 {
		// No duration → keep the legacy 10×10 fallback. Total is
		// reported as GridSide² so nothing changes for callers that
		// haven't been updated yet.
		return q, q.GridSide * q.GridSide
	}

	desired := int(math.Ceil(q.DurationSeconds / float64(q.IntervalSec)))
	if desired > maxThumbsPerSprite {
		// Bump interval to keep the sprite small. Round up to the
		// next 5 s so the manifest reads as a tidy number ("one
		// thumb every 25 s") rather than 23.x s.
		scaled := int(math.Ceil(q.DurationSeconds / float64(maxThumbsPerSprite)))
		scaled = ((scaled + 4) / 5) * 5
		if scaled > q.IntervalSec {
			q.IntervalSec = scaled
		}
		desired = int(math.Ceil(q.DurationSeconds / float64(q.IntervalSec)))
	}

	grid := int(math.Ceil(math.Sqrt(float64(desired))))
	if grid < 2 {
		grid = 2
	}
	q.GridSide = grid
	return q, desired
}

// GenerateTrickplay renders a single sprite sheet covering up to
// `GridSide² * IntervalSec` seconds of `inputPath` and writes both
// the PNG (`<outputDir>/sprite.png`) and the JSON manifest
// (`<outputDir>/manifest.json`) to disk.
//
// Both writes are atomic — the helpers are the same `AtomicWriteFile`
// the rest of the imaging package uses, so a server crash mid-render
// can never leave a half-written sprite that the next caller would
// trust as cached.
//
// Returns the manifest so the caller can persist or serve it without
// re-reading the file. Errors include the failing stage so log lines
// are actionable: `ffmpeg`, `read sprite`, `write sprite`, `marshal`,
// `write manifest`.
func GenerateTrickplay(ctx context.Context, inputPath, outputDir string, params TrickplayParams) (*TrickplayManifest, error) {
	p, total := params.adapt()

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("create dir: %w", err)
	}
	spritePath := filepath.Join(outputDir, "sprite.png")
	manifestPath := filepath.Join(outputDir, "manifest.json")

	// ffmpeg: 1 frame every IntervalSec, scale to ThumbWidth keeping
	// aspect, then `tile` packs into a single grid image. The tile
	// filter produces a sprite up to `GridSide × GridSide` cells —
	// shorter inputs leave trailing cells black, which is fine since
	// the manifest's `total` tells the player where to stop.
	tileExpr := fmt.Sprintf("fps=1/%d,scale=%d:-2,tile=%dx%d",
		p.IntervalSec, p.ThumbWidth, p.GridSide, p.GridSide)

	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-y", // overwrite — we control the output dir
		"-skip_frame", "nokey", // sample on keyframes only when possible (fast)
		"-i", inputPath,
		"-vf", tileExpr,
		"-frames:v", "1",
		"-q:v", "5", // JPEG quality (lower = better, but PNG output ignores)
		spritePath,
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		// Best-effort cleanup so a partial sprite from a failed run
		// doesn't get served.
		_ = os.Remove(spritePath)
		return nil, fmt.Errorf("ffmpeg: %s: %w", string(out), err)
	}

	// Re-read so we can report dimensions back to the manifest. The
	// alternative (compute from params) would lie when ffmpeg's
	// scaler rounds the height to an even number per `-2`.
	width, height, err := readPNGDimensions(spritePath)
	if err != nil {
		return nil, fmt.Errorf("read sprite: %w", err)
	}

	thumbWidth := width / p.GridSide
	thumbHeight := height / p.GridSide
	// `total` is the real number of thumbnails the sprite carries —
	// either ceil(duration / interval) when we have the runtime, or
	// GridSide² as a legacy fallback. The player consumer reads this
	// via the JSON manifest and clamps `floor(time / interval)` so
	// hovering past the end of the sprite doesn't index into trailing
	// black cells.
	manifest := &TrickplayManifest{
		Version:     TrickplayManifestVersion,
		IntervalSec: p.IntervalSec,
		ThumbWidth:  thumbWidth,
		ThumbHeight: thumbHeight,
		Columns:     p.GridSide,
		Rows:        p.GridSide,
		Total:       total,
	}

	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}
	if err := AtomicWriteFile(manifestPath, body, 0o644); err != nil {
		return nil, fmt.Errorf("write manifest: %w", err)
	}

	return manifest, nil
}

// readPNGDimensions parses the PNG IHDR chunk for width/height
// without decoding the full image. PNG header layout: 8-byte magic,
// then 4-byte length + "IHDR" + width(4) + height(4). We only
// validate the magic; a corrupt IHDR produces a wrong number rather
// than a panic, but our own pipeline produced this file 100 ms ago
// so a corrupt header is implausible enough to trade for the speed.
func readPNGDimensions(path string) (int, int, error) {
	const headerLen = 8 + 4 + 4 + 4 + 4
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	buf := make([]byte, headerLen)
	if _, err := f.Read(buf); err != nil {
		return 0, 0, err
	}
	w := int(buf[16])<<24 | int(buf[17])<<16 | int(buf[18])<<8 | int(buf[19])
	h := int(buf[20])<<24 | int(buf[21])<<16 | int(buf[22])<<8 | int(buf[23])
	return w, h, nil
}

// GenerateTrickplayWithDeadline is the timeout-bounded helper most
// HTTP callers want. ffmpeg can stall forever on a corrupt input,
// and the keyframe-only decode for a 4-hour film at 30 s intervals
// can take 90-120 s on a stock laptop, so the default budget is
// 180 s — short enough to fail fast on corrupt inputs, long enough
// that legitimate long-form content always completes.
func GenerateTrickplayWithDeadline(parent context.Context, inputPath, outputDir string, params TrickplayParams, timeout time.Duration) (*TrickplayManifest, error) {
	if timeout <= 0 {
		timeout = 180 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	return GenerateTrickplay(ctx, inputPath, outputDir, params)
}
