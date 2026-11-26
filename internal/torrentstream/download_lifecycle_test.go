package torrentstream

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

// Tests del ciclo de vida de descargas-a-biblioteca (T-8 de-dup por
// infohash, T-9 cancelación y timeout sin progreso) contra un cliente
// torrent real pero hermético: sin DHT, sin trackers, sin peers. Nada sale
// de la máquina; los magnets nunca resuelven y los .torrent servidos por
// httptest no tienen quien los siembre.

func init() {
	tuneClientConfig = func(cfg *torrent.ClientConfig) {
		cfg.NoDHT = true
		cfg.DisableTrackers = true
		cfg.ListenPort = 0
		cfg.NoDefaultPortForwarding = true
	}
}

type recordingSink struct {
	mu   sync.Mutex
	jobs []DownloadJob
}

func (s *recordingSink) PublishDownload(j DownloadJob) {
	s.mu.Lock()
	s.jobs = append(s.jobs, j)
	s.mu.Unlock()
}

func (s *recordingSink) snapshot() []DownloadJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]DownloadJob(nil), s.jobs...)
}

func newTestManager(t *testing.T, opts Options) *Manager {
	t.Helper()
	opts.Enabled = true
	if opts.DataDir == "" {
		opts.DataDir = t.TempDir()
	}
	m, err := New(opts, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })
	return m
}

func randomInfoHash(t *testing.T) string {
	t.Helper()
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

// doneChan adapta el onDone de StartDownload a un canal.
func doneChan() (chan error, func(error)) {
	ch := make(chan error, 1)
	return ch, func(err error) { ch <- err }
}

func waitDone(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("download goroutine did not finish")
		return nil
	}
}

func waitHolders(t *testing.T, m *Manager, ih string, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		m.mu.Lock()
		got := m.holders[ih]
		m.mu.Unlock()
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("holders[%s] = %d, want %d", ih, got, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestDownload_CancelWhileResolvingMetadata(t *testing.T) {
	sink := &recordingSink{}
	m := newTestManager(t, Options{MetadataTimeout: time.Minute, Sink: sink})
	ih := randomInfoHash(t)

	done, onDone := doneChan()
	job := m.StartDownload("magnet:?xt=urn:btih:"+ih, t.TempDir(), onDone)
	waitHolders(t, m, ih, 1)

	ok, err := m.RemoveDownload(job.ID)
	if !ok || err != nil {
		t.Fatalf("RemoveDownload(active) = %v, %v; want true, nil", ok, err)
	}
	if got := len(m.Downloads()); got != 0 {
		t.Fatalf("job still listed after cancel: %d jobs", got)
	}
	if err := waitDone(t, done); !errors.Is(err, ErrDownloadCanceled) {
		t.Fatalf("onDone err = %v, want ErrDownloadCanceled", err)
	}
	waitHolders(t, m, ih, 0) // torrent soltado

	// Un job cancelado ya no está en el panel: no debe reaparecer por SSE.
	for _, j := range sink.snapshot() {
		if j.Status == DownloadFailed {
			t.Fatalf("canceled job was published as failed: %+v", j)
		}
	}
}

func TestDownload_RemoveWhileCopyingIsRefused(t *testing.T) {
	m := newTestManager(t, Options{})
	j := &downloadJob{snap: DownloadJob{ID: "copying", Status: DownloadCopying}}
	m.mu.Lock()
	m.downloads[j.snap.ID] = j
	m.mu.Unlock()

	if ok, err := m.RemoveDownload("copying"); ok || !errors.Is(err, ErrDownloadActive) {
		t.Fatalf("RemoveDownload(copying) = %v, %v; want false, ErrDownloadActive", ok, err)
	}
	if ok, err := m.RemoveDownload("missing"); ok || err != nil {
		t.Fatalf("RemoveDownload(missing) = %v, %v; want false, nil", ok, err)
	}
}

// T-8: dos src distintos del mismo contenido → un solo job activo.
func TestDownload_DuplicateInfoHashIsRejected(t *testing.T) {
	m := newTestManager(t, Options{MetadataTimeout: time.Minute})
	ih := randomInfoHash(t)

	firstDone, onFirst := doneChan()
	first := m.StartDownload("magnet:?xt=urn:btih:"+ih+"&dn=uno", t.TempDir(), onFirst)
	waitHolders(t, m, ih, 1)

	dupDone, onDup := doneChan()
	dup := m.StartDownload("magnet:?xt=urn:btih:"+ih+"&dn=dos", t.TempDir(), onDup)
	if dup.ID == first.ID {
		t.Fatal("different src must create a new job (de-dup by src only covers identical src)")
	}
	if err := waitDone(t, dupDone); !errors.Is(err, ErrDuplicateDownload) {
		t.Fatalf("duplicate onDone err = %v, want ErrDuplicateDownload", err)
	}
	// El duplicado no es holder y no ha soltado el torrent del original.
	waitHolders(t, m, ih, 1)

	if _, err := m.RemoveDownload(first.ID); err != nil {
		t.Fatal(err)
	}
	if err := waitDone(t, firstDone); !errors.Is(err, ErrDownloadCanceled) {
		t.Fatalf("first onDone err = %v, want ErrDownloadCanceled", err)
	}
	waitHolders(t, m, ih, 0)
}

// T-9: metadata resuelta pero nadie siembra → falla por falta de progreso
// en vez de vivir para siempre.
func TestDownload_StallTimeout(t *testing.T) {
	m := newTestManager(t, Options{
		AllowPrivateUpstreams: true, // el .torrent se sirve desde loopback
		DownloadStallTimeout:  150 * time.Millisecond,
	})
	m.downloadTick = 10 * time.Millisecond

	src := t.TempDir()
	payload := filepath.Join(src, "Pelicula.Sin.Seeds.2026.mkv")
	if err := os.WriteFile(payload, make([]byte, 64<<10), 0o644); err != nil {
		t.Fatal(err)
	}
	info := metainfo.Info{PieceLength: 16 << 10}
	if err := info.BuildFromFilePath(payload); err != nil {
		t.Fatal(err)
	}
	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	mi := metainfo.MetaInfo{InfoBytes: infoBytes}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-bittorrent")
		_ = mi.Write(w)
	}))
	t.Cleanup(srv.Close)

	done, onDone := doneChan()
	m.StartDownload(srv.URL+"/x.torrent", t.TempDir(), onDone)
	if err := waitDone(t, done); !errors.Is(err, ErrDownloadStalled) {
		t.Fatalf("onDone err = %v, want ErrDownloadStalled", err)
	}
	waitHolders(t, m, mi.HashInfoBytes().HexString(), 0)

	jobs := m.Downloads()
	if len(jobs) != 1 || jobs[0].Status != DownloadFailed {
		t.Fatalf("stalled job should stay listed as failed: %+v", jobs)
	}
}
