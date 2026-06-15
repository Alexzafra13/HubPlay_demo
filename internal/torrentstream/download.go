package torrentstream

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/google/uuid"
)

// DownloadEventSink recibe el snapshot de un job cada vez que cambia, para
// que un transporte (SSE) lo empuje al cliente. Es opcional: nil desactiva
// el push y el front cae al fetch puntual. Interfaz local (sink pattern)
// para no importar el paquete event desde el motor — misma regla anti-ciclo
// que usa observability en stream/iptv.
type DownloadEventSink interface {
	PublishDownload(job DownloadJob)
}

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
// Dest es ruta del filesystem del servidor: no se expone al cliente (json:"-").
type DownloadJob struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Dest       string         `json:"-"`
	Status     DownloadStatus `json:"status"`
	BytesDone  int64          `json:"bytes_done"`
	BytesTotal int64          `json:"bytes_total"`
	Error      string         `json:"error,omitempty"`
}

// isActiveDownload indica un job en vuelo (todavía consume el src/scratch).
func isActiveDownload(s DownloadStatus) bool {
	return s == DownloadQueued || s == DownloadDownloading || s == DownloadCopying
}

// downloadJob is the internal, mutable record. Además del snapshot público
// guarda el src (de-dup), el infohash (sólo cuando resuelve metadata) y el
// instante en que alcanzó estado terminal (para el prune del reaper).
type downloadJob struct {
	mu       sync.Mutex
	snap     DownloadJob
	src      string
	infoHash string
	finished time.Time
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

// terminalSince devuelve el estado actual y, si es terminal, cuándo lo
// alcanzó (cero si sigue activo). Lo usa el prune.
func (j *downloadJob) terminalSince() (DownloadStatus, time.Time) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.snap.Status, j.finished
}

// StartDownload fully downloads src (magnet or http .torrent) and copies its
// files into destDir, then calls onDone. It returns immediately with a
// queued job; progress is observed via Downloads(). The download runs on the
// manager's background context (it survives the originating HTTP request and
// is cancelled on Close).
//
// De-dup: si ya hay un job activo para el mismo src, devuelve ese en vez de
// lanzar otra goroutine — dos descargas del mismo torrent competirían por los
// mismos ficheros de scratch y gastarían ancho de banda doble.
func (m *Manager) StartDownload(src, destDir string, onDone func(error)) DownloadJob {
	m.mu.Lock()
	for _, j := range m.downloads {
		if j.src == src {
			if s := j.get(); isActiveDownload(s.Status) {
				m.mu.Unlock()
				return s
			}
		}
	}
	j := &downloadJob{src: src, snap: DownloadJob{
		ID:     uuid.NewString(),
		Dest:   destDir,
		Status: DownloadQueued,
	}}
	m.downloads[j.snap.ID] = j
	m.mu.Unlock()

	m.publishDownload(j)
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

// publishDownload empuja el snapshot del job al sink (SSE), si lo hay.
func (m *Manager) publishDownload(j *downloadJob) {
	if m.sink == nil {
		return
	}
	m.sink.PublishDownload(j.get())
}

func (m *Manager) runDownload(j *downloadJob, src, destDir string, onDone func(error)) {
	var t *torrent.Torrent

	// finish marca el estado terminal, suelta la referencia al torrent
	// (refcount: drop + limpieza de scratch sólo cuando es el último holder,
	// para no tirar una sesión de streaming del mismo infohash), empuja el
	// snapshot final por SSE y avisa al caller.
	finish := func(status DownloadStatus, derr error) {
		j.set(func(s *DownloadJob) {
			s.Status = status
			if derr != nil {
				s.Error = derr.Error()
			}
		})
		j.mu.Lock()
		j.finished = m.now()
		j.mu.Unlock()
		if t != nil {
			m.release(t)
		}
		m.publishDownload(j)
		if onDone != nil {
			onDone(derr)
		}
	}
	fail := func(err error) {
		m.logger.Warn("torrentstream: download failed", "src", src, "error", err)
		finish(DownloadFailed, err)
	}

	var err error
	t, err = m.addTorrent(src)
	if err != nil {
		fail(err) // t == nil: nada que soltar
		return
	}
	// El infohash de un magnet/torrent se conoce ya tras addTorrent (antes
	// de la metadata). Lo registramos como holder de inmediato para que un
	// reap concurrente no nos lo quite.
	m.acquire(t.InfoHash().HexString())

	select {
	case <-t.GotInfo():
	case <-m.dlCtx.Done():
		fail(m.dlCtx.Err())
		return
	case <-time.After(m.opts.MetadataTimeout):
		fail(ErrMetadataTimeout)
		return
	}

	j.mu.Lock()
	j.infoHash = t.InfoHash().HexString()
	j.mu.Unlock()

	total := t.Length()
	if total <= 0 {
		fail(ErrNoFiles)
		return
	}
	name := t.Name()
	j.set(func(s *DownloadJob) {
		s.Name = name
		s.Status = DownloadDownloading
		s.BytesTotal = total
	})
	m.publishDownload(j)
	t.DownloadAll()

	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-m.dlCtx.Done():
			fail(m.dlCtx.Err())
			return
		case <-tick.C:
			j.set(func(s *DownloadJob) { s.BytesDone = t.BytesCompleted() })
			m.publishDownload(j)
			if t.BytesMissing() == 0 {
				goto complete
			}
		}
	}

complete:
	j.set(func(s *DownloadJob) { s.Status = DownloadCopying })
	m.publishDownload(j)
	if err := copyFiles(m.opts.DataDir, selectDownloadFiles(t), destDir); err != nil {
		fail(fmt.Errorf("copy to library: %w", err))
		return
	}
	j.set(func(s *DownloadJob) { s.BytesDone = total })
	m.logger.Info("torrentstream: download completed", "name", name, "dest", destDir)
	finish(DownloadCompleted, nil)
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

// videoExts / subtitleExts son los ficheros que vale la pena bajar a la
// biblioteca. El resto (.nfo, .txt, .jpg, .exe, "RARBG.txt"…) sólo ensucia
// y el scanner podría indexar basura. Mantenemos los subtítulos sidecar
// porque el scanner los engancha como pistas externas del vídeo.
var videoExts = map[string]bool{
	".mkv": true, ".mp4": true, ".m4v": true, ".avi": true, ".mov": true,
	".wmv": true, ".flv": true, ".webm": true, ".ts": true, ".m2ts": true,
	".mpg": true, ".mpeg": true,
}

var subtitleExts = map[string]bool{
	".srt": true, ".ass": true, ".ssa": true, ".sub": true, ".vtt": true, ".idx": true,
}

// isSamplePath descarta los típicos "sample" (clip de 30s que viene aparte)
// para que no acabe indexado como si fuera la película.
func isSamplePath(p string) bool {
	return strings.Contains(strings.ToLower(p), "sample")
}

// torrentFile es el par (ruta, tamaño) que necesita el selector — extraído
// de *torrent.Torrent para poder testear selectFiles sin un cliente real.
type torrentFile struct {
	path   string
	length int64
}

// selectDownloadFiles elige qué ficheros del torrent copiar a la biblioteca.
func selectDownloadFiles(t *torrent.Torrent) []string {
	files := make([]torrentFile, 0, len(t.Files()))
	for _, f := range t.Files() {
		files = append(files, torrentFile{path: f.Path(), length: f.Length()})
	}
	return selectFiles(files)
}

// selectFiles se queda con los vídeos (todos: un pack de temporada trae varios
// episodios) y sus subtítulos sidecar, descartando samples y basura. Si no
// reconoce ningún vídeo cae al fichero más grande, para no copiar nada vacío.
func selectFiles(files []torrentFile) []string {
	var keep []string
	hasVideo := false
	var largest torrentFile
	for _, f := range files {
		if f.length > largest.length {
			largest = f
		}
		if isSamplePath(f.path) {
			continue
		}
		switch ext := strings.ToLower(filepath.Ext(f.path)); {
		case videoExts[ext]:
			keep = append(keep, f.path)
			hasVideo = true
		case subtitleExts[ext]:
			keep = append(keep, f.path)
		}
	}
	if hasVideo {
		return keep
	}
	if largest.path != "" {
		return []string{largest.path}
	}
	return nil
}
