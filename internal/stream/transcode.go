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

// Session representa una sesión de transcoding activa.
// Los campos cmd/cancel/done son internos; Manager.sessions es la vista pública.
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

// Transcoder gestiona procesos ffmpeg.
type Transcoder struct {
	mu               sync.Mutex
	sessions         map[string]*Session
	baseDir          string
	ffmpeg           string
	transcodeTimeout time.Duration
	hwAccel       HWAccelType
	encoder       string
	libx264Preset string
	logger        *slog.Logger
}

// TranscoderConfig agrupa los parámetros de NewTranscoder.
// Campos vacíos usan defaults: FFmpegPath="ffmpeg", TranscodeTimeout=4h,
// Encoder="libx264", Libx264Preset="veryfast".
type TranscoderConfig struct {
	BaseDir          string
	FFmpegPath       string
	TranscodeTimeout time.Duration
	HWAccel          HWAccelType
	Encoder          string
	Libx264Preset    string
	Logger           *slog.Logger
}

// NewTranscoder crea un Transcoder con defaults para campos vacíos.
func NewTranscoder(cfg TranscoderConfig) *Transcoder {
	ffmpegPath := cfg.FFmpegPath
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	transcodeTimeout := cfg.TranscodeTimeout
	if transcodeTimeout <= 0 {
		transcodeTimeout = 4 * time.Hour
	}
	encoder := cfg.Encoder
	if encoder == "" {
		encoder = "libx264"
	}
	libx264Preset := cfg.Libx264Preset
	if libx264Preset == "" {
		libx264Preset = "veryfast"
	}
	return &Transcoder{
		sessions:         make(map[string]*Session),
		baseDir:          cfg.BaseDir,
		ffmpeg:           ffmpegPath,
		transcodeTimeout: transcodeTimeout,
		hwAccel:          cfg.HWAccel,
		encoder:          encoder,
		libx264Preset:    libx264Preset,
		logger:           cfg.Logger.With("module", "transcoder"),
	}
}

// Start inicia una sesión HLS. Los campos transcoder-side de req
// (OutputDir, HWAccel, Encoder, Libx264Preset) se sobrescriben aquí.
func (t *Transcoder) Start(sessionID, itemID string, req TranscodeRequest) (*Session, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if existing, ok := t.sessions[sessionID]; ok {
		existing.Stop()
		delete(t.sessions, sessionID)
	}

	outputDir := filepath.Join(t.baseDir, sessionID)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("creating output dir: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), t.transcodeTimeout)

	req.OutputDir = outputDir
	req.HWAccel = t.hwAccel
	req.Encoder = t.encoder
	req.Libx264Preset = t.libx264Preset

	args := BuildFFmpegArgs(req)
	cmd := exec.CommandContext(ctx, t.ffmpeg, args...)
	cmd.Dir = outputDir

	session := &Session{
		ID:        sessionID,
		ItemID:    itemID,
		Profile:   req.Profile,
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
		"profile", req.Profile.Name,
		"start_time", req.StartTime,
	)

	return session, nil
}

// RestartAt reemplaza el proceso ffmpeg sin borrar segmentos previos
// (útil para seeks). NO borra el outputDir a diferencia de Start.
func (t *Transcoder) RestartAt(sessionID, itemID string, req TranscodeRequest) (*Session, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	outputDir := filepath.Join(t.baseDir, sessionID)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("ensuring output dir: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), t.transcodeTimeout)

	req.OutputDir = outputDir
	req.HWAccel = t.hwAccel
	req.Encoder = t.encoder
	req.Libx264Preset = t.libx264Preset

	args := BuildFFmpegArgs(req)
	cmd := exec.CommandContext(ctx, t.ffmpeg, args...)
	cmd.Dir = outputDir

	session := &Session{
		ID:        sessionID,
		ItemID:    itemID,
		Profile:   req.Profile,
		OutputDir: outputDir,
		StartedAt: time.Now(),
		cmd:       cmd,
		cancel:    cancel,
		done:      make(chan struct{}),
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("restarting ffmpeg: %w", err)
	}

	go func() {
		defer close(session.done)
		if err := cmd.Wait(); err != nil {
			t.logger.Debug("ffmpeg restart process ended", "session", sessionID, "error", err)
		}
	}()

	t.sessions[sessionID] = session
	t.logger.Info("transcoding restarted",
		"session", sessionID,
		"item", itemID,
		"start_time", req.StartTime,
		"start_segment", req.StartSegmentNumber,
	)
	return session, nil
}

func (t *Transcoder) GetSession(sessionID string) (*Session, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s, ok := t.sessions[sessionID]
	return s, ok
}

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

func (t *Transcoder) ActiveSessions() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.sessions)
}

func (s *Session) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
	}
	_ = os.RemoveAll(s.OutputDir)
}

func (s *Session) ManifestPath() string {
	return filepath.Join(s.OutputDir, "stream.m3u8")
}

func (s *Session) SegmentPath(index int) string {
	return filepath.Join(s.OutputDir, fmt.Sprintf("segment%05d.ts", index))
}

// TranscodeRequest agrupa los parámetros de BuildFFmpegArgs.
//
// Contrato de llenado: el caller rellena los campos de contenido
// (Input, Profile, StartTime, CopyVideo/Audio, ToneMap, AudioStreamIndex,
// BurnSub); el Transcoder sobreescribe los 4 de infraestructura
// (OutputDir, HWAccel, Encoder, Libx264Preset) en Start/RestartAt.
type TranscodeRequest struct {
	Input     string
	OutputDir string
	Profile   Profile
	// StartTime: offset en seg para -ss. 0 = desde el principio.
	StartTime float64
	HWAccel   HWAccelType
	Encoder   string // default "libx264"
	// Libx264Preset: ignorado para HW encoders. Default "veryfast".
	Libx264Preset string
	// CopyVideo/CopyAudio: stream-copy del track. El profile "original"
	// fuerza ambos a true.
	CopyVideo bool
	CopyAudio bool
	// ToneMap: inserta chain zscale+tonemap HDR→SDR. Solo con CopyVideo=false.
	ToneMap bool
	// StartSegmentNumber: -start_number HLS. En seek-restart coincide
	// con el segmento de la VOD manifest ya servida al cliente.
	StartSegmentNumber int
	// AudioStreamIndex: >=0 fuerza -map 0:a:<N> (per-type index, NO
	// absolute stream id). <0 deja el auto-pick de ffmpeg.
	AudioStreamIndex int
	// BurnSub: nil = sin burn-in. Fuerza CopyVideo=false y elige
	// filter_complex (bitmap) o subtitles= (ASS/SSA) según codec.
	BurnSub *BurnSubtitleSpec
}

// BuildFFmpegArgs construye los argumentos ffmpeg para transcoding HLS.
// Semántica de campos en el doc-comment de TranscodeRequest.
func BuildFFmpegArgs(req TranscodeRequest) []string {
	encoder := req.Encoder
	if encoder == "" {
		encoder = "libx264"
	}
	libx264Preset := req.Libx264Preset
	if libx264Preset == "" {
		libx264Preset = "veryfast"
	}
	manifestPath := filepath.Join(req.OutputDir, "stream.m3u8")
	segmentPattern := filepath.Join(req.OutputDir, "segment%05d.ts")

	copyVideo := req.CopyVideo
	copyAudio := req.CopyAudio

	// Perfil "original" implica copy de ambos streams.
	if req.Profile.Name == "original" {
		copyVideo = true
		copyAudio = true
	}

	// Burn-in necesita decoded frames; forzar re-encode.
	if req.BurnSub != nil {
		copyVideo = false
	}
	useFilterComplex := req.BurnSub != nil && IsImageSubtitleCodec(req.BurnSub.Codec)
	useSubtitlesFilter := req.BurnSub != nil && IsStyledTextSubtitleCodec(req.BurnSub.Codec)

	args := []string{
		"-hide_banner",
		"-loglevel", "warning",
	}

	// Flags de HW-accel van antes de -i; innecesarios en stream-copy.
	if !copyVideo {
		args = append(args, HWAccelInputArgs(req.HWAccel)...)
	}

	if req.StartTime > 0 {
		args = append(args, "-ss", strconv.FormatFloat(req.StartTime, 'f', 3, 64))
	}

	// Prefijo `file:` evita que rutas que empiecen por `-` se interpreten como flags.
	args = append(args, "-i", "file:"+req.Input)

	// Selección de streams. En filter_complex el -map "[burned]" se
	// añade después; aquí solo mapeamos audio. El `?` evita fallo
	// en vídeos sin pista de audio.
	if useFilterComplex {
		if req.AudioStreamIndex >= 0 {
			args = append(args, "-map", fmt.Sprintf("0:a:%d", req.AudioStreamIndex))
		} else {
			args = append(args, "-map", "0:a:0?")
		}
	} else if req.AudioStreamIndex >= 0 {
		args = append(args,
			"-map", "0:v:0",
			"-map", fmt.Sprintf("0:a:%d", req.AudioStreamIndex),
		)
	}

	// -copyts: sin esto, un seek-restart reinicia los PTS a 0 y los
	// segmentos no cuadran con la VOD manifest sintética. Con -copyts
	// el PTS siempre refleja la posición real en el fichero fuente.
	args = append(args, "-copyts")

	if copyVideo {
		args = append(args, "-c:v", "copy")
	} else {
		args = append(args, "-c:v", encoder)
		if encoder == "libx264" {
			args = append(args,
				"-preset", libx264Preset,
				"-tune", "zerolatency",
			)
		}
		args = append(args,
			"-b:v", req.Profile.VideoBitrate,
			"-maxrate", req.Profile.VideoBitrate,
			"-bufsize", req.Profile.VideoBitrate,
		)

		vfChain := buildVideoFilterChain(req.Profile, req.ToneMap)

		switch {
		case useFilterComplex:
			// Burn-in bitmap: [0:v]→vfChain→[scaled] + [0:s:N]→overlay→[burned]
			filterComplex := fmt.Sprintf(
				"[0:v]%s[scaled];[scaled][0:s:%d]overlay[burned]",
				vfChain, req.BurnSub.Index,
			)
			args = append(args,
				"-filter_complex", filterComplex,
				"-map", "[burned]",
			)
		case useSubtitlesFilter:
			// ASS/SSA: subtitles= antes del scale para mejor nitidez.
			subPath := ffmpegInputPathEscape(req.BurnSub.InputPath)
			chain := fmt.Sprintf("subtitles=filename='%s':si=%d,%s",
				subPath, req.BurnSub.Index, vfChain)
			args = append(args, "-vf", chain)
		default:
			args = append(args, "-vf", vfChain)
		}

		args = append(args, "-r", strconv.Itoa(req.Profile.MaxFrameRate))
	}

	if copyAudio {
		args = append(args, "-c:a", "copy")
	} else {
		args = append(args,
			"-c:a", "aac",
			"-b:a", req.Profile.AudioBitrate,
			"-ac", "2",
		)
	}

	args = append(args,
		"-f", "hls",
		"-hls_time", "6",
		"-hls_list_size", "0",
		"-hls_segment_filename", segmentPattern,
		"-hls_flags", "independent_segments",
		"-start_number", strconv.Itoa(req.StartSegmentNumber),
		manifestPath,
	)

	return args
}

// buildVideoFilterChain monta la cadena -vf: scale+pad (SDR) o
// zscale→tonemap(hable)→scale+pad (HDR→SDR). El tonemap requiere
// libzimg; sin ella ffmpeg falla (preferible a renderizar mal).
func buildVideoFilterChain(profile Profile, toneMap bool) string {
	scalePad := fmt.Sprintf(
		"scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2",
		profile.Width, profile.Height, profile.Width, profile.Height,
	)
	if !toneMap {
		return scalePad
	}
	const tonemapChain = "zscale=t=linear:npl=100,format=gbrpf32le,zscale=p=bt709,tonemap=hable,zscale=t=bt709:m=bt709:r=tv,format=yuv420p"
	return tonemapChain + "," + scalePad
}
