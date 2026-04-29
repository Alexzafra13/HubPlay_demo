package imaging

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// forgedPNG returns a PNG signature + IHDR chunk that advertises the given
// dimensions. The image has no image data; image.DecodeConfig only needs
// IHDR to report Width/Height, so this is enough to exercise the guard
// without allocating a real pixel buffer.
func forgedPNG(w, h uint32) []byte {
	var buf bytes.Buffer
	// 8-byte PNG signature.
	buf.Write([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A})
	// IHDR chunk: 4B length, 4B type "IHDR", 13B data, 4B CRC over type+data.
	lenBytes := []byte{0x00, 0x00, 0x00, 13}
	typeBytes := []byte("IHDR")
	data := make([]byte, 13)
	binary.BigEndian.PutUint32(data[0:4], w)
	binary.BigEndian.PutUint32(data[4:8], h)
	data[8] = 8  // bit depth
	data[9] = 2  // color type (RGB)
	data[10] = 0 // compression
	data[11] = 0 // filter
	data[12] = 0 // interlace
	crc := crc32.ChecksumIEEE(append(append([]byte{}, typeBytes...), data...))
	crcBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(crcBytes, crc)
	buf.Write(lenBytes)
	buf.Write(typeBytes)
	buf.Write(data)
	buf.Write(crcBytes)
	return buf.Bytes()
}

func TestIsSafePathSegment(t *testing.T) {
	cases := map[string]bool{
		"item-1":                             true,
		"abc123":                             true,
		"550e8400-e29b-41d4-a716-446655440000": true,
		"":            false,
		".":           false,
		"..":          false,
		"../etc":      false,
		"foo/bar":     false,
		"foo\\bar":    false,
		"a\x00b":      false,
	}
	for in, want := range cases {
		if got := IsSafePathSegment(in); got != want {
			t.Errorf("IsSafePathSegment(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestSniffContentType_DetectsPNGRegardlessOfHeader(t *testing.T) {
	// Build a real PNG byte stream.
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	data := buf.Bytes()

	ct, full, err := SniffContentType(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("sniff: %v", err)
	}
	if !strings.HasPrefix(ct, "image/png") {
		t.Errorf("sniffed: got %q want image/png", ct)
	}

	// Full stream must still deliver the complete original bytes.
	got, err := io.ReadAll(full)
	if err != nil {
		t.Fatalf("readall: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("full stream mismatched (got %d bytes want %d)", len(got), len(data))
	}
}

func TestSniffContentType_RejectsHTMLMasqueradingAsImage(t *testing.T) {
	// Client could set multipart Content-Type: image/jpeg but actually upload HTML.
	// Sniff must see through that.
	html := []byte("<!doctype html><html><body>pwn</body></html>")
	ct, _, err := SniffContentType(bytes.NewReader(html))
	if err != nil {
		t.Fatalf("sniff: %v", err)
	}
	if strings.HasPrefix(ct, "image/") {
		t.Fatalf("HTML sniffed as image: %q", ct)
	}
}

func TestEnforceMaxPixels_AcceptsReasonablePNG(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1000, 1000))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := EnforceMaxPixels(buf.Bytes()); err != nil {
		t.Fatalf("1000x1000 rejected: %v", err)
	}
}

func TestEnforceMaxPixels_RejectsOversizedPNG(t *testing.T) {
	// 50000x50000 = 2.5e9 pixels, well over the 40M guard.
	data := forgedPNG(50000, 50000)
	err := EnforceMaxPixels(data)
	if err == nil {
		t.Fatal("expected ErrTooLarge for 50000x50000, got nil")
	}
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
}

func TestEnforceMaxPixels_NonImageAccepted(t *testing.T) {
	// Unknown formats (animated GIF, AVIF, BMP, …) skip the bomb check
	// and rely on content-type + size limits upstream. The fixture is
	// a malformed RIFF header that even the WebP decoder rejects, so
	// we exercise the "DecodeConfig errored, accept anyway" branch.
	// Note: a *valid* WebP header is now decoded by the registered
	// x/image/webp decoder — see TestComputeBlurhash_* for that path.
	if err := EnforceMaxPixels([]byte("RIFFxxxxWEBPVP8 ...")); err != nil {
		t.Fatalf("unsupported format should not error: %v", err)
	}
}

func TestSafeGet_RejectsLoopback(t *testing.T) {
	srv := httptest.NewServer(nil) // 127.0.0.1
	defer srv.Close()

	_, _, err := SafeGet(srv.URL+"/x", 1024, time.Second)
	if err == nil {
		t.Fatal("expected ErrUnsafeURL for 127.0.0.1, got nil")
	}
	if !errors.Is(err, ErrUnsafeURL) {
		t.Fatalf("want ErrUnsafeURL, got %v", err)
	}
}

func TestSafeGet_RejectsNonHTTP(t *testing.T) {
	_, _, err := SafeGet("file:///etc/passwd", 1024, time.Second)
	if !errors.Is(err, ErrUnsafeURL) {
		t.Fatalf("file:// scheme: want ErrUnsafeURL, got %v", err)
	}
	_, _, err = SafeGet("ftp://example.com/x", 1024, time.Second)
	if !errors.Is(err, ErrUnsafeURL) {
		t.Fatalf("ftp:// scheme: want ErrUnsafeURL, got %v", err)
	}
}

func TestSafeGet_RejectsLiteralLoopbackHostname(t *testing.T) {
	_, _, err := SafeGet("http://localhost:1/x", 1024, time.Second)
	if !errors.Is(err, ErrUnsafeURL) {
		t.Fatalf("localhost: want ErrUnsafeURL, got %v", err)
	}
}
