package imaging

import (
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func createTestJPEG(t *testing.T, path string, w, h int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
}

func createTestPNG(t *testing.T, path string, w, h int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateThumbnail_JPEG(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "source.jpg")
	dstPath := filepath.Join(dir, "thumb.jpg")

	createTestJPEG(t, srcPath, 800, 600)

	if err := GenerateThumbnail(srcPath, dstPath, 200); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify output exists
	f, err := os.Open(dstPath)
	if err != nil {
		t.Fatalf("thumbnail not created: %v", err)
	}
	defer f.Close()

	img, _, err := image.DecodeConfig(f)
	if err != nil {
		t.Fatalf("failed to decode thumbnail: %v", err)
	}

	if img.Width != 200 {
		t.Errorf("expected width 200, got %d", img.Width)
	}
	// Height should be proportional: 600 * 200 / 800 = 150
	if img.Height != 150 {
		t.Errorf("expected height 150, got %d", img.Height)
	}
}

func TestGenerateThumbnail_PNG(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "source.png")
	dstPath := filepath.Join(dir, "thumb.png")

	createTestPNG(t, srcPath, 640, 480)

	if err := GenerateThumbnail(srcPath, dstPath, 320); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	f, err := os.Open(dstPath)
	if err != nil {
		t.Fatalf("thumbnail not created: %v", err)
	}
	defer f.Close()

	img, _, err := image.DecodeConfig(f)
	if err != nil {
		t.Fatalf("failed to decode thumbnail: %v", err)
	}

	if img.Width != 320 {
		t.Errorf("expected width 320, got %d", img.Width)
	}
	// 480 * 320 / 640 = 240
	if img.Height != 240 {
		t.Errorf("expected height 240, got %d", img.Height)
	}
}

func TestGenerateThumbnail_SmallerThanMax(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "small.jpg")
	dstPath := filepath.Join(dir, "thumb.jpg")

	createTestJPEG(t, srcPath, 100, 80)

	// maxWidth larger than source → should copy the file unchanged
	if err := GenerateThumbnail(srcPath, dstPath, 300); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	srcInfo, _ := os.Stat(srcPath)
	dstInfo, err := os.Stat(dstPath)
	if err != nil {
		t.Fatalf("output not created: %v", err)
	}

	if srcInfo.Size() != dstInfo.Size() {
		t.Errorf("expected same file size for copy, src=%d dst=%d", srcInfo.Size(), dstInfo.Size())
	}
}

func TestGenerateThumbnail_CreatesDir(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "source.jpg")
	dstPath := filepath.Join(dir, "sub", "dir", "thumb.jpg")

	createTestJPEG(t, srcPath, 400, 300)

	if err := GenerateThumbnail(srcPath, dstPath, 200); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(dstPath); err != nil {
		t.Errorf("expected thumbnail at nested path: %v", err)
	}
}

func TestGenerateThumbnail_SourceNotFound(t *testing.T) {
	dir := t.TempDir()
	err := GenerateThumbnail(filepath.Join(dir, "missing.jpg"), filepath.Join(dir, "out.jpg"), 200)
	if err == nil {
		t.Error("expected error for missing source")
	}
}

func TestNearestNeighborResize(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 100, 50))
	for y := 0; y < 50; y++ {
		for x := 0; x < 100; x++ {
			src.Set(x, y, color.RGBA{R: 255, G: 0, B: 0, A: 255})
		}
	}

	dst := nearestNeighborResize(src, 10, 5)
	bounds := dst.Bounds()

	if bounds.Dx() != 10 || bounds.Dy() != 5 {
		t.Errorf("expected 10x5, got %dx%d", bounds.Dx(), bounds.Dy())
	}

	// Color should be preserved
	r, g, b, _ := dst.At(5, 2).RGBA()
	if r>>8 != 255 || g>>8 != 0 || b>>8 != 0 {
		t.Errorf("expected red pixel, got (%d,%d,%d)", r>>8, g>>8, b>>8)
	}
}
