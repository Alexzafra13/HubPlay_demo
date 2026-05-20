package imaging

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg" // registra el decoder JPEG y exporta jpeg.Encode usado por GenerateAvatar
	_ "image/png" // registra el decoder PNG

	_ "golang.org/x/image/webp" // registra el decoder WebP
)

// GenerateAvatar decodifica una imagen arbitraria (PNG/JPEG/WebP),
// la recorta al cuadrado centrado y la redimensiona a size×size,
// devolviendo JPEG calidad 85.
//
// Pensado para avatares de usuario subidos vía POST /me/avatar:
//   - JPEG quality 85 → ~10-20 KB por avatar a 256px, sin dependencias
//     extra (no usamos webp encoder para no añadir cgo).
//   - Recorte centrado: la mayoría de fotos de perfil están centradas
//     en el sujeto; eso evita decisiones de UX (sin crop UI por ahora).
//   - Reutilizamos el resize nearest-neighbor de thumbnail.go: a 256px
//     la pérdida es invisible y nos ahorra una dependencia.
//
// Decompression-bomb guard: el caller debería llamar EnforceMaxPixels
// sobre los bytes crudos antes de pasarlos aquí, igual que hace el
// resto de imaging/. No lo duplicamos para que el caller no decodifique
// dos veces si quiere otras checks.
func GenerateAvatar(src []byte, size int) ([]byte, error) {
	if size <= 0 {
		return nil, fmt.Errorf("avatar size must be positive")
	}
	img, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, fmt.Errorf("decode avatar: %w", err)
	}

	cropped := centerCropSquare(img)
	resized := nearestNeighborResize(cropped, size, size)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, resized, &jpeg.Options{Quality: 85}); err != nil {
		return nil, fmt.Errorf("encode avatar jpeg: %w", err)
	}
	return buf.Bytes(), nil
}

// centerCropSquare devuelve una sub-imagen cuadrada centrada del
// origen. Si la imagen ya es cuadrada, la devuelve tal cual.
func centerCropSquare(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == h {
		return src
	}
	side := w
	if h < w {
		side = h
	}
	x0 := b.Min.X + (w-side)/2
	y0 := b.Min.Y + (h-side)/2
	rect := image.Rect(x0, y0, x0+side, y0+side)

	// La mayoría de tipos concretos (RGBA, NRGBA, YCbCr, etc.)
	// implementan SubImage. Si no, recurrimos a copiar a un RGBA
	// nuevo — más lento pero seguro.
	type subImager interface {
		SubImage(r image.Rectangle) image.Image
	}
	if si, ok := src.(subImager); ok {
		return si.SubImage(rect)
	}
	dst := image.NewRGBA(image.Rect(0, 0, side, side))
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			dst.Set(x, y, src.At(x0+x, y0+y))
		}
	}
	return dst
}
