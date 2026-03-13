package stream

// Profile defines a transcoding quality profile.
type Profile struct {
	Name       string
	Width      int
	Height     int
	VideoBitrate string // e.g. "4000k"
	AudioBitrate string // e.g. "192k"
	MaxFrameRate int
}

// Profiles maps profile names to their definitions.
var Profiles = map[string]Profile{
	"1080p": {Name: "1080p", Width: 1920, Height: 1080, VideoBitrate: "4000k", AudioBitrate: "192k", MaxFrameRate: 30},
	"720p":  {Name: "720p", Width: 1280, Height: 720, VideoBitrate: "2500k", AudioBitrate: "128k", MaxFrameRate: 30},
	"480p":  {Name: "480p", Width: 854, Height: 480, VideoBitrate: "1200k", AudioBitrate: "128k", MaxFrameRate: 30},
	"360p":  {Name: "360p", Width: 640, Height: 360, VideoBitrate: "600k", AudioBitrate: "96k", MaxFrameRate: 30},
	"original": {Name: "original"}, // Direct play, no transcoding
}

// ProfileNames returns sorted profile names from highest to lowest quality.
func ProfileNames() []string {
	return []string{"original", "1080p", "720p", "480p", "360p"}
}

// DefaultProfile returns the default transcoding profile.
func DefaultProfile() Profile {
	return Profiles["720p"]
}
