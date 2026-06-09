package runtimetune

import "testing"

func TestParseCPUMax(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		want   float64
		wantOK bool
	}{
		{"two cores", "200000 100000", 2, true},
		{"one core", "100000 100000", 1, true},
		{"half core", "50000 100000", 0.5, true},
		{"unlimited", "max 100000", 0, false},
		{"trailing newline", "200000 100000\n", 2, true},
		{"single field", "200000", 0, false},
		{"garbage", "abc def", 0, false},
		{"zero period", "100000 0", 0, false},
		{"empty", "", 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseCPUMax(tc.in)
			if ok != tc.wantOK || (ok && got != tc.want) {
				t.Fatalf("parseCPUMax(%q) = (%v, %v), want (%v, %v)", tc.in, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestCPURatio(t *testing.T) {
	if v, ok := cpuRatio(200000, 100000); !ok || v != 2 {
		t.Fatalf("cpuRatio(200000,100000) = (%v,%v), want (2,true)", v, ok)
	}
	if _, ok := cpuRatio(0, 100000); ok {
		t.Fatal("cpuRatio with zero quota should be !ok")
	}
	if _, ok := cpuRatio(-1, 100000); ok {
		t.Fatal("cpuRatio with negative quota should be !ok")
	}
	if _, ok := cpuRatio(100000, 0); ok {
		t.Fatal("cpuRatio with zero period should be !ok")
	}
}

func TestParseMemLimit(t *testing.T) {
	const giB = int64(1) << 30
	tests := []struct {
		name   string
		in     string
		want   int64
		wantOK bool
	}{
		{"1 GiB", "1073741824", giB, true},
		{"trailing newline", "1073741824\n", giB, true},
		{"v2 max", "max", 0, false},
		{"empty", "", 0, false},
		{"zero", "0", 0, false},
		{"garbage", "lots", 0, false},
		{"v1 unlimited sentinel", "9223372036854771712", 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseMemLimit(tc.in)
			if ok != tc.wantOK || (ok && got != tc.want) {
				t.Fatalf("parseMemLimit(%q) = (%v, %v), want (%v, %v)", tc.in, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}
