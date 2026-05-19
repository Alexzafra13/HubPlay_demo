package upload

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// ─── ValidateExtension ──────────────────────────────────────────────

func TestValidateExtension_Accepts(t *testing.T) {
	for _, name := range []string{
		"movie.mkv", "movie.MKV", "show.mp4", "clip.webm",
		"old.avi", "broadcast.ts", "subs.srt", "subs.ASS",
	} {
		if err := ValidateExtension(name); err != nil {
			t.Errorf("ValidateExtension(%q) = %v, want nil", name, err)
		}
	}
}

func TestValidateExtension_Rejects(t *testing.T) {
	cases := []string{
		"movie.exe",
		"script.sh",
		"file",        // no extension
		"document.pdf",
		"image.png",
		"music.mp3",   // audio only — out of scope v1
		"archive.zip",
	}
	for _, name := range cases {
		err := ValidateExtension(name)
		if !errors.Is(err, ErrExtensionNotAllowed) {
			t.Errorf("ValidateExtension(%q) = %v, want ErrExtensionNotAllowed", name, err)
		}
	}
}

func TestAllowedExtensions_NonEmpty(t *testing.T) {
	got := AllowedExtensions()
	if len(got) < 10 {
		t.Errorf("expected >= 10 allowed extensions, got %d", len(got))
	}
	// Sanity: mutating the slice does not leak into the package.
	got[0] = "PWNED"
	if _, ok := allowedExtensions["PWNED"]; ok {
		t.Error("AllowedExtensions leaked internal map reference")
	}
}

// ─── DetectKind: video ──────────────────────────────────────────────

func TestDetectKind_MKV(t *testing.T) {
	header := []byte{0x1A, 0x45, 0xDF, 0xA3, 0x01, 0x02, 0x03, 0x04}
	kind, mime, err := DetectKindFromBytes(pad(header, 32))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if kind != KindVideo {
		t.Errorf("kind = %s, want video", kind)
	}
	if mime != "video/x-matroska" {
		t.Errorf("mime = %q", mime)
	}
}

func TestDetectKind_MP4(t *testing.T) {
	// 4-byte size + "ftyp" + brand + ...
	header := []byte{0x00, 0x00, 0x00, 0x20}
	header = append(header, []byte("ftypisom")...)
	header = append(header, bytes.Repeat([]byte{0}, 20)...)
	kind, mime, err := DetectKindFromBytes(header)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if kind != KindVideo || mime != "video/mp4" {
		t.Errorf("got kind=%s mime=%s", kind, mime)
	}
}

func TestDetectKind_AVI(t *testing.T) {
	header := append([]byte("RIFF"), bytes.Repeat([]byte{0}, 28)...)
	kind, _, err := DetectKindFromBytes(header)
	if err != nil || kind != KindVideo {
		t.Errorf("AVI not detected: kind=%s err=%v", kind, err)
	}
}

func TestDetectKind_MPEGTS(t *testing.T) {
	// Two sync bytes 188 bytes apart — the spec invariant.
	buf := make([]byte, 200)
	buf[0] = 0x47
	buf[188] = 0x47
	kind, mime, err := DetectKindFromBytes(buf)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if kind != KindVideo || mime != "video/mp2t" {
		t.Errorf("got kind=%s mime=%s", kind, mime)
	}
}

// TestDetectKind_RejectsLonelyTSSync pin la defensa contra falso-positivo:
// un único 0x47 al principio sin la segunda sync no debe pasar como TS.
func TestDetectKind_RejectsLonelyTSSync(t *testing.T) {
	buf := make([]byte, 256)
	buf[0] = 0x47
	// buf[188] left at 0
	_, _, err := DetectKindFromBytes(buf)
	if !errors.Is(err, ErrMimeMismatch) {
		t.Errorf("lonely 0x47 byte passed detection: %v", err)
	}
}

// ─── DetectKind: subtitles ──────────────────────────────────────────

func TestDetectKind_SRT(t *testing.T) {
	body := "1\n00:00:01,000 --> 00:00:04,000\nHello there\n\n"
	kind, _, err := DetectKindFromBytes([]byte(body))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if kind != KindSubtitle {
		t.Errorf("SRT kind = %s", kind)
	}
}

func TestDetectKind_WebVTT(t *testing.T) {
	body := "WEBVTT\n\n00:00:01.000 --> 00:00:04.000\nHello\n"
	kind, _, err := DetectKindFromBytes([]byte(body))
	if err != nil || kind != KindSubtitle {
		t.Errorf("WebVTT kind=%s err=%v", kind, err)
	}
}

func TestDetectKind_WebVTT_WithBOM(t *testing.T) {
	body := "\xEF\xBB\xBFWEBVTT\n\n00:00:01.000 --> 00:00:04.000\nHi\n"
	kind, _, err := DetectKindFromBytes([]byte(body))
	if err != nil || kind != KindSubtitle {
		t.Errorf("BOM WebVTT kind=%s err=%v", kind, err)
	}
}

func TestDetectKind_ASS(t *testing.T) {
	body := "[Script Info]\nTitle: Sample\nScriptType: v4.00+\n[V4+ Styles]\nFormat: Name, Fontname\n[Events]\nFormat: Layer\nDialogue: 0,0:00:01\n"
	kind, _, err := DetectKindFromBytes([]byte(body))
	if err != nil || kind != KindSubtitle {
		t.Errorf("ASS kind=%s err=%v", kind, err)
	}
}

// ─── DetectKind: rejections ─────────────────────────────────────────

func TestDetectKind_RejectsRandomBinary(t *testing.T) {
	buf := []byte{0xCA, 0xFE, 0xBA, 0xBE, 0xDE, 0xAD, 0xBE, 0xEF}
	_, _, err := DetectKindFromBytes(pad(buf, 32))
	if !errors.Is(err, ErrMimeMismatch) {
		t.Errorf("want ErrMimeMismatch, got %v", err)
	}
}

func TestDetectKind_RejectsImageDisguised(t *testing.T) {
	// PNG signature
	buf := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	_, _, err := DetectKindFromBytes(pad(buf, 32))
	if !errors.Is(err, ErrMimeMismatch) {
		t.Errorf("PNG passed as media: %v", err)
	}
}

// TestDetectKind_RejectsHEICDisguisedAsMP4: ftyp box con brand 'heic'
// es una imagen, no MP4. Hoy aceptamos cualquier ftyp; documentamos
// que es laxo — la siguiente fase (ffprobe) lo rechazará igualmente.
// El test deja la decisión clavada para revisarla más adelante.
func TestDetectKind_AcceptsAnyFtyp(t *testing.T) {
	header := []byte{0x00, 0x00, 0x00, 0x20}
	header = append(header, []byte("ftypheic")...)
	header = append(header, bytes.Repeat([]byte{0}, 20)...)
	kind, _, err := DetectKindFromBytes(header)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if kind != KindVideo {
		t.Errorf("policy changed: HEIC ftyp now %s — ffprobe step still mandatory", kind)
	}
}

func TestDetectKind_EmptyIsError(t *testing.T) {
	_, _, err := DetectKindFromBytes(nil)
	if !errors.Is(err, ErrEmptyFile) {
		t.Errorf("want ErrEmptyFile, got %v", err)
	}
}

// ─── DetectKind from stream ─────────────────────────────────────────

func TestDetectKind_StreamReader(t *testing.T) {
	header := []byte{0x1A, 0x45, 0xDF, 0xA3, 0x42, 0x42, 0x42, 0x42}
	r := strings.NewReader(string(header))
	kind, mime, err := DetectKind(r)
	if err != nil || kind != KindVideo || mime != "video/x-matroska" {
		t.Errorf("kind=%s mime=%s err=%v", kind, mime, err)
	}
}

// ─── helpers ────────────────────────────────────────────────────────

func pad(b []byte, length int) []byte {
	if len(b) >= length {
		return b
	}
	out := make([]byte, length)
	copy(out, b)
	return out
}
