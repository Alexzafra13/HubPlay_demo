package library

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Extracción de fingerprints vía `fpcalc` (chromaprint) con caché en
// disco keyed por (item_id, window, mtime, size). La caché se invalida
// automáticamente al cambiar el fichero fuente.
//
// fpcalc se invoca con `-raw` para obtener hashes decimales por frame;
// el matcher opera directamente sobre uint32.
//
// No se pipea ffmpeg a fpcalc: fpcalc tiene su propio demuxer. Para
// el outro extraemos un tail slice via ffmpeg a un tempfile (sin
// libav en proceso) y apuntamos fpcalc ahí.

// FingerprintWindow indica qué segmento del fichero cubre el fingerprint.
type FingerprintWindow string

const (
	WindowIntro FingerprintWindow = "intro"
	WindowOutro FingerprintWindow = "outro"
)

// Duraciones de ventana — generosas para captar recap+intro al inicio
// y credits+stingers al final sin reajustar por serie.
const (
	IntroWindowSeconds = 600 // 10 min
	OutroWindowSeconds = 360 // 6 min
)

// errFpcalcMissing: el binario no está en PATH. El caller degrada
// a INFO y desactiva fingerprinting — no debe crashear la instalación.
var errFpcalcMissing = errors.New("fpcalc not found on PATH")

// Fingerprinter envuelve la invocación de fpcalc y la caché en disco.
// Sin estado mutable aparte de cacheDir — seguro entre goroutines.
type Fingerprinter struct {
	cacheDir   string
	fpcalcPath string // "" si no está instalado
}

func NewFingerprinter(cacheDir string) *Fingerprinter {
	dir := filepath.Join(cacheDir, "fingerprints")
	_ = os.MkdirAll(dir, 0o755)
	path, _ := exec.LookPath("fpcalc")
	return &Fingerprinter{cacheDir: dir, fpcalcPath: path}
}

func (f *Fingerprinter) Available() bool { return f.fpcalcPath != "" }

// Compute devuelve los hashes chromaprint de la ventana de audio indicada.
// Cachea en disco; devuelve (nil, errFpcalcMissing) sin fpcalc,
// (nil, nil) si el fichero es más corto que la ventana.
func (f *Fingerprinter) Compute(
	ctx context.Context,
	itemID string,
	sourcePath string,
	window FingerprintWindow,
) ([]uint32, error) {
	if !f.Available() {
		return nil, errFpcalcMissing
	}
	st, err := os.Stat(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("stat source: %w", err)
	}
	cachePath := filepath.Join(f.cacheDir, fmt.Sprintf("%s.%s.fp", itemID, window))
	if hashes, ok := readCache(cachePath, st); ok {
		return hashes, nil
	}
	hashes, err := f.compute(ctx, sourcePath, window)
	if err != nil {
		return nil, err
	}
	_ = writeCache(cachePath, st, hashes)
	return hashes, nil
}

func (f *Fingerprinter) compute(
	ctx context.Context,
	sourcePath string,
	window FingerprintWindow,
) ([]uint32, error) {
	switch window {
	case WindowIntro:
		return runFpcalc(ctx, f.fpcalcPath, sourcePath, 0, IntroWindowSeconds)
	case WindowOutro:
		// Tail-slice via ffmpeg a wav temporal, luego fpcalc sobre él.
		return tailFpcalc(ctx, f.fpcalcPath, sourcePath, OutroWindowSeconds)
	default:
		return nil, fmt.Errorf("unknown window %q", window)
	}
}

// runFpcalc invoca `fpcalc -raw -length N <path>` y parsea la lista de hashes.
// fpcalc emite int32 signados — se preserva el patrón de bits via int64→uint32.
func runFpcalc(ctx context.Context, fpcalcPath, source string, offsetSec, lengthSec int) ([]uint32, error) {
	args := []string{"-raw", "-length", strconv.Itoa(lengthSec)}
	if offsetSec > 0 {
		args = append(args, "-ts", "0")
	}
	args = append(args, source)
	cmd := exec.CommandContext(ctx, fpcalcPath, args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("fpcalc: %w", err)
	}
	return parseFpcalcOutput(out)
}

// tailFpcalc extrae los últimos lengthSec segundos via ffmpeg y los
// pasa a fpcalc. Usamos tempfile en vez de stdin para evitar skew
// entre el stdin parser de fpcalc y el muxer de ffmpeg.
func tailFpcalc(ctx context.Context, fpcalcPath, source string, lengthSec int) ([]uint32, error) {
	tmp, err := os.CreateTemp("", "hubplay-outro-*.wav")
	if err != nil {
		return nil, fmt.Errorf("temp wav: %w", err)
	}
	tmp.Close()
	defer os.Remove(tmp.Name())

	// Mono 11025 Hz — chromaprint downsamplea internamente a esto,
	// así ahorramos el resampler de ffmpeg.
	ffArgs := []string{
		"-loglevel", "error",
		"-sseof", fmt.Sprintf("-%d", lengthSec),
		"-i", source,
		"-vn", "-sn",
		"-ac", "1",
		"-ar", "11025",
		"-f", "wav",
		"-y", tmp.Name(),
	}
	if err := exec.CommandContext(ctx, "ffmpeg", ffArgs...).Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg tail-extract: %w", err)
	}
	return runFpcalc(ctx, fpcalcPath, tmp.Name(), 0, lengthSec)
}

func parseFpcalcOutput(raw []byte) ([]uint32, error) {
	var fp string
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "FINGERPRINT=") {
			fp = strings.TrimPrefix(line, "FINGERPRINT=")
			break
		}
	}
	if fp == "" {
		return nil, errors.New("fpcalc: no FINGERPRINT line in output")
	}
	tokens := strings.Split(strings.TrimSpace(fp), ",")
	out := make([]uint32, 0, len(tokens))
	for _, tok := range tokens {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		// fpcalc emite int32 signados; preservar patrón de bits.
		v, err := strconv.ParseInt(tok, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse hash %q: %w", tok, err)
		}
		out = append(out, uint32(v))
	}
	if len(out) == 0 {
		return nil, errors.New("fpcalc: empty fingerprint")
	}
	return out, nil
}

// Formato de caché en disco. Header 24 bytes:
//   [0..8)   uint64 mtime ns
//   [8..16)  int64  tamaño fuente
//   [16..20) uint32 cantidad de hashes
//   [20..24) reservado
// Seguido de `count` uint32 little-endian. Clave = mtime+size.
const cacheHeaderSize = 24

func readCache(path string, src os.FileInfo) ([]uint32, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	header := make([]byte, cacheHeaderSize)
	if _, err := io.ReadFull(f, header); err != nil {
		return nil, false
	}
	mtime := binary.LittleEndian.Uint64(header[0:8])
	size := int64(binary.LittleEndian.Uint64(header[8:16]))
	count := binary.LittleEndian.Uint32(header[16:20])
	if uint64(src.ModTime().UnixNano()) != mtime || src.Size() != size {
		return nil, false
	}
	if count == 0 || count > 1<<20 {
		return nil, false // >1M frames es inválido
	}
	body := make([]byte, 4*count)
	if _, err := io.ReadFull(f, body); err != nil {
		return nil, false
	}
	hashes := make([]uint32, count)
	for i := range hashes {
		hashes[i] = binary.LittleEndian.Uint32(body[4*i : 4*i+4])
	}
	return hashes, true
}

func writeCache(path string, src os.FileInfo, hashes []uint32) error {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	header := make([]byte, cacheHeaderSize)
	binary.LittleEndian.PutUint64(header[0:8], uint64(src.ModTime().UnixNano()))
	binary.LittleEndian.PutUint64(header[8:16], uint64(src.Size()))
	binary.LittleEndian.PutUint32(header[16:20], uint32(len(hashes)))
	if _, err := f.Write(header); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	body := make([]byte, 4*len(hashes))
	for i, h := range hashes {
		binary.LittleEndian.PutUint32(body[4*i:4*i+4], h)
	}
	if _, err := f.Write(body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
