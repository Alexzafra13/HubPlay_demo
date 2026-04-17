package imaging

import "testing"

func TestIsValidKind(t *testing.T) {
	cases := map[string]bool{
		"primary":  true,
		"backdrop": true,
		"logo":     true,
		"thumb":    true,
		"banner":   true,
		"":         false,
		"bogus":    false,
		"PRIMARY":  false, // case-sensitive by design (matches DB enum)
	}
	for in, want := range cases {
		if got := IsValidKind(in); got != want {
			t.Errorf("IsValidKind(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestIsValidContentType(t *testing.T) {
	cases := map[string]bool{
		"image/jpeg":               true,
		"image/jpeg; charset=x":    true,
		"image/png":                true,
		"image/png; foo=bar":       true,
		"image/webp":               true,
		"image/gif":                false,
		"image/bmp":                false,
		"text/html":                false,
		"application/octet-stream": false,
		"":                         false,
	}
	for in, want := range cases {
		if got := IsValidContentType(in); got != want {
			t.Errorf("IsValidContentType(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestExtensionForContentType(t *testing.T) {
	cases := map[string]string{
		"image/jpeg": ".jpg",
		"image/png":  ".png",
		"image/webp": ".webp",
		"image/gif":  ".jpg", // unknown types fall through to .jpg (historical behavior)
		"":           ".jpg",
	}
	for in, want := range cases {
		if got := ExtensionForContentType(in); got != want {
			t.Errorf("ExtensionForContentType(%q) = %q, want %q", in, got, want)
		}
	}
}
