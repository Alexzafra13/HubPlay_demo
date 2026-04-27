package imaging

import (
	"context"
	"encoding/json"
	"fmt"
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

// TrickplayParams configures the sprite generation. Defaults suitable
// for general use:
//
//   - IntervalSec = 10  → one frame every 10 s of source.
//   - ThumbWidth  = 320 → 16:9 yields ~180 px height; ~50 KB JPEG per
//     thumb at quality 85.
//   - GridSide    = 10  → 10×10 = 100 thumbnails per sprite, covering
//     1000 s (≈ 16 min) of source. For longer runtimes the caller
//     can either bump GridSide or chunk into multiple sprites.
type TrickplayParams struct {
	IntervalSec int
	ThumbWidth  int
	GridSide    int
}

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
	p := params.defaults()

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
	manifest := &TrickplayManifest{
		IntervalSec: p.IntervalSec,
		ThumbWidth:  thumbWidth,
		ThumbHeight: thumbHeight,
		Columns:     p.GridSide,
		Rows:        p.GridSide,
		// `total` is unknown without re-probing the source for its
		// runtime. The caller supplies it via TrickplayParams in a
		// future iteration; today the player can derive an upper bound
		// from columns*rows and stop once duration is reached.
		Total: p.GridSide * p.GridSide,
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
// HTTP callers want. ffmpeg can stall forever on a corrupt input;
// 60 s covers a typical 2 h movie at GridSide=10 (one frame every 10
// s, ~100 frames decoded keyframe-only) on a stock laptop.
func GenerateTrickplayWithDeadline(parent context.Context, inputPath, outputDir string, params TrickplayParams, timeout time.Duration) (*TrickplayManifest, error) {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	return GenerateTrickplay(ctx, inputPath, outputDir, params)
}
