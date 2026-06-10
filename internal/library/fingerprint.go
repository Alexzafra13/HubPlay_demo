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
	"time"
)

// Fingerprint extraction via chromaprint's `fpcalc` binary, plus a
// disk cache keyed on (item_id, window kind, source mtime, source
// size). The cache is invalidated automatically when the source
// file changes — re-encoding or replacing the mkv produces a fresh
// fingerprint without manual cleanup.
//
// fpcalc is invoked with `-raw` so we get decimal hashes one per
// frame; the matcher works on the raw uint32 values directly. The
// `-length` flag caps the audio window we analyse — 600 s for the
// intro window, configurable for the outro.
//
// We do NOT pipe ffmpeg through fpcalc: fpcalc has its own demuxer
// and reads `-length` seconds from the start of the file. For the
// outro window we extract a tail slice via ffmpeg into a tempfile
// (no in-process libav binding — we already trust the system
// ffmpeg), then point fpcalc at that. The tempfile lives only for
// the duration of the call.

// FingerprintWindow names which slice of the file the fingerprint
// covers. The orchestrator translates back to seconds when writing
// segments to the DB (intro starts at 0, outro starts at duration -
// window length).
type FingerprintWindow string

const (
	WindowIntro FingerprintWindow = "intro" // first IntroWindowSeconds of audio
	WindowOutro FingerprintWindow = "outro" // last OutroWindowSeconds of audio
)

// Window length defaults — intentionally generous so we can match
// recap+intro composites at the head and credits+post-credits
// stingers at the tail without retuning per-series.
const (
	IntroWindowSeconds = 600 // 10 min
	OutroWindowSeconds = 360 // 6 min
)

// errFpcalcMissing surfaces when the binary isn't on PATH. Caller
// (segment_fingerprinter.go) demotes this to a one-time INFO log
// and disables fingerprint detection — installs without
// chromaprint-tools shouldn't crash, just degrade gracefully.
var errFpcalcMissing = errors.New("fpcalc not found on PATH")

// Fingerprinter wraps the fpcalc invocation and the on-disk cache.
// Stateless aside from cacheDir — safe to share across goroutines.
type Fingerprinter struct {
	cacheDir   string // <cache>/fingerprints
	fpcalcPath string // resolved at construction; "" if missing
}

func NewFingerprinter(cacheDir string) *Fingerprinter {
	dir := filepath.Join(cacheDir, "fingerprints")
	_ = os.MkdirAll(dir, 0o755)
	path, _ := exec.LookPath("fpcalc")
	return &Fingerprinter{cacheDir: dir, fpcalcPath: path}
}

// Available reports whether fpcalc was found on PATH at startup.
// Callers use this to short-circuit the entire fingerprint
// detection pipeline — no point spawning workers if every
// invocation will return errFpcalcMissing.
func (f *Fingerprinter) Available() bool { return f.fpcalcPath != "" }

// Compute returns the chromaprint hashes for the given audio window
// of the source file. Cached on disk under
// `<cache>/fingerprints/<itemID>.<window>.fp`; the cache is keyed
// on (mtime, size) of the source file so a re-encoded or replaced
// file invalidates automatically.
//
// Returns (nil, errFpcalcMissing) when fpcalc isn't installed.
// Returns (nil, nil) when the source file is shorter than the
// requested window — there's nothing to fingerprint.
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
		// ffmpeg tail-slice into a temp wav, then fpcalc on it. Two
		// processes; kept to one OS pipe so we don't buffer the
		// whole window in Go memory. ffprobe-then-ffmpeg would be
		// half the round-trip but we already have duration cached
		// in items.duration_ticks at the call site.
		return tailFpcalc(ctx, f.fpcalcPath, sourcePath, OutroWindowSeconds)
	default:
		return nil, fmt.Errorf("unknown window %q", window)
	}
}

// runFpcalc invokes `fpcalc -raw -length N <path>` and parses the
// decimal hash list. Output format:
//
//	DURATION=600
//	FINGERPRINT=12345,67890,...,123
//
// We ignore DURATION (we already know it) and tokenise the comma
// list into uint32. fpcalc emits hashes as signed int32 — we cast
// via int64→uint32 to preserve the bit pattern.
// fingerprintToolTimeout acota cada invocación de fpcalc/ffmpeg del
// fingerprinting. Sin techo, un fichero sobre un mount colgado dejaba
// un worker (de 2) bloqueado para siempre con el mutex de librería
// tomado. PB-11 (audit 2026-06-10).
const fingerprintToolTimeout = 2 * time.Minute

func runFpcalc(ctx context.Context, fpcalcPath, source string, offsetSec, lengthSec int) ([]uint32, error) {
	ctx, cancel := context.WithTimeout(ctx, fingerprintToolTimeout)
	defer cancel()
	args := []string{"-raw", "-length", strconv.Itoa(lengthSec)}
	if offsetSec > 0 {
		args = append(args, "-ts", "0") // suppress timestamps in output
	}
	args = append(args, source)
	cmd := exec.CommandContext(ctx, fpcalcPath, args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("fpcalc: %w", err)
	}
	return parseFpcalcOutput(out)
}

// tailFpcalc extracts the last lengthSec seconds via ffmpeg piped
// into fpcalc's stdin. fpcalc reads raw audio over stdin when it's
// invoked without a file arg AND given `-rate -channels -format`,
// but the simpler path is to write a temp wav and point fpcalc at
// it — no version-skew between fpcalc's stdin parser and ffmpeg's
// muxer.
func tailFpcalc(ctx context.Context, fpcalcPath, source string, lengthSec int) ([]uint32, error) {
	ctx, cancel := context.WithTimeout(ctx, fingerprintToolTimeout)
	defer cancel()
	tmp, err := os.CreateTemp("", "hubplay-outro-*.wav")
	if err != nil {
		return nil, fmt.Errorf("temp wav: %w", err)
	}
	tmp.Close()
	defer os.Remove(tmp.Name())

	// `-sseof -lengthSec` seeks to (duration - lengthSec). We pin the
	// audio to mono 11025 Hz — chromaprint downsamples to that
	// internally anyway, so we save ffmpeg's resampler on the
	// playback-rate audio.
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
		// fpcalc emits signed int32; preserve the bit pattern.
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

// On-disk cache format. Header is 24 bytes:
//
//	[0..8)   uint64 mtime nanoseconds
//	[8..16)  int64  source size in bytes
//	[16..20) uint32 hash count
//	[20..24) reserved (zero)
//
// Followed by `count` little-endian uint32 hashes. Total size:
// 24 + 4*count bytes. Mtime+size is the cache key — when either
// changes, readCache returns ok=false and Compute re-runs fpcalc.
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
		return nil, false // sanity guard; >1M frames is bogus
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
