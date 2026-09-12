package imaging

import (
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	xdraw "golang.org/x/image/draw"
)

// ThumbnailQuality es la calidad JPEG de las miniaturas generadas por
// ?w=N. 85 en vez de 80: los backdrops a pantalla completa en TV enseñan
// el blocking de JPEG en degradados (cielos, fundidos a negro) y el coste
// en bytes es ~15 %.
const ThumbnailQuality = 85

// GenerateThumbnail: resize a maxWidth (aspect-ratio preservado, filtro
// Catmull-Rom) y escribe a dstPath. Sólo JPEG y PNG (std-lib + x/image).
//
// Antes se usaba vecino más cercano: un backdrop "original" de TMDb
// (3840 px) reducido a 1280 px salía con aliasing visible ("pixelado")
// en la TV. Catmull-Rom es un resampler bicúbico que promedia los píxeles
// fuente: nítido sin escalera. Cuesta ~100-300 ms por backdrop 4K en un
// x86 modesto, pero se paga una vez por (imagen, ancho) — el handler
// cachea el resultado en disco.
func GenerateThumbnail(srcPath, dstPath string, maxWidth int) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer src.Close() //nolint:errcheck

	img, format, err := image.Decode(src)
	if err != nil {
		return fmt.Errorf("decode image: %w", err)
	}

	bounds := img.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	if srcW <= maxWidth {
		// Imagen ya más pequeña; copiamos tal cual.
		return copyFile(srcPath, dstPath)
	}

	dstW := maxWidth
	dstH := srcH * maxWidth / srcW
	if dstH < 1 {
		dstH = 1
	}

	resized := resample(img, dstW, dstH)

	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return fmt.Errorf("create thumbnail dir: %w", err)
	}

	out, err := os.Create(dstPath)
	if err != nil {
		return fmt.Errorf("create thumbnail file: %w", err)
	}
	defer out.Close() //nolint:errcheck

	switch strings.ToLower(format) {
	case "png":
		if err := png.Encode(out, resized); err != nil {
			return fmt.Errorf("encode png thumbnail: %w", err)
		}
	default:
		// Default JPEG para todo lo demás (incluido jpeg).
		if err := jpeg.Encode(out, resized, &jpeg.Options{Quality: ThumbnailQuality}); err != nil {
			return fmt.Errorf("encode jpeg thumbnail: %w", err)
		}
	}

	return nil
}

// resample escala src a dstW×dstH con el kernel Catmull-Rom de x/image
// (bicúbico, buen equilibrio nitidez/suavidad tanto reduciendo como
// ampliando). Devuelve siempre un *image.RGBA nuevo.
func resample(src image.Image, dstW, dstH int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
	return dst
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}
