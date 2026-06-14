package torrentstream

import (
	"io"
	"sync/atomic"
	"time"

	"github.com/anacrolix/torrent"
)

// Session is one live torrent being streamed. Shared across viewers of
// the same content. All fields are read-only after construction except
// lastTouchUnixNano, which is updated atomically on every read so the
// idle reaper can decide when to close it.
type Session struct {
	infoHash  string
	torrent   *torrent.Torrent
	file      *torrent.File
	readahead int64

	lastTouchUnixNano atomic.Int64
}

// InfoHash returns the torrent's infohash (hex) — the session key.
func (s *Session) InfoHash() string { return s.infoHash }

// Name is the torrent's display name.
func (s *Session) Name() string {
	if s.torrent == nil {
		return ""
	}
	return s.torrent.Name()
}

// FileName is the streamed file's path within the torrent.
func (s *Session) FileName() string {
	if s.file == nil {
		return ""
	}
	return s.file.DisplayPath()
}

// Length is the streamed file's size in bytes.
func (s *Session) Length() int64 {
	if s.file == nil {
		return 0
	}
	return s.file.Length()
}

// BytesCompleted is how much of the streamed file has downloaded so far.
func (s *Session) BytesCompleted() int64 {
	if s.file == nil {
		return 0
	}
	return s.file.BytesCompleted()
}

// ContentType guesses a Content-Type from the file name for the player.
func (s *Session) ContentType() string { return guessContentType(s.FileName()) }

// Reader opens a sequential, responsive reader over the streamed file.
// SetReadahead pulls a window ahead of the read cursor and SetResponsive
// returns reads as soon as the needed piece lands — together that is what
// lets playback start before the whole file is downloaded. The returned
// reader is an io.ReadSeekCloser, so http.ServeContent can satisfy Range
// requests (seeking). Callers MUST Close it.
func (s *Session) Reader() io.ReadSeekCloser {
	r := s.file.NewReader()
	r.SetReadahead(s.readahead)
	r.SetResponsive()
	return r
}

func (s *Session) touch(now time.Time) {
	s.lastTouchUnixNano.Store(now.UnixNano())
}

// Touch marks the session as recently used (called on every stream
// request) so the idle reaper keeps it alive while someone is watching.
func (s *Session) Touch() { s.touch(time.Now()) }

// idle reports whether the session has gone untouched for longer than
// timeout as of now.
func (s *Session) idle(now time.Time, timeout time.Duration) bool {
	last := s.lastTouchUnixNano.Load()
	return now.Sub(time.Unix(0, last)) > timeout
}
