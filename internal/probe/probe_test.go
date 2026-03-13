package probe

import (
	"testing"
	"time"
)

func TestParseOutput_Movie(t *testing.T) {
	data := []byte(`{
		"format": {
			"filename": "/media/movie.mkv",
			"duration": "7325.123",
			"size": "4294967296",
			"bit_rate": "4690000",
			"format_name": "matroska,webm"
		},
		"streams": [
			{
				"index": 0,
				"codec_type": "video",
				"codec_name": "h264",
				"profile": "High",
				"bit_rate": "4500000",
				"width": 1920,
				"height": 1080,
				"r_frame_rate": "24000/1001",
				"color_transfer": "",
				"color_space": "bt709",
				"disposition": {"default": 1, "forced": 0}
			},
			{
				"index": 1,
				"codec_type": "audio",
				"codec_name": "aac",
				"bit_rate": "128000",
				"channels": 6,
				"sample_rate": "48000",
				"tags": {"language": "eng", "title": "Surround 5.1"},
				"disposition": {"default": 1}
			},
			{
				"index": 2,
				"codec_type": "subtitle",
				"codec_name": "subrip",
				"tags": {"language": "spa", "title": "Spanish"},
				"disposition": {"default": 0, "forced": 0, "hearing_impaired": 1}
			}
		]
	}`)

	result, err := parseOutput(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Format
	if result.Format.Filename != "/media/movie.mkv" {
		t.Errorf("expected filename, got %q", result.Format.Filename)
	}
	if result.Format.Size != 4294967296 {
		t.Errorf("expected size 4294967296, got %d", result.Format.Size)
	}
	if result.Format.BitRate != 4690000 {
		t.Errorf("expected bitrate 4690000, got %d", result.Format.BitRate)
	}
	if result.Format.FormatName != "matroska,webm" {
		t.Errorf("expected format 'matroska,webm', got %q", result.Format.FormatName)
	}

	// Duration — ~7325 seconds
	expectedDuration := time.Duration(7325.123 * float64(time.Second))
	diff := result.Format.Duration - expectedDuration
	if diff < 0 {
		diff = -diff
	}
	if diff > time.Millisecond {
		t.Errorf("expected duration ~%v, got %v", expectedDuration, result.Format.Duration)
	}

	// Streams
	if len(result.Streams) != 3 {
		t.Fatalf("expected 3 streams, got %d", len(result.Streams))
	}

	video := result.Streams[0]
	if video.CodecType != "video" {
		t.Errorf("expected video stream, got %q", video.CodecType)
	}
	if video.CodecName != "h264" {
		t.Errorf("expected h264, got %q", video.CodecName)
	}
	if video.Width != 1920 || video.Height != 1080 {
		t.Errorf("expected 1920x1080, got %dx%d", video.Width, video.Height)
	}
	// 24000/1001 ≈ 23.976
	if video.FrameRate < 23.975 || video.FrameRate > 23.977 {
		t.Errorf("expected frame rate ~23.976, got %f", video.FrameRate)
	}
	if !video.IsDefault {
		t.Error("expected video to be default")
	}

	audio := result.Streams[1]
	if audio.Channels != 6 {
		t.Errorf("expected 6 channels, got %d", audio.Channels)
	}
	if audio.Language != "eng" {
		t.Errorf("expected language 'eng', got %q", audio.Language)
	}
	if audio.Title != "Surround 5.1" {
		t.Errorf("expected title 'Surround 5.1', got %q", audio.Title)
	}

	sub := result.Streams[2]
	if sub.Language != "spa" {
		t.Errorf("expected language 'spa', got %q", sub.Language)
	}
	if !sub.IsHearingImpaired {
		t.Error("expected hearing impaired flag")
	}
}

func TestParseOutput_HDR(t *testing.T) {
	tests := []struct {
		name          string
		colorTransfer string
		profile       string
		wantHDR       string
	}{
		{"HDR10", "smpte2084", "", "HDR10"},
		{"HLG", "arib-std-b67", "", "HLG"},
		{"DolbyVision", "", "Dolby Vision profile 5", "DolbyVision"},
		{"SDR", "bt709", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectHDR(tt.colorTransfer, tt.profile)
			if got != tt.wantHDR {
				t.Errorf("expected %q, got %q", tt.wantHDR, got)
			}
		})
	}
}

func TestParseFrameRate(t *testing.T) {
	tests := []struct {
		input string
		want  float64
	}{
		{"24000/1001", 23.976},
		{"30/1", 30.0},
		{"25/1", 25.0},
		{"0/0", 0},
		{"invalid", 0},
	}

	for _, tt := range tests {
		got := parseFrameRate(tt.input)
		diff := got - tt.want
		if diff < 0 {
			diff = -diff
		}
		if diff > 0.001 {
			t.Errorf("parseFrameRate(%q) = %f, want %f", tt.input, got, tt.want)
		}
	}
}

func TestDurationTicks(t *testing.T) {
	d := 2*time.Hour + 30*time.Minute
	ticks := DurationTicks(d)

	// 2.5 hours = 9000 seconds = 90,000,000,000 ticks
	expected := int64(90000000000)
	if ticks != expected {
		t.Errorf("expected %d ticks, got %d", expected, ticks)
	}

	// Round-trip
	back := TicksToDuration(ticks)
	if back != d {
		t.Errorf("expected %v, got %v", d, back)
	}
}

func TestParseOutput_InvalidJSON(t *testing.T) {
	_, err := parseOutput([]byte("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseOutput_Empty(t *testing.T) {
	result, err := parseOutput([]byte(`{"format":{},"streams":[]}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Streams) != 0 {
		t.Errorf("expected 0 streams, got %d", len(result.Streams))
	}
}
