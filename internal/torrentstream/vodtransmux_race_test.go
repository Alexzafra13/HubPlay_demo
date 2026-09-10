package torrentstream

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hubplay/internal/probe"
)

// slowProber blocks each Probe until released so several Prepare calls can
// be parked mid-probe deterministically.
type slowProber struct {
	res     *probe.Result
	release chan struct{}
	calls   atomic.Int32
}

func (p *slowProber) Probe(ctx context.Context, _ string) (*probe.Result, error) {
	p.calls.Add(1)
	select {
	case <-p.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return p.res, nil
}

// TestVODPrepare_ConcurrentSameInfohash_SingleTranscode es la regresión del
// hallazgo "dos Prepare concurrentes para el mismo infohash lanzaban dos
// ffmpeg en el mismo workDir y el segundo pisaba la sesión del primero
// (huérfano sin stop)": N llamadas concurrentes deben compartir UN
// arranque, UN probe y devolver todas el mismo resultado HLS.
func TestVODPrepare_ConcurrentSameInfohash_SingleTranscode(t *testing.T) {
	p := &slowProber{res: probeResult("matroska,webm", "h264", "ac3"), release: make(chan struct{})}
	m := newTestVOD(t, p)
	var starts atomic.Int32
	var stopped atomic.Bool
	inner := fakeStarter(&stopped)
	m.start = func(ctx context.Context, args []string, workDir string) (func(), func() error, error) {
		starts.Add(1)
		return inner(ctx, args, workDir)
	}

	const n = 8
	var wg sync.WaitGroup
	results := make([]PlayResult, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = m.Prepare(context.Background(), &fakePlayable{ih: "CAFE"})
		}(i)
	}
	// Give every goroutine time to reach Prepare, then let the probe finish.
	time.Sleep(50 * time.Millisecond)
	close(p.release)
	wg.Wait()

	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("Prepare[%d]: %v", i, errs[i])
		}
		if results[i].Mode != PlayRemux || !results[i].HLS || results[i].InfoHash != "cafe" {
			t.Fatalf("Prepare[%d]: got %+v, want remux/HLS for infohash cafe", i, results[i])
		}
	}
	if got := starts.Load(); got != 1 {
		t.Errorf("transcoder starts = %d, want exactly 1", got)
	}
	if got := p.calls.Load(); got != 1 {
		t.Errorf("probe calls = %d, want exactly 1", got)
	}
	if _, ok := m.PlaylistPath("cafe"); !ok {
		t.Error("shared session should be live after concurrent Prepare")
	}
}

// TestVODPrepare_ConcurrentDirect_NoSessionLeft: cuando la decisión es
// direct-play, los que esperaban en el placeholder reciben ese mismo
// resultado y no queda ninguna sesión registrada.
func TestVODPrepare_ConcurrentDirect_NoSessionLeft(t *testing.T) {
	p := &slowProber{res: probeResult("mov,mp4,m4a", "h264", "aac"), release: make(chan struct{})}
	m := newTestVOD(t, p)

	var wg sync.WaitGroup
	var direct atomic.Int32
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := m.Prepare(context.Background(), &fakePlayable{ih: "d1"})
			if err == nil && res.Mode == PlayDirect && !res.HLS {
				direct.Add(1)
			}
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(p.release)
	wg.Wait()

	if direct.Load() != 4 {
		t.Errorf("all 4 callers should get direct play, got %d", direct.Load())
	}
	m.mu.Lock()
	sessions, readers := len(m.sessions), len(m.readers)
	m.mu.Unlock()
	if sessions != 0 || readers != 0 {
		t.Errorf("direct play must leave no session/reader behind: sessions=%d readers=%d", sessions, readers)
	}
}

// TestVODPrepare_WaiterHonoursContext: un caller que espera en el
// placeholder respeta su propio ctx (no se queda colgado si el probe del
// primero tarda).
func TestVODPrepare_WaiterHonoursContext(t *testing.T) {
	p := &slowProber{res: probeResult("matroska,webm", "h264", "ac3"), release: make(chan struct{})}
	m := newTestVOD(t, p)
	var stopped atomic.Bool
	m.start = fakeStarter(&stopped)

	first := make(chan error, 1)
	go func() {
		_, err := m.Prepare(context.Background(), &fakePlayable{ih: "w1"})
		first <- err
	}()
	time.Sleep(30 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := m.Prepare(ctx, &fakePlayable{ih: "w1"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiter should fail with its ctx error, got %v", err)
	}
	close(p.release)
	if err := <-first; err != nil {
		t.Fatalf("first Prepare: %v", err)
	}
}

// TestVODWatchExit_CrashDropsSession: si ffmpeg muere con error por su
// cuenta (no por Stop), la sesión deja de anunciarse como HLS y se limpia
// el workDir. Un final limpio (EOF del fichero) mantiene la sesión.
func TestVODWatchExit_CrashDropsSession(t *testing.T) {
	m := newTestVOD(t, fakeProber{res: probeResult("matroska,webm", "h264", "ac3")})
	exit := make(chan error, 1)
	m.start = func(_ context.Context, _ []string, workDir string) (func(), func() error, error) {
		_ = os.WriteFile(filepath.Join(workDir, "index.m3u8"), []byte("#EXTM3U\n"), 0o644)
		_ = os.WriteFile(filepath.Join(workDir, "seg-00000.ts"), []byte("ts"), 0o644)
		return func() {}, func() error { return <-exit }, nil
	}
	if _, err := m.Prepare(context.Background(), &fakePlayable{ih: "cr"}); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	pl, ok := m.PlaylistPath("cr")
	if !ok {
		t.Fatal("session should be live")
	}

	exit <- errors.New("exit status 1")
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, ok := m.PlaylistPath("cr"); !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("crashed transcoder should drop the session")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if fileExists(pl) {
		t.Error("work dir should be removed after crash")
	}
}

func TestVODWatchExit_CleanExitKeepsSession(t *testing.T) {
	m := newTestVOD(t, fakeProber{res: probeResult("matroska,webm", "h264", "ac3")})
	exit := make(chan error, 1)
	m.start = func(_ context.Context, _ []string, workDir string) (func(), func() error, error) {
		_ = os.WriteFile(filepath.Join(workDir, "index.m3u8"), []byte("#EXTM3U\n"), 0o644)
		_ = os.WriteFile(filepath.Join(workDir, "seg-00000.ts"), []byte("ts"), 0o644)
		return func() {}, func() error { return <-exit }, nil
	}
	if _, err := m.Prepare(context.Background(), &fakePlayable{ih: "ok"}); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	exit <- nil // whole file transcoded
	time.Sleep(50 * time.Millisecond)
	if _, ok := m.PlaylistPath("ok"); !ok {
		t.Error("clean exit must keep the finished HLS session servable")
	}
}

// TestVODPlaylistPath_TouchesTorrentReader: servir playlist/segmentos debe
// contar como actividad del torrent subyacente (si no, el reaper del
// Manager de torrents lo tiraría bajo un transcode vivo).
func TestVODPlaylistPath_TouchesTorrentReader(t *testing.T) {
	m := newTestVOD(t, fakeProber{res: probeResult("matroska,webm", "h264", "ac3")})
	var stopped atomic.Bool
	m.start = fakeStarter(&stopped)
	fp := &fakePlayable{ih: "tt"}
	if _, err := m.Prepare(context.Background(), fp); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	before := fp.touched.Load()
	m.PlaylistPath("tt")
	m.SegmentPath("tt", "seg-00000.ts")
	if fp.touched.Load() < before+2 {
		t.Errorf("playlist/segment access should touch the torrent reader (before=%d after=%d)", before, fp.touched.Load())
	}
}

func TestVODShutdown_Idempotent(t *testing.T) {
	m := newTestVOD(t, fakeProber{res: probeResult("mov,mp4,m4a", "h264", "aac")})
	m.Shutdown()
	m.Shutdown() // must not panic on the closed reaper channel
}
