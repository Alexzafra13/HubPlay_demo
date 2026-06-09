//go:build linux

package runtimetune

import (
	"os"
	"strconv"
	"strings"
)

// cpuQuota returns the effective CPU count allowed by the cgroup CFS
// quota (v2 then v1), or 0 when unlimited / unreadable.
func cpuQuota() float64 {
	// cgroup v2.
	if b, err := os.ReadFile("/sys/fs/cgroup/cpu.max"); err == nil {
		if v, ok := parseCPUMax(string(b)); ok {
			return v
		}
		return 0 // file present (v2) but "max" → unlimited
	}
	// cgroup v1.
	quota := readCgroupInt("/sys/fs/cgroup/cpu/cpu.cfs_quota_us")
	period := readCgroupInt("/sys/fs/cgroup/cpu/cpu.cfs_period_us")
	if v, ok := cpuRatio(quota, period); ok {
		return v
	}
	return 0
}

// memoryLimit returns the cgroup memory limit in bytes (v2 then v1), or 0
// when unlimited / unreadable.
func memoryLimit() int64 {
	if b, err := os.ReadFile("/sys/fs/cgroup/memory.max"); err == nil {
		if v, ok := parseMemLimit(string(b)); ok {
			return v
		}
		return 0
	}
	if b, err := os.ReadFile("/sys/fs/cgroup/memory/memory.limit_in_bytes"); err == nil {
		if v, ok := parseMemLimit(string(b)); ok {
			return v
		}
	}
	return 0
}

func readCgroupInt(path string) int64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return 0
	}
	return n
}
