package torrentstream

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyFiles(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	// Multi-file torrent layout: <name>/<file>.
	if err := os.MkdirAll(filepath.Join(src, "Movie 2024"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "Movie 2024", "movie.mkv"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "Movie 2024", "movie.srt"), []byte("subs"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := copyFiles(src, []string{"Movie 2024/movie.mkv", "Movie 2024/movie.srt"}, dst)
	if err != nil {
		t.Fatalf("copyFiles: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dst, "Movie 2024", "movie.mkv"))
	if err != nil || string(got) != "payload" {
		t.Fatalf("copied video: %q err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dst, "Movie 2024", "movie.srt")); err != nil {
		t.Errorf("subtitle not copied: %v", err)
	}
}

func TestSelectFiles(t *testing.T) {
	tests := []struct {
		name  string
		files []torrentFile
		want  []string
	}{
		{
			name: "keeps video + sidecar subs, drops nfo/txt/sample",
			files: []torrentFile{
				{"Movie 2024/movie.mkv", 4_000_000_000},
				{"Movie 2024/movie.srt", 50_000},
				{"Movie 2024/movie.nfo", 1_000},
				{"Movie 2024/RARBG.txt", 30},
				{"Movie 2024/Sample/sample.mkv", 40_000_000},
			},
			want: []string{"Movie 2024/movie.mkv", "Movie 2024/movie.srt"},
		},
		{
			name: "season pack keeps every episode",
			files: []torrentFile{
				{"Show S01/s01e01.mkv", 1_000_000_000},
				{"Show S01/s01e02.mkv", 1_000_000_000},
			},
			want: []string{"Show S01/s01e01.mkv", "Show S01/s01e02.mkv"},
		},
		{
			name: "no recognised video falls back to the largest file",
			files: []torrentFile{
				{"weird.bin", 900},
				{"big.iso", 5_000_000_000},
			},
			want: []string{"big.iso"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectFiles(tt.files)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v want %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("at %d: got %q want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestScratchPathRefusesEscapes(t *testing.T) {
	m := &Manager{opts: Options{DataDir: filepath.Join(t.TempDir(), "scratch")}}
	if p := m.scratchPath(""); p != "" {
		t.Errorf("empty name should yield no path, got %q", p)
	}
	if p := m.scratchPath("../../etc"); p != "" {
		t.Errorf("escaping name should yield no path, got %q", p)
	}
	if p := m.scratchPath("Movie 2024"); p == "" {
		t.Errorf("normal name should resolve, got empty")
	}
}

func TestDownloadJobSnapshot(t *testing.T) {
	j := &downloadJob{snap: DownloadJob{ID: "x", Status: DownloadQueued}}
	j.set(func(s *DownloadJob) {
		s.Status = DownloadDownloading
		s.BytesDone = 50
		s.BytesTotal = 100
	})
	got := j.get()
	if got.Status != DownloadDownloading || got.BytesDone != 50 || got.BytesTotal != 100 {
		t.Fatalf("snapshot: %+v", got)
	}
}
