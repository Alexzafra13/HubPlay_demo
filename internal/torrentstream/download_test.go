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
