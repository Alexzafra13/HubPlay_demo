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

// ComputeBlurhash calcula la cadena blurhash de una imagen ya cargada en
// memoria (no toca disco).
//
// Acepta JPEG, PNG y WebP. WebP hace falta para los logos de Fanart —
// si se quita ese decoder, devolvería cadena vacía silenciosamente y el
// frontend pintaría el placeholder gris en su lugar.
//
// Devuelve cadena vacía para formatos no soportados (GIF animado, AVIF,
// BMP, etc.). Se admite un logger nulo.
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
