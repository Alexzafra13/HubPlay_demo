package blurhash

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

func TestEncode_SolidColor(t *testing.T) {
	// A solid red image should produce a valid blurhash.
	img := GenerateTestImage(64, 64, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	hash := Encode(4, 3, img)

	if hash == "" {
		t.Fatal("expected non-empty blurhash")
	}

	// Blurhash for 4x3 components = 1 (size) + 1 (quant) + 4 (DC) + 11*2 (AC) = 28 chars
	expectedLen := 1 + 1 + 4 + (4*3-1)*2
	if len(hash) != expectedLen {
		t.Errorf("expected length %d, got %d (hash=%q)", expectedLen, len(hash), hash)
	}

	// All characters must be valid base83
	for _, ch := range hash {
		if !strings.ContainsRune(base83Chars, ch) {
			t.Errorf("invalid base83 character %q in hash %q", string(ch), hash)
		}
	}
}

func TestEncode_DifferentColors_DifferentHashes(t *testing.T) {
	red := GenerateTestImage(32, 32, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	blue := GenerateTestImage(32, 32, color.RGBA{R: 0, G: 0, B: 255, A: 255})

	hashRed := Encode(4, 3, red)
	hashBlue := Encode(4, 3, blue)

	if hashRed == hashBlue {
		t.Error("expected different hashes for different colors")
	}
}

func TestEncode_SameImage_Deterministic(t *testing.T) {
	img := GenerateTestImage(48, 48, color.RGBA{R: 100, G: 150, B: 200, A: 255})
	hash1 := Encode(4, 3, img)
	hash2 := Encode(4, 3, img)

	if hash1 != hash2 {
		t.Errorf("expected deterministic output, got %q and %q", hash1, hash2)
	}
}

func TestEncode_SmallImage(t *testing.T) {
	// Even a 1x1 image should work.
	img := GenerateTestImage(1, 1, color.RGBA{R: 128, G: 128, B: 128, A: 255})
	hash := Encode(4, 3, img)

	if hash == "" {
		t.Fatal("expected non-empty blurhash for 1x1 image")
	}
}

func TestEncode_ComponentVariations(t *testing.T) {
	img := GenerateTestImage(32, 32, color.RGBA{R: 50, G: 100, B: 150, A: 255})

	tests := []struct {
		x, y        int
		expectedLen int
	}{
		{1, 1, 1 + 1 + 4},                 // 6 chars (DC only)
		{2, 2, 1 + 1 + 4 + (2*2-1)*2},     // 12 chars
		{4, 3, 1 + 1 + 4 + (4*3-1)*2},     // 28 chars
		{9, 9, 1 + 1 + 4 + (9*9-1)*2},     // 166 chars (maximum)
	}

	for _, tc := range tests {
		hash := Encode(tc.x, tc.y, img)
		if len(hash) != tc.expectedLen {
			t.Errorf("Encode(%d,%d): expected len %d, got %d", tc.x, tc.y, tc.expectedLen, len(hash))
		}
	}
}

func TestEncodeFromRGBA(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 16), G: uint8(y * 16), B: 128, A: 255})
		}
	}

	hash := EncodeFromRGBA(img)
	if hash == "" {
		t.Fatal("expected non-empty blurhash from EncodeFromRGBA")
	}

	// Should be same as Encode(4, 3, img)
	expected := Encode(4, 3, img)
	if hash != expected {
		t.Errorf("EncodeFromRGBA mismatch: got %q, want %q", hash, expected)
	}
}

func TestSRGBRoundtrip(t *testing.T) {
	// linearToSRGB(sRGBToLinear(x)) should approximately equal x
	values := []float64{0.0, 0.04045, 0.1, 0.5, 0.8, 1.0}
	for _, v := range values {
		roundtrip := linearToSRGB(sRGBToLinear(v))
		diff := roundtrip - v
		if diff > 0.001 || diff < -0.001 {
			t.Errorf("sRGB roundtrip for %.4f: got %.4f (diff %.6f)", v, roundtrip, diff)
		}
	}
}

func TestBase83Encoding(t *testing.T) {
	tests := []struct {
		value  int
		length int
		want   string
	}{
		{0, 1, "0"},
		{1, 1, "1"},
		{82, 1, "~"},
		{0, 2, "00"},
		{83, 2, "10"},
	}

	for _, tc := range tests {
		got := encodeBase83(tc.value, tc.length)
		if got != tc.want {
			t.Errorf("encodeBase83(%d, %d) = %q, want %q", tc.value, tc.length, got, tc.want)
		}
	}
}

func TestDownsample(t *testing.T) {
	src := GenerateTestImage(100, 100, color.RGBA{R: 200, G: 100, B: 50, A: 255})
	dst := downsample(src, 10, 10)

	bounds := dst.Bounds()
	if bounds.Dx() != 10 || bounds.Dy() != 10 {
		t.Errorf("expected 10x10, got %dx%d", bounds.Dx(), bounds.Dy())
	}

	// Should preserve the color (solid image)
	r, g, b, _ := dst.At(5, 5).RGBA()
	if r>>8 != 200 || g>>8 != 100 || b>>8 != 50 {
		t.Errorf("expected (200,100,50), got (%d,%d,%d)", r>>8, g>>8, b>>8)
	}
}
