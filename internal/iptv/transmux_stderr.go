package iptv

// Bounded ring buffer for ffmpeg stderr. The transmux manager attaches
// one of these to each session and drains the process's stderr pipe
// into it; on session exit the captured tail is logged and fed to the
// codec-error classifier (transmux_codec_classify.go).

import (
	"bufio"
	"io"
	"strings"
	"sync"
)

// ffmpegStderrTailLines is how many stderr lines we keep per session.
// Sized to capture the cluster of warnings + the actual fatal line
// ffmpeg prints right before exiting (typically <20 lines on a real
// failure) without growing unbounded for long-running sessions that
// log warnings continuously over hours.
const ffmpegStderrTailLines = 64

// stderrScannerMaxLine bounds a single stderr line. ffmpeg's normal
// warning output stays well under 1 KiB, but `-loglevel debug`,
// builds that print full TLS chains, or codec-internal errors can
// emit much longer ones. A too-small max would make bufio.Scanner
// abort with ErrTooLong, the consumer goroutine would exit, the
// kernel pipe buffer would fill, and ffmpeg would block on write —
// effectively wedging the session until -rw_timeout fires upstream.
// 64 KiB is comfortably above anything observed in the wild and
// still small enough that holding one in a session is irrelevant.
const stderrScannerMaxLine = 64 * 1024

// stderrRing is a goroutine-safe FIFO of the last N stderr lines from
// ffmpeg. We use it instead of a full buffer because a session that
// runs for hours produces tens of thousands of warning lines and the
// only one operators ever care about is the fatal one right before
// exit. Newline-delimited.
//
// `done` is closed when the consumer goroutine exits (pipe EOF or
// reader error). processWatcher waits on it after cmd.Wait() before
// reading String() so the captured tail always includes the fatal
// line ffmpeg emits right before terminating — without this barrier
// Wait() can return before the consumer has flushed its last bytes,
// the breaker logs miss the actual cause, and looksLikeCodecError
// fires against an incomplete tail.
type stderrRing struct {
	mu    sync.Mutex
	lines []string
	max   int
	done  chan struct{}
}

func newStderrRing(max int) *stderrRing {
	if max <= 0 {
		max = 16
	}
	return &stderrRing{
		max:   max,
		lines: make([]string, 0, max),
		done:  make(chan struct{}),
	}
}

// consume reads from r line-by-line until EOF, keeping the last `max`
// lines. Lines that exceed stderrScannerMaxLine cause the scanner to
// stop, in which case we drain the rest of the pipe to /dev/null so
// ffmpeg's stderr write side never blocks. Closing `done` on return
// lets processWatcher synchronise with us before reading the tail.
func (r *stderrRing) consume(rd io.Reader) {
	defer close(r.done)
	scanner := bufio.NewScanner(rd)
	scanner.Buffer(make([]byte, 4096), stderrScannerMaxLine)
	for scanner.Scan() {
		r.push(scanner.Text())
	}
	// If Scan stopped because of ErrTooLong (or any other read error),
	// keep draining so the writer never blocks. EOF returns immediately.
	_, _ = io.Copy(io.Discard, rd)
}

// wait blocks until consume has returned. Safe on a nil receiver
// (returns immediately) so call sites don't need a guard for pre-
// spawn failure paths where the ring was never wired to a pipe.
func (r *stderrRing) wait() {
	if r == nil || r.done == nil {
		return
	}
	<-r.done
}

func (r *stderrRing) push(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.lines) >= r.max {
		// Drop the oldest line (FIFO). Slice trick: re-use the backing
		// array by shifting in place rather than allocating.
		copy(r.lines, r.lines[1:])
		r.lines = r.lines[:len(r.lines)-1]
	}
	r.lines = append(r.lines, line)
}

// String returns the accumulated tail joined by " | ". Empty when the
// session has not produced any stderr (ffmpeg is silent at -loglevel
// warning unless something goes wrong).
func (r *stderrRing) String() string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.lines) == 0 {
		return ""
	}
	return strings.Join(r.lines, " | ")
}
