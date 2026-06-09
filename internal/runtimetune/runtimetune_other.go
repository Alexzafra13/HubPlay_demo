//go:build !linux

package runtimetune

// cgroup limits are a Linux concept; on other platforms there is nothing
// to read, so Configure becomes a no-op.
func cpuQuota() float64 { return 0 }

func memoryLimit() int64 { return 0 }
