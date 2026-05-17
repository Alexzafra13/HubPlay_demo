// Package sysmetrics: sampler de CPU%/RAM + probe de modelos CPU/GPU para
// el panel admin. Usa gopsutil (pure-Go, multiplataforma) + nvidia-smi opcional.
//
// Sampler en background (no en el handler) por dos razones:
//   - cpu.Percent() necesita 2 reads espaciados ≥100 ms; medir en el handler
//     añadiría 100 ms a cada poll y serializaría samples tras la latencia.
//   - cadencia fija independiente del polling del admin — la sparkline mantiene
//     ritmo limpio aunque se cierre y reabra la página.
//
// Snapshot vía atomic.Value: reads del handler no bloquean al sampler ni viceversa.
package sysmetrics

import (
	"context"
	"log/slog"
	"os/exec"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
)

// HostInfo: snapshot de un ciclo de probe. Mezcla campos estáticos (modelos,
// RAM total) con live (CPU%, RAM used) para que el handler haga 1 sola lectura
// atómica. Tags JSON → emitible directo al wire.
type HostInfo struct {
	// CPUModel: ModelName del primer CPU físico vía cpu.Info()
	// ("AMD Ryzen 5 5600..."). "" si el probe falla.
	CPUModel string `json:"cpu_model"`
	// CPUCoresPhysical: cores físicos (en hyper-threaded = mitad de los lógicos).
	// gopsutil lo lee de /proc/cpuinfo o WMI. 0 si falla.
	CPUCoresPhysical int `json:"cpu_cores_physical"`
	// CPUCoresLogical: threads lógicos (== runtime.NumCPU() casi siempre).
	// Explícito para que la label diga "6 cores / 12 threads". Nunca 0 — fallback
	// a runtime.NumCPU().
	CPUCoresLogical int `json:"cpu_cores_logical"`
	// CPUPercent: utilización host-wide 0-100. Sample por tick; en el primer
	// tick es 0 (necesita delta). Host-wide a propósito: "¿puedo añadir otro
	// transcode?" depende de quién sea quien esté consumiendo, no solo hubplay.
	CPUPercent float64 `json:"cpu_percent"`
	// RAMTotalBytes: estático durante la vida del proceso.
	RAMTotalBytes uint64 `json:"ram_total_bytes"`
	// RAMUsedBytes: Total - Available. El campo "Used" de gopsutil engaña en
	// Linux porque cuenta cache como usado; "Available" es la estimación honesta
	// del kernel de "lo que se puede reclamar". Total-Available = lo que `free -h`
	// reporta como used.
	RAMUsedBytes uint64 `json:"ram_used_bytes"`
	// GPUModel: descripción de la NVIDIA probe (ej. "NVIDIA GeForce GTX 1660").
	// "" en hosts sin NVIDIA (Intel/AMD/Apple Silicon no tienen probe estándar;
	// los badges "VAAPI"/"VideoToolbox" los cubren igual).
	GPUModel string `json:"gpu_model"`
	// GPUMemoryTotalBytes: VRAM de la primera NVIDIA. 0 si no hay.
	GPUMemoryTotalBytes uint64 `json:"gpu_memory_total_bytes"`
	// GPUDriverVersion: driver NVIDIA. "" en hosts no-NVIDIA.
	GPUDriverVersion string `json:"gpu_driver_version"`
}

// Sampler: probe periódico en background; expone el último snapshot.
// New() → Start(ctx) → Snapshot().
type Sampler struct {
	// snapshot: último HostInfo. atomic.Value → reads del handler no compiten
	// con writes del sampler.
	snapshot atomic.Value // HostInfo
	interval time.Duration
	logger   *slog.Logger
	// nvidiaSMI: path al binario nvidia-smi. "" desactiva el probe NVIDIA.
	// Capturado en construcción para no repetir LookPath en cada probe.
	nvidiaSMI string
	// staticInfo: campos que no cambian en la vida del proceso. Capturados 1
	// vez en Start() para que el tick no repita probes lentos (cpu.Info()
	// spawnea wmic en Windows).
	staticInfo HostInfo
}

// New: interval entre samples (default 5 s = cadencia del panel admin).
// Construcción barata — los probes lentos (cpu.Info, nvidia-smi) corren en
// Start(), así un test puede New() sin Start().
func New(interval time.Duration, logger *slog.Logger) *Sampler {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	// LookPath 1 vez en construcción — el probe sigue corriendo aunque exista
	// el binario (puede haber driver sin tarjeta).
	smi, _ := exec.LookPath("nvidia-smi")
	s := &Sampler{
		interval:  interval,
		logger:    logger.With("module", "sysmetrics"),
		nvidiaSMI: smi,
	}
	// Seed con fallback a runtime.NumCPU() para que un Snapshot() pre-Start()
	// no devuelva struct vacío.
	s.snapshot.Store(HostInfo{
		CPUCoresLogical: runtime.NumCPU(),
	})
	return s
}

// Start: bloquea brevemente para los probes estáticos (modelos, RAM total) y
// para un primer probe dinámico, así el primer Snapshot() ya viene poblado.
// Goroutine periódica corre hasta cancel del ctx.
//
// No idempotente: 2º Start es no-op pero la vida de la goroutine va atada
// al primer ctx.
func (s *Sampler) Start(ctx context.Context) {
	s.staticInfo = s.probeStatic()
	// Probe dinámico inicial: cpu.Percent con interval>0 bloquea hasta tener
	// un delta. Pagamos el peaje al boot 1 vez en vez de en cada handler.
	first := s.probeDynamic(250 * time.Millisecond)
	s.snapshot.Store(s.merge(first))

	go s.run(ctx)
}

// Snapshot: thread-safe y no-bloqueante.
func (s *Sampler) Snapshot() HostInfo {
	if v := s.snapshot.Load(); v != nil {
		return v.(HostInfo)
	}
	return HostInfo{CPUCoresLogical: runtime.NumCPU()}
}

// run: loop periódico hasta cancel del ctx. Cada tick mide CPU% (delta vs
// tick previo) + RAM used. Los campos estáticos vienen de s.staticInfo —
// los probes lentos no se repiten.
func (s *Sampler) run(ctx context.Context) {
	t := time.NewTicker(s.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			dyn := s.probeDynamic(0)
			s.snapshot.Store(s.merge(dyn))
		}
	}
}

// probeStatic: probes lentos one-shot. Fallos degradan suave (campos en zero
// value → el panel pinta "—" en esa fila).
func (s *Sampler) probeStatic() HostInfo {
	info := HostInfo{
		CPUCoresLogical: runtime.NumCPU(),
	}

	if cpus, err := cpu.Info(); err == nil && len(cpus) > 0 {
		info.CPUModel = strings.TrimSpace(cpus[0].ModelName)
		// Cores físicos: gopsutil suma Cores de cada entry. Single-socket = 1
		// entry, dual-socket = 2.
		var physical int
		for _, c := range cpus {
			physical += int(c.Cores)
		}
		info.CPUCoresPhysical = physical
	} else if err != nil {
		s.logger.Debug("cpu.Info failed", "error", err)
	}

	// RAM total estático; Used + Available vienen del probe dinámico.
	if vm, err := mem.VirtualMemory(); err == nil {
		info.RAMTotalBytes = vm.Total
	} else {
		s.logger.Debug("mem.VirtualMemory failed", "error", err)
	}

	// Probe NVIDIA best-effort; sin nvidia-smi → campos GPU vacíos.
	if s.nvidiaSMI != "" {
		if model, vram, driver := probeNVIDIA(s.nvidiaSMI, s.logger); model != "" {
			info.GPUModel = model
			info.GPUMemoryTotalBytes = vram
			info.GPUDriverVersion = driver
		}
	}

	return info
}

// probeDynamic: CPU% (host-wide) + RAM used. cpuInterval=0 → delta desde la
// llamada previa (o 0 en la primera). >0 → bloquea ese tiempo y devuelve la
// media. El tick pasa 0; Start() pasa intervalo corto para que el primer
// snapshot no sea 0.
func (s *Sampler) probeDynamic(cpuInterval time.Duration) HostInfo {
	var dyn HostInfo
	if pcts, err := cpu.Percent(cpuInterval, false); err == nil && len(pcts) > 0 {
		dyn.CPUPercent = pcts[0]
	} else if err != nil {
		s.logger.Debug("cpu.Percent failed", "error", err)
	}
	if vm, err := mem.VirtualMemory(); err == nil {
		// Total - Available = columna "used" de `free -h` (ver HostInfo.RAMUsedBytes).
		if vm.Total >= vm.Available {
			dyn.RAMUsedBytes = vm.Total - vm.Available
		}
	}
	return dyn
}

// merge: junta static + dynamic en un solo snapshot — Snapshot() es 1 read.
func (s *Sampler) merge(dyn HostInfo) HostInfo {
	out := s.staticInfo
	out.CPUPercent = dyn.CPUPercent
	out.RAMUsedBytes = dyn.RAMUsedBytes
	return out
}

// probeNVIDIA: 1 sola llamada a nvidia-smi en CSV. Devuelve "" / 0 ante
// cualquier fallo. No retry: si falló al boot, la GPU no es usable para
// HubPlay igual.
//
// Output (--format=csv,noheader,nounits):
//	NVIDIA GeForce GTX 1660, 6144, 560.35.03
//
// Memory en MiB → bytes para consistencia con RAMTotalBytes.
func probeNVIDIA(smiPath string, logger *slog.Logger) (model string, vramBytes uint64, driver string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, smiPath,
		"--query-gpu=name,memory.total,driver_version",
		"--format=csv,noheader,nounits",
	)
	out, err := cmd.Output()
	if err != nil {
		logger.Debug("nvidia-smi probe failed", "error", err)
		return "", 0, ""
	}
	lines := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)
	if len(lines) == 0 || lines[0] == "" {
		return "", 0, ""
	}
	fields := strings.Split(lines[0], ",")
	if len(fields) < 3 {
		return "", 0, ""
	}
	model = strings.TrimSpace(fields[0])
	// MiB → bytes. Si no parsea, el panel pinta "—" en VRAM.
	if mib, perr := atoiUint(strings.TrimSpace(fields[1])); perr == nil {
		vramBytes = mib * 1024 * 1024
	}
	driver = strings.TrimSpace(fields[2])
	return model, vramBytes, driver
}

// atoiUint: parse de uint sin importar strconv (input acotado, nunca usuario).
func atoiUint(s string) (uint64, error) {
	if s == "" {
		return 0, errEmptyNumber
	}
	var n uint64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errBadNumber
		}
		n = n*10 + uint64(c-'0')
	}
	return n, nil
}

// Sentinels package-local — fuera del hot path.
var (
	errEmptyNumber = &probeError{"empty number"}
	errBadNumber   = &probeError{"non-numeric byte"}
)

type probeError struct{ msg string }

func (e *probeError) Error() string { return e.msg }
