package torrentstream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"hubplay/internal/probe"
)

// VODTransmux turns a torrent's main file into something a browser can play.
// It probes the file and either reports DirectPlay (serve /torrent/stream as
// is), spawns a cheap `-c copy` ffmpeg remux into HLS (PlayRemux), or reports
// Reencode (PlayReencode, not yet wired — P1b-2).
//
// It is the VOD sibling of the IPTV transmux: same ffmpeg→HLS pattern, but
// fed from our OWN in-process torrent reader (over a loopback HTTP server so
// ffmpeg/ffprobe get Range/seek) instead of an external upstream. No UA
// spoofing, no reconnect-at-eof, no live sliding window — the playlist is an
// event list that keeps every segment so the title stays seekable while it
// downloads.
type VODTransmux struct {
	workRoot    string
	prober      probe.Prober
	logger      *slog.Logger
	idleTimeout time.Duration

	// reencode settings: encoder name + decode-side hwaccel flags (from
	// stream.DetectHWAccel), and whether reencode is allowed at all.
	encoder          string
	hwAccelInputArgs []string
	allowReencode    bool

	// start is the seam that actually launches the transcoder; overridden in
	// tests so the lifecycle can be exercised without a real ffmpeg.
	start transcodeStarter

	// loopback serves torrent readers to ffmpeg/ffprobe at baseURL.
	srv     *http.Server
	baseURL string

	mu       sync.Mutex
	readers  map[string]Playable    // infohash → reader source (for the loopback)
	sessions map[string]*vodSession // infohash → running remux

	now          func() time.Time
	reaperStop   chan struct{}
	reaperDone   chan struct{}
	shutdownOnce sync.Once
}

// Playable is the slice of *Session the transmux needs. Exported so the HTTP
// handler can name it (it passes a *Session, which satisfies this).
type Playable interface {
	InfoHash() string
	FileName() string
	Length() int64
	Reader() io.ReadSeekCloser
	Touch()
}

// transcodeStarter runs ffmpeg with the given argv (the manager builds them
// for remux or reencode) and returns a stop func plus a wait func. stop kills
// the process and blocks (bounded) until it has exited so the work dir can be
// removed safely; wait blocks until the process exits and returns its error
// (nil on a clean EOF). The default implementation execs ffmpeg; tests
// inject a fake.
type transcodeStarter func(ctx context.Context, args []string, workDir string) (stop func(), wait func() error, err error)

// vodSession is one remux/reencode session. It is registered in
// VODTransmux.sessions BEFORE the probe runs (as a placeholder) so that two
// concurrent Prepare calls for the same infohash share one transcode instead
// of spawning two ffmpegs into the same work dir; `ready` closes once the
// startup decision is final (result/err populated).
type vodSession struct {
	infoHash   string
	workDir    string
	stop       func()
	lastAccess atomicTime

	ready  chan struct{} // closed when result/err are final
	result PlayResult
	err    error

	// stopping is set by Stop before killing ffmpeg so the exit watcher can
	// tell an operator/reaper kill apart from a crash.
	stopping atomic.Bool
}

// PlayResult tells the handler how to deliver a source.
type PlayResult struct {
	Mode PlayMode
	// HLS is true when a transcode (remux OR reencode) is running and the
	// content should be played from the HLS playlist. False for direct play
	// and for reencode that was declined (disabled).
	HLS      bool
	InfoHash string // for HLS, the key under which the playlist/segments live
}

// VODConfig configures the transmux. Zero values get sane defaults.
type VODConfig struct {
	WorkRoot    string
	FFmpegBin   string
	Prober      probe.Prober
	IdleTimeout time.Duration
	// Encoder + HWAccelInputArgs select the reencode encoder (from
	// stream.DetectHWAccel); empty Encoder defaults to software libx264.
	Encoder          string
	HWAccelInputArgs []string
	// AllowReencode gates the CPU-heavy reencode path. Default true.
	AllowReencode bool
}

// NewVODTransmux builds the manager and starts its loopback reader server +
// idle reaper.
func NewVODTransmux(cfg VODConfig, logger *slog.Logger) (*VODTransmux, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 5 * time.Minute
	}
	if cfg.WorkRoot == "" {
		cfg.WorkRoot = filepath.Join(os.TempDir(), "hubplay-vod")
	}
	if cfg.Prober == nil {
		cfg.Prober = probe.New()
	}
	if err := os.MkdirAll(cfg.WorkRoot, 0o755); err != nil {
		return nil, fmt.Errorf("torrentstream: vod work dir: %w", err)
	}

	m := &VODTransmux{
		workRoot:         cfg.WorkRoot,
		prober:           cfg.Prober,
		logger:           logger,
		idleTimeout:      cfg.IdleTimeout,
		encoder:          cfg.Encoder,
		hwAccelInputArgs: cfg.HWAccelInputArgs,
		allowReencode:    cfg.AllowReencode,
		readers:          make(map[string]Playable),
		sessions:         make(map[string]*vodSession),
		now:              time.Now,
		reaperStop:       make(chan struct{}),
		reaperDone:       make(chan struct{}),
	}
	m.start = m.startFFmpeg
	if err := m.startLoopback(); err != nil {
		return nil, err
	}
	go m.reapLoop()
	return m, nil
}

// loopbackKeyRe matches the /r/{infohash} loopback path.
var loopbackKeyRe = regexp.MustCompile(`^/r/([a-fA-F0-9]+)$`)

// startLoopback binds a 127.0.0.1 server that serves registered torrent
// readers with Range support so ffmpeg/ffprobe can seek. It is loopback-only
// and carries no auth — it is never exposed beyond the host.
func (m *VODTransmux) startLoopback() error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("torrentstream: vod loopback listen: %w", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/r/", m.serveReader)
	m.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	m.baseURL = "http://" + ln.Addr().String()
	go func() { _ = m.srv.Serve(ln) }()
	return nil
}

func (m *VODTransmux) serveReader(w http.ResponseWriter, r *http.Request) {
	mm := loopbackKeyRe.FindStringSubmatch(r.URL.Path)
	if mm == nil {
		http.NotFound(w, r)
		return
	}
	m.mu.Lock()
	p := m.readers[strings.ToLower(mm[1])]
	m.mu.Unlock()
	if p == nil {
		http.NotFound(w, r)
		return
	}
	reader := p.Reader()
	defer reader.Close() //nolint:errcheck
	// Touch on every read so the torrent session isn't reaped as "idle" while
	// ffmpeg is actively streaming from it (ffmpeg keeps one long GET open and
	// reads incrementally, so per-read touches fire throughout the transcode).
	tr := &touchReader{ReadSeeker: reader, touch: p.Touch}
	http.ServeContent(w, r, p.FileName(), time.Now(), tr)
}

// touchReader marks the underlying session used on every read.
type touchReader struct {
	io.ReadSeeker
	touch func()
}

func (t *touchReader) Read(b []byte) (int, error) {
	t.touch()
	return t.ReadSeeker.Read(b)
}

// Prepare probes the session's file and returns how to play it, spawning the
// remux transcoder when needed. Safe to call repeatedly for the same content
// (an active remux session is reused).
func (m *VODTransmux) Prepare(ctx context.Context, sess Playable) (PlayResult, error) {
	ih := strings.ToLower(sess.InfoHash())

	// Already transcoding (or deciding) this content → join that session.
	// Registering the placeholder under the lock is what guarantees a single
	// ffmpeg per infohash: a second caller that arrives mid-probe waits on
	// `ready` instead of probing + spawning again into the same work dir.
	m.mu.Lock()
	if s, ok := m.sessions[ih]; ok {
		m.mu.Unlock()
		return m.awaitSession(ctx, s)
	}
	s := &vodSession{infoHash: ih, ready: make(chan struct{})}
	s.lastAccess.set(m.now())
	m.sessions[ih] = s
	m.readers[ih] = sess
	m.mu.Unlock()

	// finish publishes the outcome to any waiter. keep=false drops the
	// placeholder (direct play, declined reencode, error): nothing keeps
	// running for this infohash.
	finish := func(res PlayResult, err error, keep bool) (PlayResult, error) {
		if !keep {
			m.mu.Lock()
			if m.sessions[ih] == s {
				delete(m.sessions, ih)
			}
			delete(m.readers, ih)
			m.mu.Unlock()
		}
		s.result, s.err = res, err
		close(s.ready)
		return res, err
	}

	info, err := m.probe(ctx, ih)
	if err != nil {
		return finish(PlayResult{}, fmt.Errorf("torrentstream: vod probe: %w", err), false)
	}
	mode := DecidePlayMode(info)

	// Reencode is opt-out: a 4K HEVC software transcode is brutal on weak
	// hosts, so an operator can disable it and have those sources reported as
	// unsupported instead.
	if mode == PlayReencode && !m.allowReencode {
		return finish(PlayResult{Mode: PlayReencode, InfoHash: ih}, nil, false)
	}

	switch mode {
	case PlayRemux, PlayReencode:
		if err := m.startTranscode(ctx, s, mode, info); err != nil {
			return finish(PlayResult{}, err, false)
		}
		return finish(PlayResult{Mode: mode, HLS: true, InfoHash: ih}, nil, true)
	default:
		// Direct play needs no running transcoder.
		return finish(PlayResult{Mode: mode, InfoHash: ih}, nil, false)
	}
}

// awaitSession blocks until the session's startup decision is final and
// returns it. A session that ended up as HLS is touched so the join counts
// as activity for the reaper.
func (m *VODTransmux) awaitSession(ctx context.Context, s *vodSession) (PlayResult, error) {
	select {
	case <-s.ready:
	case <-ctx.Done():
		return PlayResult{}, ctx.Err()
	}
	if s.err != nil {
		return PlayResult{}, s.err
	}
	if s.result.HLS {
		s.lastAccess.set(m.now())
	}
	return s.result, nil
}

func (m *VODTransmux) probe(ctx context.Context, ih string) (MediaInfo, error) {
	res, err := m.prober.Probe(ctx, m.baseURL+"/r/"+ih)
	if err != nil {
		return MediaInfo{}, err
	}
	return mediaInfoFromProbe(res), nil
}

// mediaInfoFromProbe extracts the play-decision inputs from a probe result:
// the container and the primary (non-cover-art) video + first audio codec.
func mediaInfoFromProbe(res *probe.Result) MediaInfo {
	mi := MediaInfo{Container: res.Format.FormatName}
	for _, s := range res.Streams {
		switch s.CodecType {
		case "video":
			if mi.VideoCodec == "" && !s.IsAttachedPic {
				mi.VideoCodec = s.CodecName
			}
		case "audio":
			if mi.AudioCodec == "" {
				mi.AudioCodec = s.CodecName
			}
		}
	}
	return mi
}

func (m *VODTransmux) startTranscode(ctx context.Context, s *vodSession, mode PlayMode, info MediaInfo) error {
	ih := s.infoHash
	workDir := filepath.Join(m.workRoot, ih)
	// A previous session for this infohash may have left files behind (e.g.
	// a Windows "file in use" during cleanup); start from a clean dir so a
	// stale playlist from the old run can't satisfy waitFirstSegment.
	_ = os.RemoveAll(workDir)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return fmt.Errorf("torrentstream: vod session dir: %w", err)
	}
	inputURL := m.baseURL + "/r/" + ih
	var args []string
	if mode == PlayReencode {
		args = buildVODReencodeArgs(inputURL, workDir, m.encoder, m.hwAccelInputArgs)
	} else {
		args = buildVODRemuxArgs(inputURL, workDir, AudioNeedsTranscode(info.AudioCodec))
	}
	// Detached context: the transcode outlives the request that triggered it
	// (other viewers join the same HLS output); the reaper stops it on idle.
	stop, wait, err := m.start(context.WithoutCancel(ctx), args, workDir)
	if err != nil {
		_ = os.RemoveAll(workDir)
		return fmt.Errorf("torrentstream: vod start: %w", err)
	}
	// The placeholder was registered by Prepare; fill in the live bits under
	// the lock so Stop/reaper see a consistent session.
	m.mu.Lock()
	s.workDir = workDir
	s.stop = stop
	m.mu.Unlock()
	go m.watchExit(s, wait)

	// Wait for the first segment so the handler can return a playable
	// playlist (bounded; the caller's ctx also applies).
	if err := m.waitFirstSegment(ctx, workDir); err != nil {
		m.Stop(ih)
		return err
	}
	return nil
}

// watchExit tears the session down if ffmpeg dies on its own with an error.
// A clean exit (whole file transcoded) keeps the session: the playlist and
// segments on disk stay servable until the idle reaper collects them. A
// kill requested via Stop is not a crash (stopping flag).
func (m *VODTransmux) watchExit(s *vodSession, wait func() error) {
	err := wait()
	if err == nil || s.stopping.Load() {
		return
	}
	m.mu.Lock()
	live := m.sessions[s.infoHash] == s
	m.mu.Unlock()
	if !live {
		return
	}
	m.logger.Warn("torrentstream: vod transcoder exited with error; dropping session",
		"infohash", s.infoHash, "error", err)
	m.Stop(s.infoHash)
}

// waitFirstSegment polls until ffmpeg has written at least one segment and the
// playlist exists, or ctx/timeout fires.
func (m *VODTransmux) waitFirstSegment(ctx context.Context, workDir string) error {
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	playlist := filepath.Join(workDir, "index.m3u8")
	for {
		if fileExists(playlist) && hasSegment(workDir) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("torrentstream: vod transcode produced no segments in time")
		case <-tick.C:
		}
	}
}

// PlaylistPath returns the index.m3u8 path for an active remux session and
// touches it (keeps it alive). ok=false when there's no such session.
func (m *VODTransmux) PlaylistPath(infohash string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.liveSessionLocked(infohash)
	if !ok {
		return "", false
	}
	return filepath.Join(s.workDir, "index.m3u8"), true
}

// liveSessionLocked returns the session for infohash if its transcoder is
// running (startup finished successfully) and touches both it and the
// underlying torrent session. Touching the torrent reader matters: the
// torrent Manager's idle reaper only sees reads, and a viewer playing
// already-written segments generates none — without this the torrent could
// be dropped under a live transcode. Caller holds m.mu.
func (m *VODTransmux) liveSessionLocked(infohash string) (*vodSession, bool) {
	ih := strings.ToLower(infohash)
	s, ok := m.sessions[ih]
	if !ok || s.workDir == "" {
		return nil, false
	}
	s.lastAccess.set(m.now())
	if p := m.readers[ih]; p != nil {
		p.Touch()
	}
	return s, true
}

// SegmentPath returns the on-disk path of a validated segment for an active
// session and touches it. Callers MUST pass IsValidSegmentName-checked names.
func (m *VODTransmux) SegmentPath(infohash, segment string) (string, bool) {
	if !IsValidSegmentName(segment) {
		return "", false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.liveSessionLocked(infohash)
	if !ok {
		return "", false
	}
	return filepath.Join(s.workDir, segment), true
}

// Stop tears down a remux session: kills the transcoder (waiting for it to
// exit so the files are no longer held open), drops the loopback reader and
// removes the work dir.
func (m *VODTransmux) Stop(infohash string) {
	ih := strings.ToLower(infohash)
	m.mu.Lock()
	s := m.sessions[ih]
	delete(m.sessions, ih)
	delete(m.readers, ih)
	m.mu.Unlock()
	if s == nil {
		return
	}
	s.stopping.Store(true)
	if s.stop != nil {
		s.stop()
	}
	if s.workDir == "" {
		return // placeholder that never started a transcoder
	}
	if err := os.RemoveAll(s.workDir); err != nil {
		m.logger.Warn("torrentstream: vod cleanup failed", "infohash", ih, "error", err)
	}
}

// Shutdown stops the reaper, all sessions and the loopback server. Idempotent.
func (m *VODTransmux) Shutdown() {
	m.shutdownOnce.Do(func() {
		close(m.reaperStop)
		<-m.reaperDone
		m.mu.Lock()
		keys := make([]string, 0, len(m.sessions))
		for ih := range m.sessions {
			keys = append(keys, ih)
		}
		m.mu.Unlock()
		for _, ih := range keys {
			m.Stop(ih)
		}
		if m.srv != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = m.srv.Shutdown(ctx)
		}
	})
}

func (m *VODTransmux) reapLoop() {
	defer close(m.reaperDone)
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-m.reaperStop:
			return
		case <-tick.C:
			m.reapIdle(m.now())
		}
	}
}

func (m *VODTransmux) reapIdle(now time.Time) {
	var idle []string
	m.mu.Lock()
	for ih, s := range m.sessions {
		if now.Sub(s.lastAccess.get()) > m.idleTimeout {
			idle = append(idle, ih)
		}
	}
	m.mu.Unlock()
	for _, ih := range idle {
		m.Stop(ih)
		m.logger.Info("torrentstream: vod session reaped (idle)", "infohash", ih)
	}
}

// startFFmpeg is the default transcodeStarter: it launches ffmpeg with the
// given argv and returns a stop func that kills it.
func (m *VODTransmux) startFFmpeg(ctx context.Context, args []string, workDir string) (func(), func() error, error) {
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	stderr := newVODStderr(m.logger, filepath.Base(workDir))
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		_ = stderr.Close()
		return nil, nil, err
	}
	exited := make(chan struct{})
	var waitErr error
	go func() {
		waitErr = cmd.Wait()
		// Closing the pipe writer lets the stderr scanner goroutine finish;
		// without it one goroutine per transcode stays blocked in Read.
		_ = stderr.Close()
		close(exited)
	}()
	stop := func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		// Kill is asynchronous: wait (bounded) so the caller can remove the
		// work dir without racing a still-writing ffmpeg (Windows would
		// fail with "file in use"; Linux could leak a late .tmp segment).
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			m.logger.Warn("torrentstream: vod ffmpeg did not exit after kill", "workDir", workDir)
		}
	}
	wait := func() error {
		<-exited
		return waitErr
	}
	return stop, wait, nil
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func hasSegment(workDir string) bool {
	entries, err := os.ReadDir(workDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && IsValidSegmentName(e.Name()) {
			return true
		}
	}
	return false
}
