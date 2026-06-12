package stream

import "testing"

func TestParseHWAccels(t *testing.T) {
	output := `Hardware acceleration methods:
vaapi
cuda
qsv
`
	accels := parseHWAccels(output)
	if len(accels) != 3 {
		t.Fatalf("expected 3 accels, got %d", len(accels))
	}

	expected := []HWAccelType{HWAccelVAAPI, HWAccelNVENC, HWAccelQSV}
	for i, want := range expected {
		if accels[i] != want {
			t.Errorf("accels[%d] = %s, want %s", i, accels[i], want)
		}
	}
}

func TestSelectAccel_Preferred(t *testing.T) {
	available := []HWAccelType{HWAccelVAAPI, HWAccelQSV}

	got := selectAccel(available, "qsv")
	if got != HWAccelQSV {
		t.Errorf("expected qsv, got %s", got)
	}
}

func TestSelectAccel_Auto(t *testing.T) {
	available := []HWAccelType{HWAccelVAAPI, HWAccelNVENC}

	got := selectAccel(available, "auto")
	if got != HWAccelNVENC {
		t.Errorf("expected nvenc (highest priority), got %s", got)
	}
}

func TestSelectAccel_NotAvailable(t *testing.T) {
	available := []HWAccelType{HWAccelVAAPI}

	got := selectAccel(available, "nvenc")
	// Falls back to auto since preferred not available
	if got != HWAccelVAAPI {
		t.Errorf("expected vaapi fallback, got %s", got)
	}
}

func TestAccelToEncoder(t *testing.T) {
	tests := []struct {
		accel HWAccelType
		want  string
	}{
		{HWAccelVAAPI, "h264_vaapi"},
		{HWAccelQSV, "h264_qsv"},
		{HWAccelNVENC, "h264_nvenc"},
		{HWAccelVideoToolbox, "h264_videotoolbox"},
		{HWAccelNone, "libx264"},
	}

	for _, tc := range tests {
		got := accelToEncoder(tc.accel)
		if got != tc.want {
			t.Errorf("accelToEncoder(%s) = %s, want %s", tc.accel, got, tc.want)
		}
	}
}

// ─── PB-5: input args con device + verificación real ─────────────────

func TestHWAccelInputArgs_VAAPI_DeclaresDevice(t *testing.T) {
	args := HWAccelInputArgs(HWAccelVAAPI, "")
	want := []string{
		"-init_hw_device", "vaapi=hw:" + DefaultVAAPIDevice,
		"-hwaccel", "vaapi",
		"-hwaccel_device", "hw",
		"-filter_hw_device", "hw",
	}
	if len(args) != len(want) {
		t.Fatalf("args = %v, want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q (full: %v)", i, args[i], want[i], args)
		}
	}
}

func TestHWAccelInputArgs_VAAPI_CustomDevice(t *testing.T) {
	args := HWAccelInputArgs(HWAccelVAAPI, "/dev/dri/renderD129")
	if args[1] != "vaapi=hw:/dev/dri/renderD129" {
		t.Errorf("custom device not threaded: %v", args)
	}
}

func TestHWAccelInputArgs_QSV_InitsDeviceOnly(t *testing.T) {
	args := HWAccelInputArgs(HWAccelQSV, "")
	joined := ""
	for _, a := range args {
		joined += a + " "
	}
	if joined != "-init_hw_device qsv=hw -filter_hw_device hw " {
		t.Errorf("unexpected QSV args: %v", args)
	}
}

func TestHWAccelInputArgs_NVENCAndNone(t *testing.T) {
	if args := HWAccelInputArgs(HWAccelNVENC, ""); len(args) != 2 || args[0] != "-hwaccel" || args[1] != "cuda" {
		t.Errorf("NVENC args = %v, want [-hwaccel cuda]", args)
	}
	if args := HWAccelInputArgs(HWAccelNone, ""); args != nil {
		t.Errorf("None args = %v, want nil", args)
	}
	if args := HWAccelInputArgs(HWAccelVideoToolbox, ""); args != nil {
		t.Errorf("VideoToolbox args = %v, want nil", args)
	}
}

func TestHWUploadVideoFilter(t *testing.T) {
	if got := HWUploadVideoFilter("h264_vaapi"); got != "format=nv12,hwupload" {
		t.Errorf("vaapi filter = %q", got)
	}
	for _, enc := range []string{"libx264", "h264_nvenc", "h264_qsv", "h264_videotoolbox", ""} {
		if got := HWUploadVideoFilter(enc); got != "" {
			t.Errorf("encoder %q should not upload, got %q", enc, got)
		}
	}
}
