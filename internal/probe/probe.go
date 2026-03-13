package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Result contains the parsed output from ffprobe.
type Result struct {
	Format  Format
	Streams []Stream
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
}

// DurationTicks converts a duration to ticks (10,000 ticks = 1ms, 10M ticks = 1s).
func DurationTicks(d time.Duration) int64 {
	return d.Microseconds() * 10
}

// TicksToDuration converts ticks back to a duration.
func TicksToDuration(ticks int64) time.Duration {
	return time.Duration(ticks/10) * time.Microsecond
}

// Prober runs ffprobe on media files. Implemented as an interface for testing.
type Prober interface {
	Probe(ctx context.Context, path string) (*Result, error)
}

// FFprobe is the real implementation that shells out to ffprobe.
type FFprobe struct {
	BinPath string // path to ffprobe binary, defaults to "ffprobe"
}

func New() *FFprobe {
	return &FFprobe{BinPath: "ffprobe"}
}

func (f *FFprobe) Probe(ctx context.Context, path string) (*Result, error) {
	bin := f.BinPath
	if bin == "" {
		bin = "ffprobe"
	}

	cmd := exec.CommandContext(ctx, bin,
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		path,
	)

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe %q: %w", path, err)
	}

	return parseOutput(out)
}

// ffprobeOutput maps the JSON structure from ffprobe.
type ffprobeOutput struct {
	Format  ffprobeFormat   `json:"format"`
	Streams []ffprobeStream `json:"streams"`
}

type ffprobeFormat struct {
	Filename   string `json:"filename"`
	Duration   string `json:"duration"`
	Size       string `json:"size"`
	BitRate    string `json:"bit_rate"`
	FormatName string `json:"format_name"`
}

type ffprobeStream struct {
	Index         int            `json:"index"`
	CodecType     string         `json:"codec_type"`
	CodecName     string         `json:"codec_name"`
	Profile       string         `json:"profile"`
	BitRate       string         `json:"bit_rate"`
	Width         int            `json:"width"`
	Height        int            `json:"height"`
	RFrameRate    string         `json:"r_frame_rate"`
	ColorTransfer string         `json:"color_transfer"`
	ColorSpace    string         `json:"color_space"`
	Channels      int            `json:"channels"`
	SampleRate    string         `json:"sample_rate"`
	Tags          map[string]any `json:"tags"`
	Disposition   map[string]int `json:"disposition"`
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

		// HDR detection
		stream.HDRType = detectHDR(s.ColorTransfer, s.Profile)

		// Tags
		if s.Tags != nil {
			if lang, ok := s.Tags["language"].(string); ok {
				stream.Language = lang
			}
			if title, ok := s.Tags["title"].(string); ok {
				stream.Title = title
			}
		}

		// Disposition
		if s.Disposition != nil {
			stream.IsDefault = s.Disposition["default"] == 1
			stream.IsForced = s.Disposition["forced"] == 1
			stream.IsHearingImpaired = s.Disposition["hearing_impaired"] == 1
		}

		result.Streams = append(result.Streams, stream)
	}

	return result, nil
}

func detectHDR(colorTransfer, profile string) string {
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
