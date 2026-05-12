// Package sysmetrics samples host-level CPU% / RAM usage and probes
// the CPU + GPU model strings so the admin panel can show "what's
// actually under the hood". Built on top of gopsutil (pure-Go,
// cross-platform: Linux, Windows, macOS, FreeBSD) plus an optional
// nvidia-smi probe for NVIDIA GPU details.
//
// Why a background sampler instead of measuring inside the
// /admin/system/stats handler:
//
//   - cpu.Percent() is a delta measurement — it needs two reads spaced
//     by at least 100 ms to return a meaningful number. Doing that on
//     the handler thread would add a hard 100-ms tax to every poll
//     and serialise samples behind handler latency.
//   - The sampler runs at a fixed cadence regardless of how often the
//     admin panel polls, so the sparkline buffer in the React UI keeps
//     a clean cadence even if the admin closes and reopens the page.
//
// The snapshot is atomic.Value-backed so reads from the handler don't
// block the sampler and vice versa.
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

// HostInfo is the snapshot of one host introspection cycle. Both the
// static fields (CPU model, GPU model, RAM total, core counts) and
// the live fields (CPU%, RAM used) live in the same struct so the
// admin handler reads a single atomic value.
//
// Fields are tagged so the same shape can be emitted directly on the
// wire without a hand-rolled marshaller.
type HostInfo struct {
	// CPUModel is the human-readable CPU name from gopsutil's
	// cpu.Info() — first physical CPU's ModelName (e.g.
	// "AMD Ryzen 5 5600 6-Core Processor"). Empty on probe failure.
	CPUModel string `json:"cpu_model"`
	// CPUCoresPhysical is the count of physical cores. On hyper-
	// threaded CPUs it's half the logical-thread count. gopsutil
	// reads this from /proc/cpuinfo on Linux and WMI on Windows.
	// Zero on probe failure.
	CPUCoresPhysical int `json:"cpu_cores_physical"`
	// CPUCoresLogical is the logical-thread count (same as
	// runtime.NumCPU() on most platforms, but explicit so the
	// sparkline label can read "6 cores / 12 threads"). Always
	// non-zero — falls back to runtime.NumCPU() if gopsutil fails.
	CPUCoresLogical int `json:"cpu_cores_logical"`
	// CPUPercent is the host-wide CPU utilisation as a 0-100 float.
	// Sampled by the background goroutine on each tick; on the very
	// first tick it's 0 (a meaningful sample needs a delta vs the
	// previous read). Host-wide deliberately — what the operator
	// cares about for "can I add another transcode?" is whether
	// ANYTHING is consuming the box, not just hubplay.
	CPUPercent float64 `json:"cpu_percent"`
	// RAMTotalBytes is total physical RAM the OS reports. Static
	// for the life of the process.
	RAMTotalBytes uint64 `json:"ram_total_bytes"`
	// RAMUsedBytes is RAM in use (total - available). gopsutil's
	// "Used" field is misleading on Linux because it counts cache
	// as used; "Available" is the kernel's own honest estimate of
	// "RAM that can be reclaimed for new allocations". Total - Available
	// gives the "really in use" figure that matches what `free -h`
	// reports as used.
	RAMUsedBytes uint64 `json:"ram_used_bytes"`
	// GPUModel is the GPU description from the NVIDIA probe, when
	// the host has nvidia-smi and at least one NVIDIA GPU.
	// Example: "NVIDIA GeForce GTX 1660". Empty when no NVIDIA GPU
	// is present (or when the host has Intel / AMD / Apple Silicon
	// — those platforms have no standard model-name probe; the
	// existing HW-accel "VAAPI" / "VideoToolbox" badges cover them).
	GPUModel string `json:"gpu_model"`
	// GPUMemoryTotalBytes is total VRAM on the first NVIDIA GPU
	// (when present). Zero otherwise.
	GPUMemoryTotalBytes uint64 `json:"gpu_memory_total_bytes"`
	// GPUDriverVersion is the NVIDIA driver version string when
	// detected. Empty on non-NVIDIA hosts.
	GPUDriverVersion string `json:"gpu_driver_version"`
}

// Sampler runs the periodic host probe in a background goroutine and
// exposes the latest snapshot. Construct with New(), start with
// Start(ctx), read with Snapshot().
type Sampler struct {
	// snapshot is the latest HostInfo. Stored as atomic.Value so the
	// handler reads don't compete with the sampler's writes.
	snapshot atomic.Value // HostInfo
	interval time.Duration
	logger   *slog.Logger
	// nvidiaSMI is the path to the nvidia-smi binary used for the
	// one-shot GPU introspection. Empty disables NVIDIA probing.
	// Captured at construction so a host without nvidia-smi pays
	// zero cost (no repeated exec.LookPath calls).
	nvidiaSMI string
	// staticInfo holds the fields that never change for the life of
	// the process (CPU model, core counts, RAM total, GPU model).
	// Captured once at Start() so the sampler tick doesn't repeat
	// the slow probes (cpu.Info() spawns wmic on Windows).
	staticInfo HostInfo
}

// New constructs a Sampler. The interval is the wall-clock gap
// between CPU% / RAM samples; 5 s is a sensible default for the
// admin panel's polling cadence. Pass a logger for warning-level
// diagnostics (probe failures, sample errors).
//
// Construction itself is cheap — the slow probes (cpu.Info,
// nvidia-smi) run on Start() so a test rig can `New` a sampler and
// never call Start.
func New(interval time.Duration, logger *slog.Logger) *Sampler {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	// LookPath once at construction so the sampler doesn't keep
	// searching $PATH on every NVIDIA probe attempt. The probe still
	// runs unconditionally — the binary's presence doesn't guarantee
	// a working GPU (e.g. driver installed but no card seated).
	smi, _ := exec.LookPath("nvidia-smi")
	s := &Sampler{
		interval:  interval,
		logger:    logger.With("module", "sysmetrics"),
		nvidiaSMI: smi,
	}
	// Seed the snapshot with a runtime.NumCPU() fallback so a
	// caller that reads Snapshot() before Start() doesn't get an
	// empty struct.
	s.snapshot.Store(HostInfo{
		CPUCoresLogical: runtime.NumCPU(),
	})
	return s
}

// Start kicks off the background goroutine. Blocks briefly to run
// the one-shot static probes (CPU model, RAM total, GPU model) so
// the first call to Snapshot() returns a populated value. Returns
// immediately afterwards; the periodic sampler runs in its own
// goroutine until ctx is cancelled.
//
// Idempotent: calling Start twice on the same sampler is a no-op
// after the first call — the goroutine's lifetime is bound to the
// first ctx passed in.
func (s *Sampler) Start(ctx context.Context) {
	s.staticInfo = s.probeStatic()
	// Initial dynamic snapshot so the first /admin/system/stats poll
	// after boot returns non-zero CPU% (cpu.Percent with interval > 0
	// blocks until the delta is meaningful — that's the boot tax we
	// pay once instead of on every handler call).
	first := s.probeDynamic(250 * time.Millisecond)
	s.snapshot.Store(s.merge(first))

	go s.run(ctx)
}

// Snapshot returns the latest probe result. Safe to call from any
// goroutine; reads are non-blocking.
func (s *Sampler) Snapshot() HostInfo {
	if v := s.snapshot.Load(); v != nil {
		return v.(HostInfo)
	}
	return HostInfo{CPUCoresLogical: runtime.NumCPU()}
}

// run is the periodic sampling loop. Runs until ctx is cancelled.
// Each tick measures CPU% (host-wide, delta against the previous
// tick) + RAM used + writes the merged snapshot. The static
// fields (model strings, RAM total) are read from s.staticInfo so
// the slow probes don't repeat.
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

// probeStatic runs the slow / one-time probes that never change
// during the life of the process. Failures degrade gracefully —
// fields stay at their zero value and the panel renders an empty
// dash for that row.
func (s *Sampler) probeStatic() HostInfo {
	info := HostInfo{
		CPUCoresLogical: runtime.NumCPU(),
	}

	// CPU info: model name + physical core count.
	if cpus, err := cpu.Info(); err == nil && len(cpus) > 0 {
		info.CPUModel = strings.TrimSpace(cpus[0].ModelName)
		// Physical cores: gopsutil sums the Cores field across
		// every entry in cpu.Info(); on a single-socket machine
		// there's one entry whose Cores matches /proc/cpuinfo's
		// "cpu cores". On dual-socket, the slice has 2 entries.
		var physical int
		for _, c := range cpus {
			physical += int(c.Cores)
		}
		info.CPUCoresPhysical = physical
	} else if err != nil {
		s.logger.Debug("cpu.Info failed", "error", err)
	}

	// RAM total — pulled from gopsutil's virtual-memory stats. Used
	// + Available come from the dynamic probe so they refresh on
	// every tick.
	if vm, err := mem.VirtualMemory(); err == nil {
		info.RAMTotalBytes = vm.Total
	} else {
		s.logger.Debug("mem.VirtualMemory failed", "error", err)
	}

	// NVIDIA GPU probe. Best-effort, swallows errors so a host
	// without nvidia-smi just leaves the GPU fields empty.
	if s.nvidiaSMI != "" {
		if model, vram, driver := probeNVIDIA(s.nvidiaSMI, s.logger); model != "" {
			info.GPUModel = model
			info.GPUMemoryTotalBytes = vram
			info.GPUDriverVersion = driver
		}
	}

	return info
}

// probeDynamic samples the metrics that change over time: CPU%
// (host-wide) + RAM used. `cpuInterval` is the delta window for the
// CPU% measurement — pass 0 to use the time since the last call,
// or a positive duration to block for that long. The handler call
// site passes 0 (we want the delta against the previous tick);
// Start() passes a short interval so the first snapshot is non-zero.
func (s *Sampler) probeDynamic(cpuInterval time.Duration) HostInfo {
	var dyn HostInfo
	// cpu.Percent with interval=0 returns the delta since the previous
	// call (or 0 on the very first call ever). With interval>0 it
	// blocks for that long and returns the average.
	if pcts, err := cpu.Percent(cpuInterval, false); err == nil && len(pcts) > 0 {
		dyn.CPUPercent = pcts[0]
	} else if err != nil {
		s.logger.Debug("cpu.Percent failed", "error", err)
	}
	if vm, err := mem.VirtualMemory(); err == nil {
		// Total - Available matches "free -h"'s "used" column. See
		// the HostInfo.RAMUsedBytes doc for why this beats vm.Used.
		if vm.Total >= vm.Available {
			dyn.RAMUsedBytes = vm.Total - vm.Available
		}
	}
	return dyn
}

// merge combines the static (probe-once) fields with the dynamic
// (per-tick) ones into a single snapshot. Keeps Snapshot()'s contract
// simple: one read, fully populated value.
func (s *Sampler) merge(dyn HostInfo) HostInfo {
	out := s.staticInfo
	out.CPUPercent = dyn.CPUPercent
	out.RAMUsedBytes = dyn.RAMUsedBytes
	return out
}

// probeNVIDIA runs nvidia-smi once with a CSV query and parses the
// first line as the primary GPU's metadata. Returns empty strings /
// zero on any failure (no GPU, binary errored, output unparseable).
//
// nvidia-smi is reliable enough that we don't retry — if it failed
// at boot, the GPU isn't usable for HubPlay anyway and the panel
// reflects "no NVIDIA detected", same as if the binary were absent.
//
// Output shape (--format=csv,noheader,nounits):
//
//	NVIDIA GeForce GTX 1660, 6144, 560.35.03
//
// Memory is in MiB (the --format=csv default unit for memory.total);
// converted to bytes for wire shape consistency with RAMTotalBytes.
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
	// memory.total in MiB → bytes. Skip silently if the number is
	// unparseable; the panel will just show "—" for VRAM.
	if mib, perr := atoiUint(strings.TrimSpace(fields[1])); perr == nil {
		vramBytes = mib * 1024 * 1024
	}
	driver = strings.TrimSpace(fields[2])
	return model, vramBytes, driver
}

// atoiUint parses a non-negative integer from s. Returns an error on
// any character outside 0-9 or on overflow. Local helper to keep the
// strconv import out of this file — it's tiny and the surface area
// is "MiB count from nvidia-smi", never user input.
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

// Package-local sentinels — keep error.New off the hot path.
var (
	errEmptyNumber = &probeError{"empty number"}
	errBadNumber   = &probeError{"non-numeric byte"}
)

type probeError struct{ msg string }

func (e *probeError) Error() string { return e.msg }
