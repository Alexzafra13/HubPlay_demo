package stream

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"hubplay/internal/procutil"
)

// Remux por segmentos independientes (DirectStream, `-c:v copy`).
//
// En vez de un ffmpeg largo que se mata y relanza en cada salto (y cuyos
// cortes dependen de dónde arrancó), cada segmento del SegmentPlan se
// genera con una invocación corta y autocontenida (~60-90 ms medidos):
// ir adelante o atrás es la misma operación, y el segmento i siempre
// contiene exactamente [Start(i), Start(i+1)).
//
// La receta de ffmpeg está validada fotograma a fotograma (2026-11-26)
// con MKV y MP4, GOP cerrado y abierto, B-frames y keyframes irregulares:
// juntando todos los segmentos no falta ni se repite ningún fotograma, la
// reproducción seguida decodifica sin errores y cada segmento suelto
// arranca con SPS/PPS. Las piezas, cada una por un fallo observado:
//
//   - `-copyts`: tiempos absolutos de la fuente; los segmentos encajan
//     entre sí sin importar qué proceso generó cada uno.
//   - `-ss <keyframe>` de entrada: salto rápido. En MKV puede caer en un
//     keyframe ANTERIOR (el índice de cues no tiene todos): lo sobrante
//     se descarta con el filtro `noise=drop`, no con un `-ss` de salida
//     (que pondría los tiempos a cero).
//   - `noise=drop` por tiempo de presentación con la ventana
//     [Lead_i, Lead_{i+1}): corta en orden de decodificación (como HLS),
//     así los fotogramas RASL de un GOP abierto van con su keyframe.
//   - `-to` con margen: `-to` corta por DTS y con B-frames se colaría el
//     keyframe siguiente; solo sirve para dejar de leer.
//   - `-avoid_negative_ts disabled`: con B-frames el primer DTS es
//     negativo y ffmpeg desplazaba SOLO el segmento 0 (+83 ms respecto al
//     resto). MPEG-TS ya tiene su margen de 1,4 s.
//   - `*_mp4toannexb,dump_extra=freq=keyframe`: un keyframe no IDR (GOP
//     abierto) no lleva SPS/PPS; sin repetirlas, un reproductor que
//     empieza en ese segmento tras un salto no puede decodificar nada.
//   - Audio transcodificado (AC3/DTS → AAC): cada segmento tiene su
//     propio codificador; arrancan 2 s antes de la frontera y sobre una
//     rejilla común de tramas AAC (1024 muestras a 48 kHz) para que el
//     final de un segmento y el principio del siguiente encajen muestra
//     a muestra (sin ello: una discontinuidad por frontera).

// remuxSpec describe qué sale en cada segmento.
type remuxSpec struct {
	Input      string
	VideoCodec string
	// AudioOrdinal es la pista de audio (índice por tipo) o -1 si el
	// fichero no tiene audio.
	AudioOrdinal  int
	CopyAudio     bool
	AudioBitrate  string
	AudioChannels int
}

const (
	// remuxBoundaryEpsilon: los tiempos del índice vienen de ffprobe con
	// 6 decimales; un fotograma justo en la frontera podría quedar
	// fuera de los dos segmentos por redondeo. 1 ms es menos que la
	// separación entre fotogramas (4 ms a 240 fps) y que una trama AAC.
	remuxBoundaryEpsilon = 0.001
	// remuxReadMargin: cuánto se lee de más tras el final del segmento
	// (el corte real lo hace noise=drop).
	remuxReadMargin = 2.0
	// remuxAudioPreroll: el codificador AAC arranca este tiempo antes de
	// la frontera para llegar "en caliente".
	remuxAudioPreroll = 2.0
	// aacFrameSeconds: duración de una trama AAC a 48 kHz.
	aacFrameSeconds = 1024.0 / 48000.0
)

func ffTime(t float64) string { return strconv.FormatFloat(t, 'f', 6, 64) }

// remuxVideoBSF devuelve la cadena de filtros de paquetes de vídeo.
func remuxVideoBSF(codec, drop string) string {
	prefix := ""
	switch strings.ToLower(codec) {
	case "h264", "avc", "avc1":
		prefix = "h264_mp4toannexb,"
	case "hevc", "h265":
		prefix = "hevc_mp4toannexb,"
	}
	return prefix + "dump_extra=freq=keyframe,noise=drop=" + drop
}

// BuildRemuxSegmentArgs construye los argumentos de ffmpeg para generar
// el segmento i del plan en out (MPEG-TS).
func BuildRemuxSegmentArgs(spec remuxSpec, plan *SegmentPlan, i int, out string) []string {
	start := plan.Start(i)
	last := plan.IsLast(i)
	// La coma va escapada: separa filtros en la lista de -bsf.
	drop := "lt(pts*tb\\," + ffTime(start-remuxBoundaryEpsilon) + ")"
	if !last {
		drop += "+gte(pts*tb\\," + ffTime(plan.End(i)-remuxBoundaryEpsilon) + ")"
	}

	seek := plan.Seek(i)
	var audioT0 float64
	transcodeAudio := spec.AudioOrdinal >= 0 && !spec.CopyAudio
	if transcodeAudio {
		audioT0 = math.Floor((start-remuxAudioPreroll)/aacFrameSeconds) * aacFrameSeconds
		if audioT0 < 0 {
			audioT0 = 0
		}
		// Leer desde antes del arranque del codificador; el vídeo
		// sobrante lo descarta noise=drop.
		if s := audioT0 - 1; s < seek {
			seek = s
		}
	}
	if seek < 0 {
		seek = 0
	}

	args := []string{"-hide_banner", "-nostdin", "-loglevel", "error", "-copyts"}
	if seek > 0 {
		args = append(args, "-ss", ffTime(seek))
	}
	args = append(args, "-i", "file:"+spec.Input)
	if !last {
		args = append(args, "-to", ffTime(plan.End(i)+remuxReadMargin))
	}
	args = append(args, "-map", "0:V:0")
	if spec.AudioOrdinal >= 0 {
		args = append(args, "-map", fmt.Sprintf("0:a:%d", spec.AudioOrdinal))
	}
	args = append(args, "-c:v", "copy")
	if spec.AudioOrdinal >= 0 {
		if transcodeAudio {
			channels := spec.AudioChannels
			if channels <= 0 {
				channels = 2
			}
			bitrate := spec.AudioBitrate
			if bitrate == "" {
				bitrate = "192k"
			}
			// asetpts cuenta muestras desde T0: tiempos exactos sobre la
			// rejilla aunque la fuente tenga jitter al principio.
			af := fmt.Sprintf("aresample=48000,atrim=start=%s,asetpts=N/SR/TB+%s/TB", ffTime(audioT0), ffTime(audioT0))
			args = append(args, "-af", af, "-c:a", "aac", "-b:a", bitrate, "-ac", strconv.Itoa(channels))
		} else {
			args = append(args, "-c:a", "copy")
		}
		args = append(args, "-bsf:a", "noise=drop="+drop)
	}
	args = append(args,
		"-avoid_negative_ts", "disabled",
		"-bsf:v", remuxVideoBSF(spec.VideoCodec, drop),
		"-f", "mpegts",
		out,
	)
	return args
}

// errSegmentOutOfRange: el cliente pidió un índice que el plan no tiene.
var errSegmentOutOfRange = errors.New("stream: segment index out of range")

// segmentRemuxer genera y cachea en disco los segmentos de una sesión.
type segmentRemuxer struct {
	ffmpeg string
	spec   remuxSpec
	plan   *SegmentPlan
	dir    string
	logger *slog.Logger

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	sem    chan struct{}

	mu       sync.Mutex
	inflight map[int]*segmentFlight
}

type segmentFlight struct {
	done chan struct{}
	err  error
}

const (
	// remuxConcurrency: ffmpeg simultáneos por sesión (petición +
	// prefetch). Cada uno solo lee unos MB.
	remuxConcurrency = 2
	// remuxPrefetch: segmentos que se preparan por delante del pedido.
	remuxPrefetch = 3
	// remuxSegmentTimeout: techo por segmento (disco dormido, NAS lento).
	remuxSegmentTimeout = 60 * time.Second
)

func newSegmentRemuxer(ffmpeg string, spec remuxSpec, plan *SegmentPlan, dir string, logger *slog.Logger) *segmentRemuxer {
	if ffmpeg == "" {
		ffmpeg = "ffmpeg"
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &segmentRemuxer{
		ffmpeg:   ffmpeg,
		spec:     spec,
		plan:     plan,
		dir:      dir,
		logger:   logger,
		ctx:      ctx,
		cancel:   cancel,
		sem:      make(chan struct{}, remuxConcurrency),
		inflight: make(map[int]*segmentFlight),
	}
}

func (r *segmentRemuxer) path(i int) string {
	return filepath.Join(r.dir, fmt.Sprintf("segment%05d.ts", i))
}

func fileReady(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Size() > 0
}

// Ensure devuelve la ruta del segmento i, generándolo si hace falta, y
// pone en marcha el prefetch de los siguientes. La generación sigue
// aunque ctx (la petición) se cancele: es barata y el siguiente intento
// la encontrará hecha.
func (r *segmentRemuxer) Ensure(ctx context.Context, i int) (string, error) {
	if i < 0 || i >= r.plan.Count() {
		return "", errSegmentOutOfRange
	}
	p := r.path(i)
	if !fileReady(p) {
		f := r.flight(i)
		if f == nil {
			return "", r.ctx.Err()
		}
		select {
		case <-f.done:
		case <-ctx.Done():
			return "", ctx.Err()
		}
		if f.err != nil {
			return "", f.err
		}
	}
	for j := i + 1; j <= i+remuxPrefetch && j < r.plan.Count(); j++ {
		if !fileReady(r.path(j)) {
			r.flight(j)
		}
	}
	return p, nil
}

// flight devuelve la generación en curso del segmento i o lanza una.
// nil si el remuxer está parado.
func (r *segmentRemuxer) flight(i int) *segmentFlight {
	r.mu.Lock()
	defer r.mu.Unlock()
	if f, ok := r.inflight[i]; ok {
		return f
	}
	if r.ctx.Err() != nil {
		return nil
	}
	f := &segmentFlight{done: make(chan struct{})}
	r.inflight[i] = f
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		f.err = r.produce(i)
		r.mu.Lock()
		delete(r.inflight, i)
		r.mu.Unlock()
		close(f.done)
	}()
	return f
}

func (r *segmentRemuxer) produce(i int) error {
	select {
	case r.sem <- struct{}{}:
	case <-r.ctx.Done():
		return r.ctx.Err()
	}
	defer func() { <-r.sem }()

	final := r.path(i)
	if fileReady(final) {
		return nil
	}
	tmp := final + ".tmp"
	ctx, cancel := context.WithTimeout(r.ctx, remuxSegmentTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.ffmpeg, BuildRemuxSegmentArgs(r.spec, r.plan, i, tmp)...)
	procutil.SetProcessGroup(cmd)
	cmd.Cancel = func() error { return procutil.KillProcessGroup(cmd) }
	cmd.WaitDelay = 5 * time.Second
	tail := &stderrTail{}
	cmd.Stderr = tail
	if err := cmd.Run(); err != nil {
		_ = os.Remove(tmp)
		if r.ctx.Err() != nil {
			return r.ctx.Err()
		}
		r.logger.Warn("remux segment failed", "segment", i, "error", err, "stderr_tail", tail.String())
		return fmt.Errorf("remux segment %d: %w", i, err)
	}
	if !fileReady(tmp) {
		_ = os.Remove(tmp)
		return fmt.Errorf("remux segment %d: empty output", i)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("remux segment %d: %w", i, err)
	}
	return nil
}

// Stop cancela las generaciones en curso y espera a que terminen.
func (r *segmentRemuxer) Stop() {
	r.cancel()
	r.wg.Wait()
}
