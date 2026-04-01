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
	logger           *slog.Logger
}

func NewTranscoder(baseDir, ffmpegPath string, transcodeTimeout time.Duration, logger *slog.Logger) *Transcoder {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	if transcodeTimeout <= 0 {
		transcodeTimeout = 4 * time.Hour
	}
	return &Transcoder{
		sessions:         make(map[string]*Session),
		baseDir:          baseDir,
		ffmpeg:           ffmpegPath,
		transcodeTimeout: transcodeTimeout,
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

	args := BuildFFmpegArgs(inputPath, outputDir, profile, startTime)
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
func BuildFFmpegArgs(input, outputDir string, profile Profile, startTime float64) []string {
	manifestPath := filepath.Join(outputDir, "stream.m3u8")
	segmentPattern := filepath.Join(outputDir, "segment%05d.ts")

	args := []string{
		"-hide_banner",
		"-loglevel", "warning",
	}

	// Seek if needed
	if startTime > 0 {
		args = append(args, "-ss", strconv.FormatFloat(startTime, 'f', 3, 64))
	}

	args = append(args, "-i", input)

	// Video encoding
	if profile.Name == "original" {
		args = append(args, "-c:v", "copy")
	} else {
		args = append(args,
			"-c:v", "libx264",
			"-preset", "veryfast",
			"-tune", "zerolatency",
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
