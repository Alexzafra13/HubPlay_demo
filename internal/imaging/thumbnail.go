package imaging

import (
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
)

// GenerateThumbnail reads the source image, resizes it to maxWidth (preserving
// aspect ratio) using nearest-neighbor interpolation, and writes the result to
// dstPath. Only JPEG and PNG are supported (standard library only).
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
		// Image is already smaller; just copy the file.
		return copyFile(srcPath, dstPath)
	}

	dstW := maxWidth
	dstH := srcH * maxWidth / srcW
	if dstH < 1 {
		dstH = 1
	}

	resized := nearestNeighborResize(img, dstW, dstH)

	// Ensure parent directory exists.
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
		// Default to JPEG for everything else (including jpeg).
		if err := jpeg.Encode(out, resized, &jpeg.Options{Quality: 80}); err != nil {
			return fmt.Errorf("encode jpeg thumbnail: %w", err)
		}
	}

	return nil
}

func nearestNeighborResize(src image.Image, dstW, dstH int) image.Image {
	bounds := src.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	for y := 0; y < dstH; y++ {
		srcY := bounds.Min.Y + y*srcH/dstH
		for x := 0; x < dstW; x++ {
			srcX := bounds.Min.X + x*srcW/dstW
			dst.Set(x, y, src.At(srcX, srcY))
		}
	}
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
