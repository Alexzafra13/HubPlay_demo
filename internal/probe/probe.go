package probe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type Result struct {
	Format   Format
	Streams  []Stream
	Chapters []Chapter
}

// Chapter: Start/End absolutos desde el origen del fichero. Title vacío si
// ffprobe no encontró `tags.title` (común en chapters auto-generados por
// Handbrake/MakeMKV vs. los manuales).
type Chapter struct {
	Start time.Duration
	End   time.Duration
	Title string
}

type Format struct {
	Filename   string
	Duration   time.Duration
	Size       int64
	BitRate    int
	FormatName string
}

type Stream struct {
	Index             int
	CodecType         string // video, audio, subtitle
	CodecName         string
	Profile           string
	BitRate           int
	Width             int
	Height            int
	FrameRate         float64
	HDRType           string
	ColorSpace        string
	Channels          int
	SampleRate        int
	Language          string
	Title             string
	IsDefault         bool
	IsForced          bool
	IsHearingImpaired bool
	// IsAttachedPic marca los "video streams" que en realidad son la
	// carátula embebida (cover art de MP3/FLAC/M4A, mjpeg/png). No son
	// reproducibles: tratarlos como pista de vídeo real hacía que un
	// fichero de música cayera a "transcode completo" y contaminaba el
	// listado de pistas del UI. PB-24 (audit 2026-06-10).
	IsAttachedPic bool
}

// DurationTicks: 10 000 ticks = 1 ms, 10 M ticks = 1 s.
func DurationTicks(d time.Duration) int64 {
	return d.Microseconds() * 10
}

func TicksToDuration(ticks int64) time.Duration {
	return time.Duration(ticks/10) * time.Microsecond
}

// Prober: interfaz para poder mockear ffprobe en tests.
type Prober interface {
	Probe(ctx context.Context, path string) (*Result, error)
}

type FFprobe struct {
	BinPath string // binario ffprobe; default "ffprobe"
}

func New() *FFprobe {
	return &FFprobe{BinPath: "ffprobe"}
}

// probeTimeout acota cada invocación de ffprobe cuando el caller no
// trae deadline propio. Un ffprobe sano tarda <1s; el techo cubre NFS
// lentos sin permitir que un fichero colgado (FIFO disfrazado, mount
// muerto, contenedor corrupto) bloquee el scan secuencial de la
// biblioteca para siempre. PB-11 (audit 2026-06-10).
const probeTimeout = 60 * time.Second

func (f *FFprobe) Probe(ctx context.Context, path string) (*Result, error) {
	bin := f.BinPath
	if bin == "" {
		bin = "ffprobe"
	}

	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, probeTimeout)
		defer cancel()
	}

	// `-v error` (no `quiet`): con quiet, ExitError.Stderr llega vacío
	// y todo fallo se loguea como un "exit status 1" inservible.
	cmd := exec.CommandContext(ctx, bin,
		"-v", "error",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		"-show_chapters",
		path,
	)

	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			detail := strings.TrimSpace(string(exitErr.Stderr))
			if len(detail) > 512 {
				detail = detail[len(detail)-512:]
			}
			return nil, fmt.Errorf("ffprobe %q: %w: %s", path, err, detail)
		}
		return nil, fmt.Errorf("ffprobe %q: %w", path, err)
	}

	return parseOutput(out)
}

// ffprobeOutput: shape JSON de ffprobe.
type ffprobeOutput struct {
	Format   ffprobeFormat    `json:"format"`
	Streams  []ffprobeStream  `json:"streams"`
	Chapters []ffprobeChapter `json:"chapters"`
}

type ffprobeChapter struct {
	StartTime string         `json:"start_time"`
	EndTime   string         `json:"end_time"`
	Tags      map[string]any `json:"tags"`
}

type ffprobeFormat struct {
	Filename   string `json:"filename"`
	Duration   string `json:"duration"`
	Size       string `json:"size"`
	BitRate    string `json:"bit_rate"`
	FormatName string `json:"format_name"`
}

type ffprobeStream struct {
	Index         int               `json:"index"`
	CodecType     string            `json:"codec_type"`
	CodecName     string            `json:"codec_name"`
	Profile       string            `json:"profile"`
	BitRate       string            `json:"bit_rate"`
	Width         int               `json:"width"`
	Height        int               `json:"height"`
	RFrameRate    string            `json:"r_frame_rate"`
	ColorTransfer string            `json:"color_transfer"`
	ColorSpace    string            `json:"color_space"`
	Channels      int               `json:"channels"`
	SampleRate    string            `json:"sample_rate"`
	Tags          map[string]any    `json:"tags"`
	Disposition   map[string]int    `json:"disposition"`
	SideDataList  []ffprobeSideData `json:"side_data_list"`
}

// ffprobeSideData captura las entradas de `side_data_list` que nos
// interesan. El "DOVI configuration record" es donde ffprobe anuncia
// Dolby Vision de verdad — el campo `profile` del stream casi nunca lo
// menciona (solo algunos MKV remux). Las entradas tienen campos
// heterogéneos según el tipo; los que no aplican quedan en cero.
type ffprobeSideData struct {
	SideDataType string `json:"side_data_type"`
	DVProfile    int    `json:"dv_profile"`
	// DVBLCompatID es `dv_bl_signal_compatibility_id`: qué puede hacer
	// un reproductor sin soporte DV con la base layer. 1/6 = HDR10,
	// 2 = SDR, 4 = HLG, 0 = nada (DV puro, ej. Profile 5 de WEB-DLs).
	DVBLCompatID int `json:"dv_bl_signal_compatibility_id"`
}

func parseOutput(data []byte) (*Result, error) {
	var raw ffprobeOutput
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse ffprobe output: %w", err)
	}

	result := &Result{
		Format: Format{
			Filename:   raw.Format.Filename,
			Size:       parseInt64(raw.Format.Size),
			BitRate:    parseInt(raw.Format.BitRate),
			FormatName: raw.Format.FormatName,
		},
	}

	if dur, err := strconv.ParseFloat(raw.Format.Duration, 64); err == nil {
		result.Format.Duration = time.Duration(dur * float64(time.Second))
	}

	for _, s := range raw.Streams {
		stream := Stream{
			Index:      s.Index,
			CodecType:  s.CodecType,
			CodecName:  s.CodecName,
			Profile:    s.Profile,
			BitRate:    parseInt(s.BitRate),
			Width:      s.Width,
			Height:     s.Height,
			FrameRate:  parseFrameRate(s.RFrameRate),
			ColorSpace: s.ColorSpace,
			Channels:   s.Channels,
			SampleRate: parseInt(s.SampleRate),
		}

		stream.HDRType = detectHDR(s.ColorTransfer, s.Profile, s.SideDataList)

		if s.Tags != nil {
			if lang, ok := s.Tags["language"].(string); ok {
				stream.Language = lang
			}
			if title, ok := s.Tags["title"].(string); ok {
				stream.Title = title
			}
		}

		if s.Disposition != nil {
			stream.IsDefault = s.Disposition["default"] == 1
			stream.IsForced = s.Disposition["forced"] == 1
			stream.IsHearingImpaired = s.Disposition["hearing_impaired"] == 1
			stream.IsAttachedPic = s.Disposition["attached_pic"] == 1
		}

		result.Streams = append(result.Streams, stream)
	}

	// Chapters: ffprobe emite start/end_time como string de segundos
	// ("42.000000"). Si falla el parse de uno, dropea sólo ese chapter —
	// mejor perder un marker que toda la metadata del stream.
	for _, c := range raw.Chapters {
		start, err1 := strconv.ParseFloat(c.StartTime, 64)
		end, err2 := strconv.ParseFloat(c.EndTime, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		ch := Chapter{
			Start: time.Duration(start * float64(time.Second)),
			End:   time.Duration(end * float64(time.Second)),
		}
		if c.Tags != nil {
			if t, ok := c.Tags["title"].(string); ok {
				ch.Title = t
			}
		}
		result.Chapters = append(result.Chapters, ch)
	}

	return result, nil
}

func detectHDR(colorTransfer, profile string, sideData []ffprobeSideData) string {
	// El DOVI configuration record manda sobre color_transfer: un DV
	// Profile 5 anuncia smpte2084 y se etiquetaba HDR10, pero su señal
	// es IPTPQc2 (ICtCp) — reproducirlo o tonemapearlo como PQ normal
	// da los colores verde/morado clásicos. Con base layer compatible
	// (8.1→HDR10, 8.4→HLG, 8.2→SDR) etiquetamos la base: cualquier
	// reproductor sin DV la decodifica bien ignorando la capa RPU.
	// PB-23 (audit 2026-06-10).
	for _, sd := range sideData {
		if !strings.Contains(sd.SideDataType, "DOVI") {
			continue
		}
		switch sd.DVBLCompatID {
		case 1, 6:
			return "HDR10"
		case 4:
			return "HLG"
		case 2:
			return "" // base layer SDR: reproducible en cualquier cliente
		default:
			return "DolbyVision" // sin base compatible (Profile 5 y afines)
		}
	}
	switch {
	case colorTransfer == "smpte2084":
		return "HDR10"
	case colorTransfer == "arib-std-b67":
		return "HLG"
	case strings.Contains(strings.ToLower(profile), "dolby vision"):
		return "DolbyVision"
	default:
		return ""
	}
}

func parseFrameRate(s string) float64 {
	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		return 0
	}
	num, err1 := strconv.ParseFloat(parts[0], 64)
	den, err2 := strconv.ParseFloat(parts[1], 64)
	if err1 != nil || err2 != nil || den == 0 {
		return 0
	}
	return math.Round(num/den*1000) / 1000
}

func parseInt(s string) int {
	v, _ := strconv.Atoi(s)
	return v
}

func parseInt64(s string) int64 {
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}
