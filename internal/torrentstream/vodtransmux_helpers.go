package torrentstream

import (
	"bufio"
	"io"
	"log/slog"
	"regexp"
	"sync/atomic"
	"time"
)

// atomicTime is a lock-free time holder for the per-session last-access stamp.
type atomicTime struct{ ns atomic.Int64 }

func (a *atomicTime) set(t time.Time) { a.ns.Store(t.UnixNano()) }
func (a *atomicTime) get() time.Time  { return time.Unix(0, a.ns.Load()) }

// segmentNamePattern matches the seg-NNNNN.ts names ffmpeg writes for the HLS
// muxer; used as a path-traversal guard before serving a segment from disk.
var segmentNamePattern = regexp.MustCompile(`^seg-\d{5,6}\.ts$`)

// IsValidSegmentName reports whether name is a safe HLS segment filename
// (rejects path traversal / arbitrary files).
func IsValidSegmentName(name string) bool {
	return segmentNamePattern.MatchString(name)
}

// newVODStderr returns a writer that forwards ffmpeg's stderr lines to the
// logger at debug level, tagged with the session key. ffmpeg is chatty; we
// keep it out of warn/error so a normal transcode doesn't spam the log.
// The caller MUST Close the returned writer once the process has exited so
// the scanner goroutine terminates.
func newVODStderr(logger *slog.Logger, key string) *io.PipeWriter {
	pr, pw := io.Pipe()
	go func() {
		sc := bufio.NewScanner(pr)
		for sc.Scan() {
			logger.Debug("vod ffmpeg", "session", key, "line", sc.Text())
		}
	}()
	return pw
}
