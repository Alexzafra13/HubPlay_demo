package upload

import "testing"

func TestSanitizeFilename_HappyPath(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"movie.mkv", "movie.mkv"},
		{"Some Movie (2024).mp4", "Some Movie (2024).mp4"},
		{"Película Año (2024).mkv", "Película Año (2024).mkv"},
		{"Star Wars - Episode IV.mp4", "Star Wars - Episode IV.mp4"},
		{"  spaced  out  .mkv  ", "spaced out .mkv"},
	}
	for _, tc := range cases {
		if got := SanitizeFilename(tc.in); got != tc.want {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSanitizeFilename_RejectsPathTraversal(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"../../etc/passwd", "passwd"},
		{"../../../movie.mkv", "movie.mkv"},
		{"/etc/passwd", "passwd"},
		{"C:\\Windows\\System32\\cmd.exe", "cmd.exe"},
		{"..\\..\\evil.bat", "evil.bat"},
		{"sub/dir/file.mkv", "file.mkv"},
	}
	for _, tc := range cases {
		if got := SanitizeFilename(tc.in); got != tc.want {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSanitizeFilename_RejectsControlAndExotic(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"hi\x00.mkv", "hi_.mkv"},
		{"file\nname.mkv", "file_name.mkv"},
		{"weird:|*?<>.mkv", "weird_.mkv"},
		{"emoji 🎬 movie.mkv", "emoji _ movie.mkv"},
	}
	for _, tc := range cases {
		got := SanitizeFilename(tc.in)
		if got != tc.want {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSanitizeFilename_RejectsEmptyish(t *testing.T) {
	cases := []string{
		"",
		"   ",
		".",
		"..",
		"...",
		"\\",
		"/",
	}
	for _, in := range cases {
		if got := SanitizeFilename(in); got != "" {
			t.Errorf("SanitizeFilename(%q) = %q, want empty", in, got)
		}
	}
}

// TestSanitizeFilename_StripsLeadingDots: a UNIX hidden file is not a
// security issue once it's relocated to a managed library dir; the
// dot just becomes confusing visually. We strip it instead of rejecting.
func TestSanitizeFilename_StripsLeadingDots(t *testing.T) {
	cases := map[string]string{
		".hidden.mkv":  "hidden.mkv",
		"...weird.mp4": "weird.mp4",
	}
	for in, want := range cases {
		if got := SanitizeFilename(in); got != want {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSanitizeFilename_TruncatesPreservingExtension: nombres larguísimos
// se cortan pero la extensión sobrevive — si no, el validator de
// extensión rechazaría todo lo grande.
func TestSanitizeFilename_TruncatesPreservingExtension(t *testing.T) {
	stem := ""
	for i := 0; i < 400; i++ {
		stem += "a"
	}
	in := stem + ".mkv"
	got := SanitizeFilename(in)
	if len(got) > maxFilenameLength {
		t.Errorf("len = %d, want <= %d", len(got), maxFilenameLength)
	}
	if got == "" || got[len(got)-4:] != ".mkv" {
		t.Errorf("extension lost: got %q", got)
	}
}

// TestSanitizeFilename_CollapsesRuns evita "a___b" o "a   b" en la
// salida — son ruidosos visualmente y suelen indicar caracteres
// reemplazados consecutivos.
func TestSanitizeFilename_CollapsesRuns(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"a:::b.mkv", "a_b.mkv"},
		{"a   b.mkv", "a b.mkv"},
	}
	for _, tc := range cases {
		if got := SanitizeFilename(tc.in); got != tc.want {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestExtensionLower(t *testing.T) {
	cases := map[string]string{
		"a.MKV":      "mkv",
		"a.mp4":      "mp4",
		"a":          "",
		"a.tar.gz":   "gz",
		"":           "",
		"NOEXT":      "",
		".env":       "env", // documenting current behaviour; sanitise rejects it earlier
	}
	for in, want := range cases {
		if got := ExtensionLower(in); got != want {
			t.Errorf("ExtensionLower(%q) = %q, want %q", in, got, want)
		}
	}
}
