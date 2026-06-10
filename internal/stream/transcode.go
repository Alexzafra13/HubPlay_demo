package stream

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"hubplay/internal/procutil"
)

// Session representa un proceso ffmpeg activo. Los campos cmd/cancel/done
// son internos al paquete; el lifecycle se controla vía Stop. El tracking
// de qué sesiones existen vive en Manager.sessions — el Transcoder es
// stateless (audit LL 2026-05-14).
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

// Transcoder spawnea procesos ffmpeg. Stateless: ni map ni mutex — cada
// llamada a Start/RestartAt arranca un nuevo proceso y devuelve la
// Session resultante. La responsabilidad de tracking (qué sesiones
// existen, cuándo pararlas) es del Manager.
type Transcoder struct {
	baseDir          string
	ffmpeg           string
	transcodeTimeout time.Duration
	hwAccel          HWAccelType
	encoder          string
	libx264Preset    string
	logger           *slog.Logger
}

// TranscoderConfig agrupa los parámetros de NewTranscoder.
// Campos opcionales: FFmpegPath ("ffmpeg"), TranscodeTimeout (4h),
// Encoder ("libx264"), Libx264Preset ("veryfast").
type TranscoderConfig struct {
	BaseDir          string
	FFmpegPath       string
	TranscodeTimeout time.Duration
	HWAccel          HWAccelType
	Encoder          string
	Libx264Preset    string
	Logger           *slog.Logger
}

// NewTranscoder crea un Transcoder. Defaults aplicados a campos vacíos
// de cfg (ver TranscoderConfig).
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
		baseDir:          cfg.BaseDir,
		ffmpeg:           ffmpegPath,
		transcodeTimeout: transcodeTimeout,
		hwAccel:          cfg.HWAccel,
		encoder:          encoder,
		libx264Preset:    libx264Preset,
		logger:           cfg.Logger.With("module", "transcoder"),
	}
}

// Start arranca una sesión HLS. El caller llena los campos caller-side
// de req; los 4 campos transcoder-side (OutputDir, HWAccel, Encoder,
// Libx264Preset) se sobrescriben con el estado interno del Transcoder.
//
// El Transcoder no controla unicidad por sessionID — el caller (Manager)
// garantiza vía singleflight que no haya dos spawns concurrentes para la
// misma key.
func (t *Transcoder) Start(sessionID, itemID string, req TranscodeRequest) (*Session, error) {
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
	// Run ffmpeg in its own process group and kill the whole group on
	// ctx-cancel (Stop / timeout), so VAAPI/NVENC or protocol helper
	// subprocesses can't outlive the session as orphans holding a slot
	// or a GPU context. WaitDelay bounds cmd.Wait() in case a grandchild
	// keeps an output pipe open after the group SIGKILL.
	procutil.SetProcessGroup(cmd)
	cmd.Cancel = func() error { return procutil.KillProcessGroup(cmd) }
	cmd.WaitDelay = 10 * time.Second

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

	t.logger.Info("transcoding started",
		"session", sessionID,
		"item", itemID,
		"profile", req.Profile.Name,
		"start_time", req.StartTime,
	)

	return session, nil
}

// RestartAt arranca un nuevo proceso ffmpeg en el offset dado SIN borrar
// los segmentos previos (útil para seeks hacia atrás dentro del rango
// ya codificado). A diferencia de Start no toca el outputDir.
//
// El caller es responsable de detener la sesión anterior (cancel + drain
// del done) antes de llamar a RestartAt — el Transcoder no lo hace por
// él.
func (t *Transcoder) RestartAt(sessionID, itemID string, req TranscodeRequest) (*Session, error) {
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
	// Same process-group teardown as Start (see rationale there).
	procutil.SetProcessGroup(cmd)
	cmd.Cancel = func() error { return procutil.KillProcessGroup(cmd) }
	cmd.WaitDelay = 10 * time.Second

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

	t.logger.Info("transcoding restarted",
		"session", sessionID,
		"item", itemID,
		"start_time", req.StartTime,
		"start_segment", req.StartSegmentNumber,
	)
	return session, nil
}

// Stop cancela el proceso ffmpeg y borra los ficheros de salida.
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

// ManifestPath returns the path to the HLS master playlist.
func (s *Session) ManifestPath() string {
	return filepath.Join(s.OutputDir, "stream.m3u8")
}

// SegmentPath returns the path to a specific segment file.
func (s *Session) SegmentPath(index int) string {
	return filepath.Join(s.OutputDir, fmt.Sprintf("segment%05d.ts", index))
}

// TranscodeRequest agrupa los parámetros de `BuildFFmpegArgs` en un
// único valor pasable. Cierra el olor F14-2-a del audit 2026-05-14
// (función de 192 LoC con 13 parámetros posicionales): mover a struct
// permite añadir/renombrar campos sin tocar los 18+ callers en tests,
// y deja los call-sites legibles ("CopyVideo: true" vs un `true` en
// la octava posición). Documentación de cada campo en los comentarios
// inline; los detalles del comportamiento ffmpeg viven en
// `BuildFFmpegArgs` justo debajo.
type TranscodeRequest struct {
	// Input es la ruta absoluta al fichero fuente. Se prefija con
	// `file:` en el args para que ffmpeg lo trate como filename
	// aunque empiece por `-`.
	Input string
	// OutputDir es el directorio donde aterrizan `stream.m3u8` +
	// `segmentNNNNN.ts`.
	OutputDir string
	// Profile contiene resolution, video/audio bitrate y framerate
	// del preset elegido (720p, 480p, etc.).
	Profile Profile
	// StartTime es el offset en segundos para `-ss`. 0 = desde el
	// principio (no se emite el flag).
	StartTime float64
	// HWAccel selecciona el acelerador de decode + frame transfer.
	// HWAccelNone para path software; los HW paths añaden flags
	// `-hwaccel ...` antes de `-i`.
	HWAccel HWAccelType
	// Encoder es el output encoder ("libx264", "h264_nvenc", ...).
	// Empty fallback a "libx264".
	Encoder string
	// Libx264Preset es el `-preset` de libx264. Ignorado para los
	// HW encoders (cada uno tiene su propio namespace de preset).
	// Empty fallback a "veryfast".
	Libx264Preset string
	// CopyVideo / CopyAudio piden stream-copy del track
	// correspondiente. Usado por DirectStream cuando el codec
	// source ya es compatible con el cliente y sólo el container
	// o el track hermano necesitan trabajo. El profile "original"
	// fuerza ambos a true por compatibilidad histórica.
	CopyVideo bool
	CopyAudio bool
	// ToneMap (sólo relevante con CopyVideo=false) prepende un
	// chain zscale → tonemap(hable) → zscale al video filter,
	// convirtiendo HDR PQ/HLG a BT.709 SDR antes del scale + pad.
	// Skipped en stream-copy paths (no hay decoded frame que
	// filtrar). La decisión ya routea HDR-para-SDR-client al
	// branch de full-transcode.
	ToneMap bool
	// StartSegmentNumber es el valor de `-start_number` HLS. Una
	// sesión de first-play pasa 0; una seek-restart pasa el index
	// del segmento que corresponde al nuevo StartTime para que
	// los `.ts` producidos coincidan con la sintetizada VOD
	// manifest ya servida al cliente.
	StartSegmentNumber int
	// AudioStreamIndex < 0 → ffmpeg auto-pick (default audio
	// track del fichero). >= 0 → emite explícito
	// `-map 0:v:0 -map 0:a:<index>` para que la elección de dub
	// del usuario seleccione una pista concreta. Es el index
	// per-type que usa ffmpeg, NO el absolute stream id.
	AudioStreamIndex int
	// BurnSub (opcional, nil = sin burn-in) controla burn-in de
	// subtítulos para codecs PGS/DVDSUB/ASS que el browser no
	// puede renderer nativamente. Setearlo fuerza CopyVideo=false
	// (overlay necesita decoded frames) y cambia el filter
	// strategy:
	//   - bitmap codecs (PGS, DVDSUB, ...) → -filter_complex
	//     con overlay
	//   - styled text (ASS / SSA)           → -vf con subtitles=
	//     prepended
	// El subtítulo es permanente para los segmentos resultantes,
	// así que el caller DEBE incluir la elección en su session
	// key (sessionKey() ya lo hace para audio; idem extensión
	// para subs).
	BurnSub *BurnSubtitleSpec
}

// BuildFFmpegArgs constructs FFmpeg arguments for HLS transcoding.
//
// La conversión a struct (`TranscodeRequest`) cerró el olor F14-2-a
// del audit 2026-05-14. El cuerpo no cambia: el switch HW/encoder/
// copy/tonemap/subburn sigue ahí, sólo cambia la firma. Ver la
// documentación de cada campo en el doc-comment de `TranscodeRequest`
// arriba; los detalles que sobreviven aquí abajo son los que dependen
// de cómo varios campos interactúan (ej. legacy "original" profile).
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

	// Legacy shortcut: callers passing the "original" profile expect
	// both streams copied without thinking about the flags.
	if req.Profile.Name == "original" {
		copyVideo = true
		copyAudio = true
	}

	// Burn-in requires re-encoding the video stream — there's no
	// decoded frame to composite onto when we're just remuxing.
	// Force the flag here so callers that flipped both knobs by
	// mistake get the safe behaviour.
	if req.BurnSub != nil {
		copyVideo = false
	}
	useFilterComplex := req.BurnSub != nil && IsImageSubtitleCodec(req.BurnSub.Codec)
	useSubtitlesFilter := req.BurnSub != nil && IsStyledTextSubtitleCodec(req.BurnSub.Codec)

	args := []string{
		"-hide_banner",
		"-loglevel", "warning",
	}

	// Hardware-accelerated decode flags go BEFORE -i. Skipped for
	// libx264 / VideoToolbox (the latter only provides an encoder,
	// no decoder pipeline worth declaring here). For pure stream-copy
	// runs (copyVideo=true) we also skip them — there is no decode
	// happening, so an accel context just adds setup cost for nothing.
	if !copyVideo {
		args = append(args, HWAccelInputArgs(req.HWAccel)...)
	}

	// Seek if needed
	if req.StartTime > 0 {
		args = append(args, "-ss", strconv.FormatFloat(req.StartTime, 'f', 3, 64))
	}

	// Prefix the input with `file:` so ffmpeg parses it as a local
	// filename even if the path itself begins with `-`. Without the
	// prefix a media file named e.g. `-loglevel.mp4` (perfectly legal
	// on most filesystems) would be interpreted as the start of a new
	// flag and break or, worse, take effect. The scanner produces
	// absolute paths so this normally can't happen, but the cost of
	// the prefix is one extra word in the args list and the upside is
	// that we never have to think about it again.
	args = append(args, "-i", "file:"+req.Input)

	// Audio + video stream selection.
	//
	// Plain path: when `audioStreamIndex >= 0`, pin both video (always
	// the first stream) and the chosen audio track. ffmpeg's default
	// stream picker gets confused once any -map is present, so we
	// must declare video too.
	//
	// Filter-complex path (bitmap subtitle burn-in): -map "[burned]"
	// is added AFTER the -filter_complex flag is emitted lower down,
	// because the label only exists once the filter graph defines it.
	// Audio still needs an explicit map here (filter_complex disables
	// the default stream picker for ALL streams) — we use 0:a:0? with
	// the trailing `?` so a video-only source doesn't fail the start.
	if useFilterComplex {
		if req.AudioStreamIndex >= 0 {
			args = append(args, "-map", fmt.Sprintf("0:a:%d", req.AudioStreamIndex))
		} else {
			// Default audio, optional. The `?` makes the map non-fatal
			// when the input has no audio track at all (rare but
			// legitimate — silent video).
			args = append(args, "-map", "0:a:0?")
		}
	} else if req.AudioStreamIndex >= 0 {
		args = append(args,
			"-map", "0:v:0",
			"-map", fmt.Sprintf("0:a:%d", req.AudioStreamIndex),
		)
	}

	// Preserve source PTS in the output. Without this flag ffmpeg
	// resets the output's presentation timestamps to 0 on each run,
	// which is fine for a continuous transcode (segments produced in
	// order, manifest entries match) but BREAKS the seek-restart
	// case: a restart at -ss 1776 -start_number 296 produces
	// segment00296.ts with internal PTS [0, 6] while the synthesized
	// VOD manifest has already told the client "segment 296 covers
	// timeline [1776, 1782]". MSE picks up the segment's actual PTS
	// (not the manifest's claim) and ends up with a Frankenstein
	// timeline; hls.js then fires fan-out requests at multiples of
	// the seek target trying to fill what it thinks are buffer holes,
	// which is exactly the +297-segment cadence the user reported on
	// 2026-05-08. -copyts keeps PTS aligned with the source so
	// segment N always lands at timeline N*hls_time, regardless of
	// how many ffmpeg runs produced it. Plex / Jellyfin both apply
	// this for the same reason.
	//
	// Pair this with default `-avoid_negative_ts auto` (already
	// applied by ffmpeg) which corrects the rare case where a
	// keyframe-aligned -ss lands on content with B-frames whose
	// decoder-order PTS is fractionally negative.
	args = append(args, "-copyts")

	// Video
	if copyVideo {
		args = append(args, "-c:v", "copy")
	} else {
		// Encoder-specific tuning. libx264 wants -preset/-tune; the
		// hardware encoders use their own preset names (ffmpeg
		// happily ignores libx264 flags it doesn't understand, but
		// we keep this clean by gating per encoder).
		args = append(args, "-c:v", encoder)
		if encoder == "libx264" {
			args = append(args,
				"-preset", libx264Preset,
				"-tune", "zerolatency",
			)
		}
		// Keyframe forzado en cada frontera de 6s. El manifest VOD
		// sintético y el seek-restart asumen "segmento N cubre
		// [6N, 6N+6)", pero el muxer HLS solo puede cortar en keyframes:
		// sin esto, el keyint por defecto del encoder (~10s en libx264)
		// produce segmentos de 6-10s y el grid del manifest miente —
		// seeks que aterrizan mal y huecos de buffer. La forma
		// prev_forced_t (y no `gte(t,n_forced*6)`) es deliberada:
		// con -copyts los PTS conservan el tiempo de la fuente, así que
		// en un seek-restart `t` arranca en startSegment*6 y la forma
		// n_forced degeneraría forzando keyframe en cada frame hasta
		// alcanzar el offset. Los restarts siempre empiezan en múltiplo
		// de 6 (manager.startSegment), así que anclar al primer frame
		// forzado mantiene la malla alineada en ambos tipos de run.
		// Funciona igual para libx264 y los encoders HW.
		args = append(args,
			"-force_key_frames", "expr:if(isnan(prev_forced_t),1,gte(t,prev_forced_t+6))",
		)
		args = append(args,
			"-b:v", req.Profile.VideoBitrate,
			"-maxrate", req.Profile.VideoBitrate,
			"-bufsize", req.Profile.VideoBitrate,
		)

		vfChain := buildVideoFilterChain(req.Profile, req.ToneMap)

		switch {
		case useFilterComplex:
			// Bitmap subtitle burn-in. Two graph nodes:
			//   1. [0:v] runs through the SDR/HDR vf chain → [scaled]
			//   2. [scaled] + [0:s:N] overlay → [burned]
			// `-map [burned]` here completes the video map that the
			// audio block above already prepared. The subtitle is now
			// permanently baked into the output frames — switching it
			// mid-session requires a fresh transcode (enforced by
			// sessionKey including BurnSubtitleIndex).
			filterComplex := fmt.Sprintf(
				"[0:v]%s[scaled];[scaled][0:s:%d]overlay[burned]",
				vfChain, req.BurnSub.Index,
			)
			args = append(args,
				"-filter_complex", filterComplex,
				"-map", "[burned]",
			)
		case useSubtitlesFilter:
			// Styled-text burn-in (ASS / SSA). The `subtitles` filter
			// re-opens the source file and rasterises the chosen sub
			// stream onto every video frame. Prepended to the chain
			// so it operates on the full-resolution source frames
			// before scale/pad — text stays crisper through the
			// downscale than rendering at output resolution would.
			subPath := ffmpegInputPathEscape(req.BurnSub.InputPath)
			chain := fmt.Sprintf("subtitles=filename='%s':si=%d,%s",
				subPath, req.BurnSub.Index, vfChain)
			args = append(args, "-vf", chain)
		default:
			args = append(args, "-vf", vfChain)
		}

		args = append(args, "-r", strconv.Itoa(req.Profile.MaxFrameRate))
	}

	// Audio
	if copyAudio {
		args = append(args, "-c:a", "copy")
	} else {
		args = append(args,
			"-c:a", "aac",
			"-b:a", req.Profile.AudioBitrate,
			"-ac", "2",
		)
	}

	// HLS output. `-start_number` is parameterised so seek-restart
	// runs produce segments that line up with the indices the
	// synthesized VOD manifest already advertised to the client.
	// `temp_file`: ffmpeg escribe cada segmento como `*.tmp` y renombra
	// al completarlo. Sin él, los .ts crecen in-place con su nombre
	// final y el handler de segmentos (waitForFile acepta Size()>0)
	// puede servir un TS truncado con Content-Length parcial — glitches
	// y stalls de hls.js, sobre todo en la ventana post-seek. El
	// transmux de IPTV ya usa este flag por la misma razón.
	args = append(args,
		"-f", "hls",
		"-hls_time", "6",
		"-hls_list_size", "0",
		"-hls_segment_filename", segmentPattern,
		"-hls_flags", "independent_segments+temp_file",
		"-start_number", strconv.Itoa(req.StartSegmentNumber),
		manifestPath,
	)

	return args
}

// buildVideoFilterChain assembles the value passed to ffmpeg's `-vf`.
//
// Without tonemap (the SDR case): the historical scale-and-letterbox
// chain — scale to the profile dimensions preserving aspect ratio,
// then pad with black bars so the encoder sees exactly profile.Width ×
// profile.Height regardless of source aspect.
//
// With tonemap (HDR source for an SDR client): a zscale-based chain
// that converts PQ / HLG / DolbyVision down to BT.709 before the
// regular scale runs:
//
//   zscale=t=linear:npl=100  → linearise PQ/HLG luma at 100-nit display peak
//   format=gbrpf32le         → float pixel format (tonemap requires it)
//   zscale=p=bt709           → swap primaries to Rec.709 colourspace
//   tonemap=hable            → Hable operator; neutral-look default Plex/Jellyfin use
//   zscale=t=bt709:m=bt709:r=tv → repackage as BT.709 SDR with TV-range
//   format=yuv420p           → drop back to the encoder's expected pixel format
//
// Then the same scale+pad as the SDR path. The whole expression is
// one filter string (commas chain filters); the encoder receives
// 8-bit BT.709 frames identical in shape to a non-HDR source.
//
// Requires ffmpeg built with libzimg (zscale). The `hwaccel` Docker
// target ships it; software builds on most distros do too. If a user
// runs against an ffmpeg without zscale they'll see an "Unrecognized
// option" failure in transcode logs — at which point HDR sources
// simply error rather than rendering wrong, which is the safer
// failure mode of the two.
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
