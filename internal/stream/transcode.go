package stream

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// Session represents an active transcoding session.
type Session struct {
	ID        string
	ItemID    string
	Profile   Profile
	OutputDir string
	StartedAt time.Time
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	done      chan struct{}
}

// Transcoder manages FFmpeg transcoding sessions.
type Transcoder struct {
	mu               sync.Mutex
	sessions         map[string]*Session // keyed by session ID
	baseDir          string              // base directory for transcoded segments
	ffmpeg           string              // path to ffmpeg binary
	transcodeTimeout time.Duration       // max duration per transcode process
	// hwAccel is the detected hardware acceleration kind (vaapi,
	// nvenc, …) chosen at startup. Empty / HWAccelNone means software
	// encode via libx264. The transcoder doesn't re-detect; this is
	// set once and read on every session.
	hwAccel HWAccelType
	encoder string // ffmpeg encoder name, e.g. "h264_nvenc" or "libx264"
	logger  *slog.Logger
}

// NewTranscoder constructs a transcoder. Pass `HWAccelNone` and an
// empty `encoder` to force software encoding (libx264); pass the
// values from `DetectHWAccel` to use the platform's accelerator.
func NewTranscoder(baseDir, ffmpegPath string, transcodeTimeout time.Duration, hwAccel HWAccelType, encoder string, logger *slog.Logger) *Transcoder {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	if transcodeTimeout <= 0 {
		transcodeTimeout = 4 * time.Hour
	}
	if encoder == "" {
		encoder = "libx264"
	}
	return &Transcoder{
		sessions:         make(map[string]*Session),
		baseDir:          baseDir,
		ffmpeg:           ffmpegPath,
		transcodeTimeout: transcodeTimeout,
		hwAccel:          hwAccel,
		encoder:          encoder,
		logger:           logger.With("module", "transcoder"),
	}
}

// Start begins a new HLS transcoding session.
func (t *Transcoder) Start(sessionID, itemID, inputPath string, profile Profile, startTime float64) (*Session, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Clean up existing session for this ID
	if existing, ok := t.sessions[sessionID]; ok {
		existing.Stop()
		delete(t.sessions, sessionID)
	}

	outputDir := filepath.Join(t.baseDir, sessionID)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("creating output dir: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), t.transcodeTimeout)

	args := BuildFFmpegArgs(inputPath, outputDir, profile, startTime, t.hwAccel, t.encoder)
	cmd := exec.CommandContext(ctx, t.ffmpeg, args...)
	cmd.Dir = outputDir

	session := &Session{
		ID:        sessionID,
		ItemID:    itemID,
		Profile:   profile,
		OutputDir: outputDir,
		StartedAt: time.Now(),
		cmd:       cmd,
		cancel:    cancel,
		done:      make(chan struct{}),
	}

	if err := cmd.Start(); err != nil {
		cancel()
		_ = os.RemoveAll(outputDir)
		return nil, fmt.Errorf("starting ffmpeg: %w", err)
	}

	go func() {
		defer close(session.done)
		if err := cmd.Wait(); err != nil {
			t.logger.Debug("ffmpeg process ended", "session", sessionID, "error", err)
		}
	}()

	t.sessions[sessionID] = session
	t.logger.Info("transcoding started",
		"session", sessionID,
		"item", itemID,
		"profile", profile.Name,
		"start_time", startTime,
	)

	return session, nil
}

// GetSession returns an active session by ID.
func (t *Transcoder) GetSession(sessionID string) (*Session, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s, ok := t.sessions[sessionID]
	return s, ok
}

// Stop terminates a transcoding session and cleans up.
func (t *Transcoder) Stop(sessionID string) {
	t.mu.Lock()
	s, ok := t.sessions[sessionID]
	if ok {
		delete(t.sessions, sessionID)
	}
	t.mu.Unlock()

	if ok {
		s.Stop()
		t.logger.Info("transcoding stopped", "session", sessionID)
	}
}

// StopAll terminates all active sessions.
func (t *Transcoder) StopAll() {
	t.mu.Lock()
	sessions := make([]*Session, 0, len(t.sessions))
	for _, s := range t.sessions {
		sessions = append(sessions, s)
	}
	t.sessions = make(map[string]*Session)
	t.mu.Unlock()

	for _, s := range sessions {
		s.Stop()
	}
	t.logger.Info("all transcoding sessions stopped", "count", len(sessions))
}

// ActiveSessions returns the number of active sessions.
func (t *Transcoder) ActiveSessions() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.sessions)
}

// Stop terminates the transcoding process and cleans up output files.
func (s *Session) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	// Wait for process to finish (with timeout)
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
	}
	_ = os.RemoveAll(s.OutputDir)
}

// ManifestPath returns the path to the HLS master playlist.
func (s *Session) ManifestPath() string {
	return filepath.Join(s.OutputDir, "stream.m3u8")
}

// SegmentPath returns the path to a specific segment file.
func (s *Session) SegmentPath(index int) string {
	return filepath.Join(s.OutputDir, fmt.Sprintf("segment%05d.ts", index))
}

// BuildFFmpegArgs constructs FFmpeg arguments for HLS transcoding.
//
// `hwAccel` selects the input-side acceleration (decode + frame
// transfer) and `encoder` selects the output encoder. Pass
// `HWAccelNone, "libx264"` for the software path. The hardware paths
// don't change the video filter chain — frames are downloaded to
// system memory after decode, scaled in software, then uploaded by
// the encoder. That's slower than a fully-on-device pipeline (vaapi
// scale_vaapi etc.) but works without rewriting the filter graph and
// matches what Plex/Jellyfin do for "general purpose" HW transcoding.
func BuildFFmpegArgs(input, outputDir string, profile Profile, startTime float64, hwAccel HWAccelType, encoder string) []string {
	if encoder == "" {
		encoder = "libx264"
	}
	manifestPath := filepath.Join(outputDir, "stream.m3u8")
	segmentPattern := filepath.Join(outputDir, "segment%05d.ts")

	args := []string{
		"-hide_banner",
		"-loglevel", "warning",
	}

	// Hardware-accelerated decode flags go BEFORE -i. Skipped for
	// libx264 / VideoToolbox (the latter only provides an encoder,
	// no decoder pipeline worth declaring here).
	args = append(args, HWAccelInputArgs(hwAccel)...)

	// Seek if needed
	if startTime > 0 {
		args = append(args, "-ss", strconv.FormatFloat(startTime, 'f', 3, 64))
	}

	// Prefix the input with `file:` so ffmpeg parses it as a local
	// filename even if the path itself begins with `-`. Without the
	// prefix a media file named e.g. `-loglevel.mp4` (perfectly legal
	// on most filesystems) would be interpreted as the start of a new
	// flag and break or, worse, take effect. The scanner produces
	// absolute paths so this normally can't happen, but the cost of
	// the prefix is one extra word in the args list and the upside is
	// that we never have to think about it again.
	args = append(args, "-i", "file:"+input)

	// Video encoding
	if profile.Name == "original" {
		args = append(args, "-c:v", "copy")
	} else {
		// Encoder-specific tuning. libx264 wants -preset/-tune; the
		// hardware encoders use their own preset names (ffmpeg
		// happily ignores libx264 flags it doesn't understand, but
		// we keep this clean by gating per encoder).
		args = append(args, "-c:v", encoder)
		if encoder == "libx264" {
			args = append(args,
				"-preset", "veryfast",
				"-tune", "zerolatency",
			)
		}
		args = append(args,
			"-b:v", profile.VideoBitrate,
			"-maxrate", profile.VideoBitrate,
			"-bufsize", profile.VideoBitrate,
			"-vf", fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2",
				profile.Width, profile.Height, profile.Width, profile.Height),
			"-r", strconv.Itoa(profile.MaxFrameRate),
		)
	}

	// Audio encoding
	if profile.Name == "original" {
		args = append(args, "-c:a", "copy")
	} else {
		args = append(args,
			"-c:a", "aac",
			"-b:a", profile.AudioBitrate,
			"-ac", "2",
		)
	}

	// HLS output
	args = append(args,
		"-f", "hls",
		"-hls_time", "6",
		"-hls_list_size", "0",
		"-hls_segment_filename", segmentPattern,
		"-hls_flags", "independent_segments",
		"-start_number", "0",
		manifestPath,
	)

	return args
}
