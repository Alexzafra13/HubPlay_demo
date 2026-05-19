package upload

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeSubpath_AcceptsValid(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{".", ""},
		{"/", ""},
		{"   ", ""},
		{"Movies", "Movies"},
		{"Movies/Action", "Movies/Action"},
		// Backslash → slash normalization (clients en Windows).
		{`Movies\Drama`, "Movies/Drama"},
		// Doble slash colapsa.
		{"Movies//Action", "Movies/Action"},
		// Trailing slash queda fuera del canónico.
		{"Movies/Action/", "Movies/Action"},
		{"/Movies/Drama/", ""}, // Wait — leading slash es ABS, rechazo
	}
	for _, tc := range cases {
		got, err := SanitizeSubpath(tc.in)
		// El caso "/Movies/Drama/" debe fallar por leading slash; lo
		// trato aparte abajo.
		if strings.HasPrefix(tc.in, "/") && tc.in != "/" {
			if err == nil {
				t.Errorf("Sanitize(%q) accepted absolute path", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("Sanitize(%q) err: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("Sanitize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSanitizeSubpath_RejectsTraversal(t *testing.T) {
	cases := []string{
		"..",
		"../etc",
		"Movies/../etc",
		"Movies/../../../etc/passwd",
		`Movies\..\..\etc`,
	}
	for _, in := range cases {
		_, err := SanitizeSubpath(in)
		if !errors.Is(err, ErrSubpathInvalid) {
			t.Errorf("Sanitize(%q) = %v, want ErrSubpathInvalid", in, err)
		}
	}
}

func TestSanitizeSubpath_RejectsAbsoluteAndDrive(t *testing.T) {
	cases := []string{
		"/etc/passwd",
		"/var/lib/hubplay",
		`C:\Windows`,
		`D:\Movies`,
	}
	for _, in := range cases {
		_, err := SanitizeSubpath(in)
		if !errors.Is(err, ErrSubpathInvalid) {
			t.Errorf("Sanitize(%q) accepted absolute, err = %v", in, err)
		}
	}
}

func TestSanitizeSubpath_SanitizesSegments(t *testing.T) {
	// Cada segmento pasa por SanitizeFilename — caracteres de control,
	// path traversal por segmento, etc., quedan limpiados o rechazan.
	got, err := SanitizeSubpath("Movies/Some Movie (2024)")
	if err != nil || got != "Movies/Some Movie (2024)" {
		t.Errorf("got %q err %v", got, err)
	}

	// Segmento con sólo caracteres inválidos → vacío → rechazo entero.
	_, err = SanitizeSubpath("Movies/...")
	if !errors.Is(err, ErrSubpathInvalid) {
		t.Errorf("expected reject, got %v", err)
	}
}

func TestResolveSubpath_HappyPath(t *testing.T) {
	root := t.TempDir()
	full, err := ResolveSubpath(root, "Movies/Drama")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := filepath.Join(root, "Movies", "Drama")
	if full != want {
		t.Errorf("got %s, want %s", full, want)
	}
}

func TestResolveSubpath_RootIsLibraryRoot(t *testing.T) {
	root := t.TempDir()
	full, err := ResolveSubpath(root, "")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if full != root {
		t.Errorf("got %s, want %s", full, root)
	}
}

func TestResolveSubpath_RejectsTraversal(t *testing.T) {
	root := t.TempDir()
	_, err := ResolveSubpath(root, "Movies/../../escape")
	if err == nil {
		t.Error("traversal accepted")
	}
}
