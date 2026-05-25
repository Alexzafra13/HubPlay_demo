package stream

// Profile define un perfil de calidad de transcodificación.
type Profile struct {
	Name       string
	Width      int
	Height     int
	VideoBitrate string // ej. "4000k"
	AudioBitrate string // ej. "192k"
	MaxFrameRate int
}

// Profiles mapea nombres de perfil a sus definiciones.
var Profiles = map[string]Profile{
	"1080p": {Name: "1080p", Width: 1920, Height: 1080, VideoBitrate: "4000k", AudioBitrate: "192k", MaxFrameRate: 30},
	"720p":  {Name: "720p", Width: 1280, Height: 720, VideoBitrate: "2500k", AudioBitrate: "128k", MaxFrameRate: 30},
	"480p":  {Name: "480p", Width: 854, Height: 480, VideoBitrate: "1200k", AudioBitrate: "128k", MaxFrameRate: 30},
	"360p":  {Name: "360p", Width: 640, Height: 360, VideoBitrate: "600k", AudioBitrate: "96k", MaxFrameRate: 30},
	"original": {Name: "original"}, // Reproducción directa, sin transcodificación
}

// ProfileNames devuelve nombres de perfil ordenados de mayor a menor calidad.
func ProfileNames() []string {
	return []string{"original", "1080p", "720p", "480p", "360p"}
}

// DefaultProfile devuelve el perfil de transcodificación por defecto.
func DefaultProfile() Profile {
	return Profiles["720p"]
}
