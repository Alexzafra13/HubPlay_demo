package imaging

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func jpegBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 80, G: 120, B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 10, G: 200, B: 10, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

func TestComputeBlurhash_JPEG(t *testing.T) {
	got := ComputeBlurhash(jpegBytes(t, 32, 32), nil)
	if got == "" {
		t.Fatal("expected non-empty blurhash for valid JPEG")
	}
}

func TestComputeBlurhash_PNG(t *testing.T) {
	got := ComputeBlurhash(pngBytes(t, 32, 32), nil)
	if got == "" {
		t.Fatal("expected non-empty blurhash for valid PNG")
	}
}

func TestComputeBlurhash_Garbage_ReturnsEmpty(t *testing.T) {
	got := ComputeBlurhash([]byte("not an image at all"), nil)
	if got != "" {
		t.Fatalf("expected empty blurhash for garbage input, got %q", got)
	}
}
