// Package sysmetrics mide CPU y RAM del equipo y averigua el modelo de
// CPU y GPU para mostrarlos en el panel admin. Usa gopsutil (que
// funciona en Linux, Windows y macOS) y opcionalmente nvidia-smi.
//
// La medida corre en segundo plano, no dentro del handler, para no
// añadir latencia a cada llamada del panel.
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

// HostInfo es lo que el panel admin lee de una vez. Mezcla datos fijos
// (modelo de CPU, RAM total) con datos vivos (uso de CPU y RAM).
type HostInfo struct {
	// Nombre del CPU. Vacío si no se pudo leer.
	CPUModel string `json:"cpu_model"`
	// Núcleos físicos. En procesadores con hyper-threading, la mitad
	// de los lógicos. 0 si no se pudo leer.
	CPUCoresPhysical int `json:"cpu_cores_physical"`
	// Núcleos lógicos (hilos). El panel muestra "6 núcleos / 12 hilos".
	CPUCoresLogical int `json:"cpu_cores_logical"`
	// Uso de CPU del equipo entero (0-100). En el primer tick es 0
	// porque hace falta un intervalo entre dos lecturas.
	CPUPercent float64 `json:"cpu_percent"`
	// RAM total. No cambia mientras corra el proceso.
	RAMTotalBytes uint64 `json:"ram_total_bytes"`
	// RAM realmente en uso (no cuenta cache, igual que `free -h`).
	RAMUsedBytes uint64 `json:"ram_used_bytes"`
	// Modelo de GPU NVIDIA si la hay (ej. "NVIDIA GeForce GTX 1660").
	// Vacío en equipos sin NVIDIA — para Intel/AMD/Apple no tenemos
	// forma estándar de leerlo.
	GPUModel string `json:"gpu_model"`
	// VRAM de la primera GPU NVIDIA. 0 si no hay.
	GPUMemoryTotalBytes uint64 `json:"gpu_memory_total_bytes"`
	// Versión del driver NVIDIA. Vacío en equipos no-NVIDIA.
	GPUDriverVersion string `json:"gpu_driver_version"`
}

// Sampler mide el equipo cada cierto tiempo y expone la última lectura.
// Flujo: New() → Start(ctx) → Snapshot().
type Sampler struct {
	// Última lectura completa.
	snapshot atomic.Value // HostInfo
	interval time.Duration
	logger   *slog.Logger
	// Ruta al binario nvidia-smi. Vacío si no está instalado.
	nvidiaSMI string
	// Datos que no cambian durante la vida del proceso (modelos, RAM total).
	// Se leen una sola vez al arrancar para no repetir trabajo lento.
	staticInfo HostInfo
}

// New construye un Sampler. interval es cada cuánto se toma muestra
// (por defecto, 5 s). La construcción es barata — el trabajo lento se
// hace en Start(), así un test puede crear uno sin arrancarlo.
func New(interval time.Duration, logger *slog.Logger) *Sampler {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	smi, _ := exec.LookPath("nvidia-smi")
	s := &Sampler{
		interval:  interval,
		logger:    logger.With("module", "sysmetrics"),
		nvidiaSMI: smi,
	}
	// Valor inicial: al menos el número de núcleos lógicos, por si alguien
	// llama a Snapshot() antes de Start().
	s.snapshot.Store(HostInfo{
		CPUCoresLogical: runtime.NumCPU(),
	})
	return s
}

// Start arranca el muestreo en segundo plano. Bloquea un momento al
// principio para leer los datos estáticos y una primera muestra, así el
// primer Snapshot() ya devuelve algo útil.
func (s *Sampler) Start(ctx context.Context) {
	s.staticInfo = s.probeStatic()
	// La primera lectura de CPU necesita un intervalo para tener
	// referencia; lo pagamos una vez al arrancar.
	first := s.probeDynamic(250 * time.Millisecond)
	s.snapshot.Store(s.merge(first))

	go s.run(ctx)
}

// Snapshot devuelve la última lectura. Es seguro llamarla desde cualquier
// sitio sin bloqueo.
func (s *Sampler) Snapshot() HostInfo {
	if v := s.snapshot.Load(); v != nil {
		return v.(HostInfo)
	}
	return HostInfo{CPUCoresLogical: runtime.NumCPU()}
}

// run es el bucle periódico que mide CPU y RAM. Termina cuando se
// cancela el contexto.
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

// probeStatic lee los datos lentos que sólo se calculan una vez. Si algo
// falla, el campo se queda a cero y el panel pinta "—" en esa fila.
func (s *Sampler) probeStatic() HostInfo {
	info := HostInfo{
		CPUCoresLogical: runtime.NumCPU(),
	}

	if cpus, err := cpu.Info(); err == nil && len(cpus) > 0 {
		info.CPUModel = strings.TrimSpace(cpus[0].ModelName)
		// Equipos con dos zócalos devuelven una entrada por zócalo;
		// sumamos para sacar el total de núcleos físicos.
		var physical int
		for _, c := range cpus {
			physical += int(c.Cores)
		}
		info.CPUCoresPhysical = physical
	} else if err != nil {
		s.logger.Debug("cpu.Info failed", "error", err)
	}

	if vm, err := mem.VirtualMemory(); err == nil {
		info.RAMTotalBytes = vm.Total
	} else {
		s.logger.Debug("mem.VirtualMemory failed", "error", err)
	}

	// Si no hay nvidia-smi, los campos GPU se quedan vacíos.
	if s.nvidiaSMI != "" {
		if model, vram, driver := probeNVIDIA(s.nvidiaSMI, s.logger); model != "" {
			info.GPUModel = model
			info.GPUMemoryTotalBytes = vram
			info.GPUDriverVersion = driver
		}
	}

	return info
}

// probeDynamic lee uso de CPU y RAM. Si cpuInterval es 0, calcula el
// uso respecto a la lectura anterior; si es mayor, bloquea ese tiempo
// y devuelve la media. El bucle pasa 0; Start() pasa un intervalo
// corto para que la primera muestra no sea 0.
func (s *Sampler) probeDynamic(cpuInterval time.Duration) HostInfo {
	var dyn HostInfo
	if pcts, err := cpu.Percent(cpuInterval, false); err == nil && len(pcts) > 0 {
		dyn.CPUPercent = pcts[0]
	} else if err != nil {
		s.logger.Debug("cpu.Percent failed", "error", err)
	}
	if vm, err := mem.VirtualMemory(); err == nil {
		if vm.Total >= vm.Available {
			dyn.RAMUsedBytes = vm.Total - vm.Available
		}
	}
	return dyn
}

// merge junta los datos fijos y los vivos en una sola lectura.
func (s *Sampler) merge(dyn HostInfo) HostInfo {
	out := s.staticInfo
	out.CPUPercent = dyn.CPUPercent
	out.RAMUsedBytes = dyn.RAMUsedBytes
	return out
}

// probeNVIDIA llama a nvidia-smi una sola vez y devuelve modelo, VRAM
// y versión del driver. Si algo falla, devuelve valores vacíos sin
// reintentar.
//
// La salida de nvidia-smi tiene este aspecto:
//	NVIDIA GeForce GTX 1660, 6144, 560.35.03
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
	// nvidia-smi devuelve la VRAM en MiB; la convertimos a bytes.
	if mib, perr := atoiUint(strings.TrimSpace(fields[1])); perr == nil {
		vramBytes = mib * 1024 * 1024
	}
	driver = strings.TrimSpace(fields[2])
	return model, vramBytes, driver
}

// atoiUint convierte un entero positivo sin tener que importar strconv;
// la entrada viene de nvidia-smi, nunca del usuario.
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

// Errores locales del paquete, fuera de la ruta caliente.
var (
	errEmptyNumber = &probeError{"empty number"}
	errBadNumber   = &probeError{"non-numeric byte"}
)

type probeError struct{ msg string }

func (e *probeError) Error() string { return e.msg }
