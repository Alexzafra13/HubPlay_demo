package stream

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"hubplay/internal/procutil"
)

// Índice de keyframes para el remux por segmentos.
//
// Copiando el vídeo (`-c:v copy`) ffmpeg solo puede cortar en los
// keyframes del fichero, así que un manifest sintético de "6 s por
// segmento" miente en casi todos los ficheros reales (medido
// 2026-11-26: 6 de 11 segmentos desplazados +2 s con keyframes cada 4 s).
// Con el índice, el manifest declara los cortes reales y cada segmento
// se genera de forma independiente (ver remux.go).

// KeyPoint es un keyframe del vídeo principal.
//
// PTS es el tiempo de presentación del keyframe. Lead es el primer
// instante de pantalla que pertenece a su grupo en ORDEN DE
// DECODIFICACIÓN: en GOP cerrado coincide con PTS; en GOP abierto
// (x265 por defecto, remuxes de Blu-ray) los fotogramas "adelantados"
// (RASL) se ven antes del keyframe pero se decodifican después de él, y
// HLS los pone en el segmento del keyframe. Cortar por Lead en vez de por
// PTS es lo que hace que ningún fotograma se pierda ni se duplique.
type KeyPoint struct {
	PTS  float64
	Lead float64
}

// KeyframeIndex es el resultado de indexar un fichero.
type KeyframeIndex struct {
	Points []KeyPoint
	// MaxPTS es el mayor tiempo de presentación visto (fin aproximado
	// del vídeo, por si la duración del item no está).
	MaxPTS float64
}

// keyframeIndexVersion invalida cachés en disco si cambia el formato o
// la semántica del índice.
const keyframeIndexVersion = 1

// parseKeyframePackets lee la salida CSV de
// `ffprobe -show_entries packet=pts_time,flags -of csv=p=0` (un paquete
// por línea, EN ORDEN DE DECODIFICACIÓN: "12.345000,K__") y construye el
// índice. Los paquetes sin pts se ignoran; los keyframes que no avanzan
// (pts o lead no crecientes) se descartan porque no sirven como corte.
func parseKeyframePackets(r io.Reader) (*KeyframeIndex, error) {
	idx := &KeyframeIndex{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	have := false
	var cur KeyPoint
	flush := func() {
		if !have {
			return
		}
		if n := len(idx.Points); n > 0 {
			prev := idx.Points[n-1]
			if cur.PTS <= prev.PTS || cur.Lead <= prev.Lead {
				return
			}
		}
		idx.Points = append(idx.Points, cur)
	}
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) < 2 {
			continue
		}
		pts, err := strconv.ParseFloat(fields[0], 64)
		if err != nil {
			continue // "N/A"
		}
		if pts > idx.MaxPTS {
			idx.MaxPTS = pts
		}
		if strings.Contains(fields[1], "K") {
			flush()
			cur = KeyPoint{PTS: pts, Lead: pts}
			have = true
			continue
		}
		if have && pts < cur.PTS && pts < cur.Lead {
			cur.Lead = pts
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	flush()
	if len(idx.Points) == 0 {
		return nil, errNoKeyframes
	}
	return idx, nil
}

var errNoKeyframes = errors.New("stream: no keyframes found")

// extractKeyframes lanza ffprobe sobre el vídeo principal (`V:0`: la V
// mayúscula excluye carátulas `attached_pic`). Solo demuxa, no
// decodifica: el coste es leer el fichero (~0,3 s por 64 MB en caché de
// disco; en un disco duro, el throughput del disco).
func extractKeyframes(ctx context.Context, ffprobe, input string) (*KeyframeIndex, error) {
	cmd := exec.CommandContext(ctx, ffprobe,
		"-v", "error",
		"-select_streams", "V:0",
		"-show_entries", "packet=pts_time,flags",
		"-of", "csv=p=0",
		"file:"+input,
	)
	procutil.SetProcessGroup(cmd)
	cmd.Cancel = func() error { return procutil.KillProcessGroup(cmd) }
	cmd.WaitDelay = 5 * time.Second
	stderr := &stderrTail{}
	cmd.Stderr = stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ffprobe: %w", err)
	}
	idx, parseErr := parseKeyframePackets(out)
	// Drenar lo que quede para que ffprobe no se bloquee escribiendo.
	_, _ = io.Copy(io.Discard, out)
	waitErr := cmd.Wait()
	if waitErr != nil {
		return nil, fmt.Errorf("ffprobe keyframes: %w: %s", waitErr, strings.TrimSpace(stderr.String()))
	}
	if parseErr != nil {
		return nil, parseErr
	}
	return idx, nil
}

// KeyframeStore cachea índices de keyframes en memoria y en disco
// (clave = sha1(ruta|tamaño|mtime): si el fichero cambia, se reindexa) y
// los calcula en segundo plano con concurrencia acotada. Seguro para uso
// concurrente. Cerrar con Close.
type KeyframeStore struct {
	dir     string
	ffprobe string
	logger  *slog.Logger
	timeout time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	sem    chan struct{}

	mu       sync.Mutex
	mem      map[string]*KeyframeIndex
	failed   map[string]time.Time
	inflight map[string]chan struct{}
}

const (
	// keyframeMemEntries acota los índices en memoria (cada uno pesa
	// unas decenas de KB); al llenarse se vacía y se tira de disco.
	keyframeMemEntries = 256
	// keyframeFailTTL evita relanzar ffprobe en cada play de un fichero
	// que no se puede indexar.
	keyframeFailTTL = 10 * time.Minute
	// keyframeConcurrency: indexar es lectura secuencial del fichero
	// entero; más de dos a la vez solo compiten por el disco.
	keyframeConcurrency = 2
	// keyframeExtractTimeout cubre ficheros enormes en discos lentos.
	keyframeExtractTimeout = 15 * time.Minute
)

// NewKeyframeStore crea el store. dir vacío = solo memoria. ffprobe
// vacío = "ffprobe" del PATH.
func NewKeyframeStore(dir, ffprobe string, logger *slog.Logger) *KeyframeStore {
	if ffprobe == "" {
		ffprobe = "ffprobe"
	}
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &KeyframeStore{
		dir:      dir,
		ffprobe:  ffprobe,
		logger:   logger.With("module", "keyframes"),
		timeout:  keyframeExtractTimeout,
		ctx:      ctx,
		cancel:   cancel,
		sem:      make(chan struct{}, keyframeConcurrency),
		mem:      make(map[string]*KeyframeIndex),
		failed:   make(map[string]time.Time),
		inflight: make(map[string]chan struct{}),
	}
}

// keyframeCacheKey identifica una versión concreta del fichero.
func keyframeCacheKey(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	h := sha1.Sum([]byte(fmt.Sprintf("%s|%d|%d|v%d", path, info.Size(), info.ModTime().UnixNano(), keyframeIndexVersion)))
	return hex.EncodeToString(h[:]), nil
}

type keyframeFile struct {
	Version int          `json:"v"`
	MaxPTS  float64      `json:"max_pts"`
	Points  [][2]float64 `json:"points"` // [pts, lead]
}

func (s *KeyframeStore) diskPath(key string) string {
	return filepath.Join(s.dir, key+".json")
}

// Lookup devuelve el índice si ya está en memoria o en disco. Nunca
// lanza ffprobe.
func (s *KeyframeStore) Lookup(path string) (*KeyframeIndex, bool) {
	key, err := keyframeCacheKey(path)
	if err != nil {
		return nil, false
	}
	return s.lookupKey(key)
}

func (s *KeyframeStore) lookupKey(key string) (*KeyframeIndex, bool) {
	s.mu.Lock()
	idx, ok := s.mem[key]
	s.mu.Unlock()
	if ok {
		return idx, true
	}
	if s.dir == "" {
		return nil, false
	}
	raw, err := os.ReadFile(s.diskPath(key))
	if err != nil {
		return nil, false
	}
	var f keyframeFile
	if err := json.Unmarshal(raw, &f); err != nil || f.Version != keyframeIndexVersion || len(f.Points) == 0 {
		return nil, false
	}
	idx = &KeyframeIndex{MaxPTS: f.MaxPTS, Points: make([]KeyPoint, len(f.Points))}
	for i, p := range f.Points {
		idx.Points[i] = KeyPoint{PTS: p[0], Lead: p[1]}
	}
	s.remember(key, idx)
	return idx, true
}

func (s *KeyframeStore) remember(key string, idx *KeyframeIndex) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.mem) >= keyframeMemEntries {
		s.mem = make(map[string]*KeyframeIndex)
	}
	s.mem[key] = idx
}

func (s *KeyframeStore) persist(key string, idx *KeyframeIndex) {
	if s.dir == "" {
		return
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		s.logger.Warn("keyframe cache dir", "error", err)
		return
	}
	f := keyframeFile{Version: keyframeIndexVersion, MaxPTS: idx.MaxPTS, Points: make([][2]float64, len(idx.Points))}
	for i, p := range idx.Points {
		f.Points[i] = [2]float64{p.PTS, p.Lead}
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return
	}
	tmp := s.diskPath(key) + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		s.logger.Warn("keyframe cache write", "error", err)
		return
	}
	if err := os.Rename(tmp, s.diskPath(key)); err != nil {
		_ = os.Remove(tmp)
	}
}

// Warm lanza la indexación en segundo plano si el fichero no está
// indexado, ni en curso, ni falló hace poco. No bloquea.
func (s *KeyframeStore) Warm(path string) {
	_, _ = s.start(path)
}

// start devuelve el canal que se cierra cuando acaba la indexación del
// fichero (nil si ya está disponible o no se puede indexar ahora).
func (s *KeyframeStore) start(path string) (chan struct{}, string) {
	key, err := keyframeCacheKey(path)
	if err != nil {
		return nil, ""
	}
	if _, ok := s.lookupKey(key); ok {
		return nil, key
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ch, ok := s.inflight[key]; ok {
		return ch, key
	}
	if at, ok := s.failed[key]; ok && time.Since(at) < keyframeFailTTL {
		return nil, key
	}
	if s.ctx.Err() != nil {
		return nil, key
	}
	ch := make(chan struct{})
	s.inflight[key] = ch
	s.wg.Add(1)
	go s.run(key, path, ch)
	return ch, key
}

func (s *KeyframeStore) run(key, path string, done chan struct{}) {
	defer s.wg.Done()
	defer func() {
		s.mu.Lock()
		delete(s.inflight, key)
		s.mu.Unlock()
		close(done)
	}()
	select {
	case s.sem <- struct{}{}:
	case <-s.ctx.Done():
		return
	}
	defer func() { <-s.sem }()

	ctx, cancel := context.WithTimeout(s.ctx, s.timeout)
	defer cancel()
	began := time.Now()
	idx, err := extractKeyframes(ctx, s.ffprobe, path)
	if err != nil {
		if s.ctx.Err() == nil {
			s.logger.Warn("keyframe indexing failed", "path", path, "error", err)
			s.mu.Lock()
			s.failed[key] = time.Now()
			s.mu.Unlock()
		}
		return
	}
	s.remember(key, idx)
	s.persist(key, idx)
	s.logger.Info("keyframes indexed", "path", path, "keyframes", len(idx.Points), "took", time.Since(began).Round(time.Millisecond))
}

// Wait devuelve el índice, lanzándolo si hace falta y esperando como
// mucho budget (o hasta que ctx acabe). Si no llega a tiempo la
// indexación sigue en segundo plano y la siguiente reproducción lo
// encontrará hecho.
func (s *KeyframeStore) Wait(ctx context.Context, path string, budget time.Duration) (*KeyframeIndex, bool) {
	ch, key := s.start(path)
	if key == "" {
		return nil, false
	}
	if ch != nil {
		t := time.NewTimer(budget)
		defer t.Stop()
		select {
		case <-ch:
		case <-t.C:
			return nil, false
		case <-ctx.Done():
			return nil, false
		}
	}
	return s.lookupKey(key)
}

// Close cancela las indexaciones en curso y espera a que terminen.
func (s *KeyframeStore) Close() {
	s.cancel()
	s.wg.Wait()
}
