package stream

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseKeyframePackets_ClosedGOP(t *testing.T) {
	// Orden de decodificación de un GOP cerrado con B-frames: tras el
	// keyframe solo vienen fotogramas que se ven después.
	in := "0.000000,K__\n0.125000,___\n0.042000,___\n0.083000,___\n" +
		"4.000000,K__\n4.125000,___\n4.042000,___\n"
	idx, err := parseKeyframePackets(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	want := []KeyPoint{{0, 0}, {4, 4}}
	if len(idx.Points) != len(want) || idx.Points[0] != want[0] || idx.Points[1] != want[1] {
		t.Fatalf("points = %+v, want %+v", idx.Points, want)
	}
	if idx.MaxPTS != 4.125 {
		t.Fatalf("MaxPTS = %v", idx.MaxPTS)
	}
}

func TestParseKeyframePackets_OpenGOPLeadingFrames(t *testing.T) {
	// GOP abierto: tras el keyframe de 5.0 llegan RASL que se ven ANTES
	// (4.917, 4.958): pertenecen a su segmento → Lead = 4.917.
	in := "0.000000,K__\n0.083000,___\n" +
		"5.000000,K__\n4.958000,___\n4.917000,___\n5.042000,___\n"
	idx, err := parseKeyframePackets(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Points) != 2 || idx.Points[1].PTS != 5 || idx.Points[1].Lead != 4.917 {
		t.Fatalf("points = %+v", idx.Points)
	}
}

func TestParseKeyframePackets_SkipsJunk(t *testing.T) {
	in := "0.500000,___\n" + // antes del primer keyframe: ignorado
		"N/A,K__\n" + // sin pts
		"1.000000,K__\n\n" +
		"0.900000,K__\n" + // keyframe que no avanza: descartado
		"garbage\n" +
		"2.000000,K_D\n"
	idx, err := parseKeyframePackets(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Points) != 2 || idx.Points[0].PTS != 1 || idx.Points[1].PTS != 2 {
		t.Fatalf("points = %+v", idx.Points)
	}
}

func TestParseKeyframePackets_NoKeyframes(t *testing.T) {
	_, err := parseKeyframePackets(strings.NewReader("0.1,___\n0.2,___\n"))
	if !errors.Is(err, errNoKeyframes) {
		t.Fatalf("err = %v, want errNoKeyframes", err)
	}
}

func TestKeyframeStore_DiskRoundTripAndInvalidation(t *testing.T) {
	dir := t.TempDir()
	media := filepath.Join(t.TempDir(), "movie.mkv")
	if err := os.WriteFile(media, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	key, err := keyframeCacheKey(media)
	if err != nil {
		t.Fatal(err)
	}
	s := NewKeyframeStore(dir, "__hubplay_nonexistent_ffprobe__", nil)
	defer s.Close()
	want := &KeyframeIndex{Points: []KeyPoint{{0, 0}, {5, 4.9}}, MaxPTS: 9.5}
	s.persist(key, want)

	// Un store nuevo (reinicio del servidor) lo encuentra en disco.
	s2 := NewKeyframeStore(dir, "__hubplay_nonexistent_ffprobe__", nil)
	defer s2.Close()
	got, ok := s2.Lookup(media)
	if !ok || len(got.Points) != 2 || got.Points[1] != want.Points[1] || got.MaxPTS != 9.5 {
		t.Fatalf("Lookup = %+v, %v", got, ok)
	}

	// Si el fichero cambia (tamaño/mtime), el índice viejo no vale.
	if err := os.WriteFile(media, []byte("version 2"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour)
	_ = os.Chtimes(media, future, future)
	if _, ok := s2.Lookup(media); ok {
		t.Fatal("stale index returned after the file changed")
	}
}

func TestKeyframeStore_FailureIsCachedAndBounded(t *testing.T) {
	media := filepath.Join(t.TempDir(), "movie.mkv")
	if err := os.WriteFile(media, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewKeyframeStore("", "__hubplay_nonexistent_ffprobe__", nil)
	defer s.Close()

	began := time.Now()
	if _, ok := s.Wait(context.Background(), media, 5*time.Second); ok {
		t.Fatal("Wait succeeded without ffprobe")
	}
	if time.Since(began) > 3*time.Second {
		t.Fatalf("a failing index must not burn the whole budget (took %v)", time.Since(began))
	}
	// El fallo queda cacheado: no se relanza en cada reproducción.
	if ch, _ := s.start(media); ch != nil {
		t.Fatal("failed index relaunched immediately")
	}
}

func TestKeyframeStore_WaitMissingFile(t *testing.T) {
	s := NewKeyframeStore("", "", nil)
	defer s.Close()
	if _, ok := s.Wait(context.Background(), "/nonexistent/file.mkv", time.Second); ok {
		t.Fatal("Wait succeeded for a missing file")
	}
}
