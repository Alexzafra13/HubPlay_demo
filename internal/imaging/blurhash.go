package imaging

import (
	"bytes"
	"image"
	_ "image/jpeg" // register JPEG decoder for image.Decode
	_ "image/png"  // register PNG decoder for image.Decode
	"log/slog"

	_ "golang.org/x/image/webp" // register WebP decoder for image.Decode

	"hubplay/internal/blurhash"
)

// ComputeBlurhash decodes raw image bytes and returns a blurhash string.
// Callers pass already-read bytes (e.g. from an upload) — this function only
// touches memory, never disk.
//
// Decoders registered: JPEG, PNG, WebP. Fanart logo assets in particular
// arrive as WebP — without the explicit registration above the std-lib
// image package would refuse them and ComputeBlurhash silently returned
// "" for every Fanart logo, which then made the frontend fall back to
// the grey-tile placeholder instead of a low-frequency preview.
//
// Returns an empty string for genuinely unsupported formats (animated
// GIF, AVIF, BMP, …). A nil logger is tolerated.
func ComputeBlurhash(data []byte, logger *slog.Logger) string {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		if logger != nil {
			logger.Warn("failed to decode image for blurhash", "error", err)
		}
		return ""
	}
	return blurhash.Encode(4, 3, img)
}
