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
