package imaging

import (
	"bytes"
	"image"
	_ "image/jpeg" // register JPEG decoder for image.Decode
	_ "image/png"  // register PNG decoder for image.Decode
	"log/slog"

	"hubplay/internal/blurhash"
)

// ComputeBlurhash decodes raw image bytes and returns a blurhash string.
// Callers pass already-read bytes (e.g. from an upload) — this function only
// touches memory, never disk.
//
// Returns an empty string when the decoder cannot understand the image
// (e.g. a WebP payload — the std-lib decoders registered here are JPEG+PNG
// only). A nil logger is tolerated.
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
