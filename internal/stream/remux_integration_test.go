package stream

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Integración con ffmpeg/ffprobe reales (se salta si no están o con
// -short). Fija la receta del remux por segmentos contra los fallos que
// destaparon las pruebas del 2026-11-26: fotogramas perdidos/duplicados
// en las fronteras con B-frames, RASL de GOP abierto, segmentos sin
// SPS/PPS y discontinuidades de audio AAC.

func requireFFmpeg(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("integración con ffmpeg real: omitida con -short")
	}
	for _, bin := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s no está en el PATH", bin)
		}
	}
}

// makeOpenGOPMovie genera un MKV H.264 con GOP abierto, B-frames y
// keyframes irregulares + audio AC3 (lo que un navegador no decodifica).
func makeOpenGOPMovie(t *testing.T, dir string) string {
	t.Helper()
	out := filepath.Join(dir, "movie.mkv")
	cmd := exec.Command("ffmpeg", "-v", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=160x90:rate=24000/1001:duration=45",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=45",
		"-c:v", "libx264", "-preset", "fast",
		"-x264-params", "keyint=110:min-keyint=20:open-gop=1:bframes=3",
		"-c:a", "ac3", "-b:a", "128k", "-shortest", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("no se pudo generar el vídeo de prueba (¿ffmpeg sin libx264/ac3?): %v %s", err, b)
	}
	return out
}

type probedPacket struct {
	pts float64
	key bool
}

func probePackets(t *testing.T, path, sel string) []probedPacket {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error", "-select_streams", sel,
		"-show_entries", "packet=pts_time,flags", "-of", "csv=p=0", path).Output()
	if err != nil {
		t.Fatalf("ffprobe %s: %v", path, err)
	}
	var pk []probedPacket
	for _, line := range strings.Fields(string(out)) {
		f := strings.Split(line, ",")
		if len(f) < 2 {
			continue
		}
		v, err := strconv.ParseFloat(f[0], 64)
		if err != nil {
			continue
		}
		pk = append(pk, probedPacket{pts: v, key: strings.Contains(f[1], "K")})
	}
	return pk
}

// nalTypesBeforeFirstSlice devuelve los tipos NAL H.264 previos al primer
// slice del segmento (deben incluir SPS=7 y PPS=8).
func nalTypesBeforeFirstSlice(t *testing.T, seg string) []int {
	t.Helper()
	raw, err := exec.Command("ffmpeg", "-v", "error", "-i", seg, "-map", "0:v:0", "-c", "copy", "-f", "h264", "-").Output()
	if err != nil {
		t.Fatalf("extract h264 %s: %v", seg, err)
	}
	var types []int
	for _, m := range regexp.MustCompile("\x00\x00\x01").FindAllIndex(raw, -1) {
		if m[1] >= len(raw) {
			break
		}
		ty := int(raw[m[1]] & 0x1f)
		if ty == 1 || ty == 5 {
			break
		}
		types = append(types, ty)
	}
	return types
}

func generateAllSegments(t *testing.T, spec remuxSpec, plan *SegmentPlan) (string, []string) {
	t.Helper()
	dir := t.TempDir()
	r := newSegmentRemuxer("ffmpeg", spec, plan, dir, testLogger())
	defer r.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	paths := make([]string, plan.Count())
	for i := 0; i < plan.Count(); i++ {
		p, err := r.Ensure(ctx, i)
		if err != nil {
			t.Fatalf("segment %d: %v", i, err)
		}
		paths[i] = p
	}
	return dir, paths
}

func TestRemuxIntegration_SegmentsAreExactAndSelfContained(t *testing.T) {
	requireFFmpeg(t)
	src := makeOpenGOPMovie(t, t.TempDir())

	idx, err := extractKeyframes(context.Background(), "ffprobe", src)
	if err != nil {
		t.Fatalf("extractKeyframes: %v", err)
	}
	leading := 0
	for _, kp := range idx.Points {
		if kp.Lead < kp.PTS {
			leading++
		}
	}
	if leading == 0 {
		t.Fatal("el vídeo de prueba debería tener GOP abierto (keyframes con fotogramas adelantados)")
	}
	plan := BuildSegmentPlan(idx, 45, 6)
	if plan == nil || plan.Count() < 4 {
		t.Fatalf("plan inútil: %+v", plan)
	}

	// Audio AC3 copiado tal cual (cliente que lo soporta).
	_, paths := generateAllSegments(t, remuxSpec{Input: src, VideoCodec: "h264", AudioOrdinal: 0, CopyAudio: true}, plan)

	srcVideo := probePackets(t, src, "v:0")
	var offset float64
	seen := map[int64]int{}
	var prevEnd float64
	for i, p := range paths {
		v := probePackets(t, p, "v:0")
		if len(v) == 0 {
			t.Fatalf("segment %d sin vídeo", i)
		}
		if !v[0].key {
			t.Errorf("segment %d no empieza en keyframe", i)
		}
		sorted := make([]float64, len(v))
		for j, pk := range v {
			sorted[j] = pk.pts
		}
		sort.Float64s(sorted)
		if i == 0 {
			offset = sorted[0] - plan.Start(0)
		}
		// Primer fotograma visible = Start(i) del plan (desfase
		// MPEG-TS constante en todos los segmentos).
		if d := sorted[0] - offset - plan.Start(i); math.Abs(d) > 0.002 {
			t.Errorf("segment %d empieza %.3f s fuera de su sitio", i, d)
		}
		if i > 0 && (sorted[0] <= prevEnd || sorted[0]-prevEnd > 0.1) {
			t.Errorf("segment %d: hueco/solape con el anterior (%.3f → %.3f)", i, prevEnd, sorted[0])
		}
		prevEnd = sorted[len(sorted)-1]
		for _, pts := range sorted {
			seen[int64(math.Round((pts-offset)*1000))]++
		}
		types := nalTypesBeforeFirstSlice(t, p)
		hasSPS, hasPPS := false, false
		for _, ty := range types {
			hasSPS = hasSPS || ty == 7
			hasPPS = hasPPS || ty == 8
		}
		if !hasSPS || !hasPPS {
			t.Errorf("segment %d sin SPS/PPS antes del primer slice (NAL %v)", i, types)
		}
	}
	// Ningún fotograma perdido ni duplicado respecto a la fuente.
	missing, dup := 0, 0
	for _, pk := range srcVideo {
		switch seen[int64(math.Round(pk.pts*1000))] {
		case 0:
			missing++
		case 1:
		default:
			dup++
		}
	}
	if missing != 0 || dup != 0 || len(seen) != len(srcVideo) {
		t.Fatalf("fotogramas: fuente %d, salida %d, faltan %d, duplicados %d", len(srcVideo), len(seen), missing, dup)
	}

	// Reproducción seguida: todos los segmentos concatenados decodifican
	// sin un solo error.
	all := filepath.Join(t.TempDir(), "all.ts")
	f, err := os.Create(all)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write(b)
	}
	_ = f.Close()
	if out, err := exec.Command("ffmpeg", "-v", "error", "-i", all, "-f", "null", "-").CombinedOutput(); err != nil || len(strings.TrimSpace(string(out))) > 0 {
		t.Fatalf("decodificación seguida con errores: %v\n%s", err, out)
	}
}

func TestRemuxIntegration_TranscodedAudioIsContinuousAcrossSegments(t *testing.T) {
	requireFFmpeg(t)
	src := makeOpenGOPMovie(t, t.TempDir())
	idx, err := extractKeyframes(context.Background(), "ffprobe", src)
	if err != nil {
		t.Fatal(err)
	}
	plan := BuildSegmentPlan(idx, 45, 6)
	// AC3 → AAC (cliente web): cada segmento con su propio codificador.
	_, paths := generateAllSegments(t, remuxSpec{
		Input: src, VideoCodec: "h264", AudioOrdinal: 0, CopyAudio: false, AudioBitrate: "128k", AudioChannels: 2,
	}, plan)

	var audio []float64
	for _, p := range paths {
		for _, pk := range probePackets(t, p, "a:0") {
			audio = append(audio, pk.pts)
		}
	}
	sort.Float64s(audio)
	// Rejilla común: entre paquetes consecutivos siempre una trama AAC
	// (1024 muestras a 48 kHz), también cruzando fronteras de segmento.
	// Tolerancia de ±1 muestra: redondeo de los tiempos MPEG-TS (90 kHz).
	var bad []string
	for j := 1; j < len(audio); j++ {
		samples := (audio[j] - audio[j-1]) * 48000
		if math.Abs(samples-1024) > 1.5 {
			bad = append(bad, fmt.Sprintf("%.3f:%+.0f", audio[j-1], samples-1024))
		}
	}
	if len(bad) > 0 {
		t.Fatalf("%d discontinuidades de audio (primeras: %v)", len(bad), bad[:min(5, len(bad))])
	}
}

func TestRemuxIntegration_KeyframeStoreIndexesAndCaches(t *testing.T) {
	requireFFmpeg(t)
	src := makeOpenGOPMovie(t, t.TempDir())
	cache := t.TempDir()
	s := NewKeyframeStore(cache, "", testLogger())
	defer s.Close()
	idx, ok := s.Wait(context.Background(), src, 30*time.Second)
	if !ok || len(idx.Points) < 4 {
		t.Fatalf("Wait = %v, %v", idx, ok)
	}
	entries, _ := os.ReadDir(cache)
	if len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), ".json") {
		t.Fatalf("caché en disco = %v", entries)
	}
	// Otro store (reinicio) lo lee de disco sin lanzar ffprobe.
	s2 := NewKeyframeStore(cache, "__hubplay_nonexistent_ffprobe__", testLogger())
	defer s2.Close()
	if got, ok := s2.Lookup(src); !ok || len(got.Points) != len(idx.Points) {
		t.Fatalf("Lookup tras reinicio = %v, %v", got, ok)
	}
}
