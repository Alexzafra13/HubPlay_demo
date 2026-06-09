// Package runtimetune aligns the Go runtime with the container's cgroup
// CPU and memory limits so HubPlay behaves well in constrained
// environments (single-board computers, NAS boxes, CPU/memory-capped
// containers) with zero manual tuning.
//
// Why this is needed: runtime.NumCPU() honours CPU *affinity*
// (--cpuset-cpus) but NOT the CFS *quota* (`docker --cpus=2`, k8s
// limits.cpu) that most deployments actually use. On a 16-core host
// running with --cpus=2 the Go scheduler would otherwise create 16 Ps
// and the autotuner would size transcode sessions for 16 cores. And with
// no GOMEMLIMIT the GC lets the heap grow until the kernel OOM-kills the
// process — the classic "works on my 32 GB box, gets killed on the 1 GB
// Pi" failure.
package runtimetune

import (
	"log/slog"
	"math"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
)

// memLimitHeadroom leaves room below the hard cgroup limit for non-heap
// memory (goroutine stacks, runtime structures) so the soft GOMEMLIMIT
// triggers GC before the kernel OOM-kills us. ffmpeg runs as a separate
// process and is accounted separately, so we don't reserve for it here.
const memLimitHeadroom = 0.9

// Configure reads the cgroup CPU/memory limits and applies them via
// GOMAXPROCS and GOMEMLIMIT. Operator-set GOMAXPROCS / GOMEMLIMIT env
// vars always win (we skip when present). No-op on non-Linux and when no
// limit is set (bare metal / unconstrained). Call once, early in boot,
// before goroutines or the streaming autotuner read GOMAXPROCS.
func Configure(logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}

	if os.Getenv("GOMAXPROCS") == "" {
		if quota := cpuQuota(); quota > 0 {
			procs := int(math.Ceil(quota))
			if procs < 1 {
				procs = 1
			}
			if procs < runtime.NumCPU() {
				runtime.GOMAXPROCS(procs)
				logger.Info("GOMAXPROCS tuned from cgroup CPU quota",
					"cpu_quota", quota, "gomaxprocs", procs, "host_cpus", runtime.NumCPU())
			}
		}
	}

	if os.Getenv("GOMEMLIMIT") == "" {
		if limit := memoryLimit(); limit > 0 {
			soft := int64(float64(limit) * memLimitHeadroom)
			debug.SetMemoryLimit(soft)
			logger.Info("GOMEMLIMIT tuned from cgroup memory limit",
				"limit_bytes", limit, "soft_limit_bytes", soft)
		}
	}
}

// parseCPUMax parses a cgroup-v2 cpu.max value ("<quota_us> <period_us>"
// or "max <period_us>") into an effective CPU count. Returns (0, false)
// when unlimited or unparseable.
func parseCPUMax(content string) (float64, bool) {
	f := strings.Fields(strings.TrimSpace(content))
	if len(f) != 2 || f[0] == "max" {
		return 0, false
	}
	quota, err1 := strconv.ParseInt(f[0], 10, 64)
	period, err2 := strconv.ParseInt(f[1], 10, 64)
	if err1 != nil || err2 != nil || quota <= 0 || period <= 0 {
		return 0, false
	}
	return float64(quota) / float64(period), true
}

// cpuRatio computes an effective CPU count from a v1 quota/period pair.
func cpuRatio(quota, period int64) (float64, bool) {
	if quota <= 0 || period <= 0 {
		return 0, false
	}
	return float64(quota) / float64(period), true
}

// parseMemLimit parses a cgroup memory limit file value. cgroup v2 uses
// the literal "max" for unlimited; cgroup v1 uses a near-int64-max
// sentinel. Both map to (0, false).
func parseMemLimit(content string) (int64, bool) {
	s := strings.TrimSpace(content)
	if s == "" || s == "max" {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	// v1 "unlimited" sentinel (PAGE_COUNTER_MAX rounded) is enormous;
	// treat anything past 2^62 as no real limit.
	if n >= (int64(1) << 62) {
		return 0, false
	}
	return n, true
}
