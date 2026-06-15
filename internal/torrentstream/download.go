package torrentstream

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

// DownloadStatus is the lifecycle of a download-to-library job.
type DownloadStatus string

const (
	DownloadQueued      DownloadStatus = "queued"
	DownloadDownloading DownloadStatus = "downloading"
	DownloadCopying     DownloadStatus = "copying"
	DownloadCompleted   DownloadStatus = "completed"
	DownloadFailed      DownloadStatus = "failed"
)

// DownloadJob is a snapshot of one download (safe to serialise to the API).
type DownloadJob struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Dest       string         `json:"dest"`
	Status     DownloadStatus `json:"status"`
	BytesDone  int64          `json:"bytes_done"`
	BytesTotal int64          `json:"bytes_total"`
	Error      string         `json:"error,omitempty"`
}

// downloadJob is the internal, mutable record.
type downloadJob struct {
	mu   sync.Mutex
	snap DownloadJob
}

func (j *downloadJob) set(fn func(*DownloadJob)) {
	j.mu.Lock()
	defer j.mu.Unlock()
	fn(&j.snap)
}

func (j *downloadJob) get() DownloadJob {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.snap
}

// StartDownload fully downloads src (magnet or http .torrent) and copies its
// files into destDir, then calls onDone. It returns immediately with a
// queued job; progress is observed via Downloads(). The download runs on the
// manager's background context (it survives the originating HTTP request and
// is cancelled on Close).
func (m *Manager) StartDownload(src, destDir string, onDone func(error)) DownloadJob {
	j := &downloadJob{snap: DownloadJob{
		ID:     uuid.NewString(),
		Dest:   destDir,
		Status: DownloadQueued,
	}}
	m.mu.Lock()
	m.downloads[j.snap.ID] = j
	m.mu.Unlock()

	go m.runDownload(j, src, destDir, onDone)
	return j.get()
}

// Downloads returns a snapshot of every known download job.
func (m *Manager) Downloads() []DownloadJob {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]DownloadJob, 0, len(m.downloads))
	for _, j := range m.downloads {
		out = append(out, j.get())
	}
	return out
}

func (m *Manager) runDownload(j *downloadJob, src, destDir string, onDone func(error)) {
	fail := func(err error) {
		j.set(func(s *DownloadJob) {
			s.Status = DownloadFailed
			s.Error = err.Error()
		})
		m.logger.Warn("torrentstream: download failed", "src", src, "error", err)
		if onDone != nil {
			onDone(err)
		}
	}

	t, err := m.addTorrent(src)
	if err != nil {
		fail(err)
		return
	}
	select {
	case <-t.GotInfo():
	case <-m.dlCtx.Done():
		t.Drop()
		fail(m.dlCtx.Err())
		return
	case <-time.After(m.opts.MetadataTimeout):
		t.Drop()
		fail(ErrMetadataTimeout)
		return
	}

	total := t.Length()
	j.set(func(s *DownloadJob) {
		s.Name = t.Name()
		s.Status = DownloadDownloading
		s.BytesTotal = total
	})
	t.DownloadAll()

	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-m.dlCtx.Done():
			t.Drop()
			fail(m.dlCtx.Err())
			return
		case <-tick.C:
			done := t.BytesCompleted()
			j.set(func(s *DownloadJob) { s.BytesDone = done })
			if total > 0 && done >= total {
				goto complete
			}
		}
	}

complete:
	j.set(func(s *DownloadJob) { s.Status = DownloadCopying })
	paths := make([]string, 0, len(t.Files()))
	for _, f := range t.Files() {
		paths = append(paths, f.Path())
	}
	if err := copyFiles(m.opts.DataDir, paths, destDir); err != nil {
		t.Drop()
		fail(fmt.Errorf("copy to library: %w", err))
		return
	}
	t.Drop()
	j.set(func(s *DownloadJob) {
		s.Status = DownloadCompleted
		s.BytesDone = total
	})
	m.logger.Info("torrentstream: download completed", "name", j.get().Name, "dest", destDir)
	if onDone != nil {
		onDone(nil)
	}
}

// copyFiles copies each relative path from the scratch dataDir into destDir,
// preserving the torrent's folder structure (so a movie's own folder lands
// inside destDir). Copy (not rename) because the scratch and the library are
// usually different mounts.
func copyFiles(dataDir string, paths []string, destDir string) error {
	for _, rel := range paths {
		if err := copyFile(filepath.Join(dataDir, rel), filepath.Join(destDir, rel)); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src) //nolint:gosec // paths derived from torrent metadata under our scratch dir
	if err != nil {
		return err
	}
	defer in.Close()           //nolint:errcheck
	out, err := os.Create(dst) //nolint:gosec // dst under the operator's library dir
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close() //nolint:errcheck
		return err
	}
	return out.Close()
}
