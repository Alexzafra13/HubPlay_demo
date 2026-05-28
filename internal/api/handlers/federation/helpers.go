package fedhandler

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// imageServer is the local interface the federation image handler
// needs from the media.ImageHandler. Only the methods actually called
// are listed, keeping the dependency narrow.
type imageServer interface {
	ServeImageByID(w http.ResponseWriter, r *http.Request, imageID string)
}

// validSegmentName matches only safe HLS segment filenames
// (e.g. segment00001.ts, stream.m3u8).
var validSegmentName = regexp.MustCompile(`^(segment\d{5}\.ts|stream\.m3u8)$`)

// waitForFile polls for a file to exist on disk, used to wait for
// FFmpeg output.
func waitForFile(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(path); err == nil && info.Size() > 0 {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for %s", filepath.Base(path))
}
