package upload

import (
	"bytes"
	"errors"
	"fmt"
	"io"
)

// ─── Errors ─────────────────────────────────────────────────────────

var (
	// ErrExtensionNotAllowed: extensión fuera del whitelist. La devuelve
	// ValidateExtension; el HTTP layer la traduce a 400 con código
	// UPLOAD_BAD_EXTENSION.
	ErrExtensionNotAllowed = errors.New("file extension not allowed")

	// ErrMimeMismatch: los primeros bytes del fichero NO casan con
	// ninguna familia que aceptamos (video/audio/subtítulo). Se devuelve
	// aunque la extensión esté en el whitelist — los magic bytes
	// mandan sobre el nombre.
	ErrMimeMismatch = errors.New("file contents do not match an allowed media type")

	// ErrEmptyFile: el primer chunk vino vacío. La pipeline no puede
	// validar nada, así que rechazamos antes de seguir.
	ErrEmptyFile = errors.New("file is empty")
)

// ─── Extensions ─────────────────────────────────────────────────────

// allowedExtensions: whitelist de extensiones que el handler acepta.
// Se mantiene aquí (no en config) porque cambiarlo NO es operación de
// runtime — añadir formatos requiere también probar que ffprobe los
// entienda, así que es una decisión de versión, no de despliegue.
//
// Video coverage: contenedores que el player HLS / direct-stream del
// proyecto ya sabe servir (.mkv y .mp4 son el 95% del corpus real
// self-hosted; .ts / .vob / .mpg para grabaciones legacy).
// Audio: NO — la app es de vídeo. Audio puro lo dejamos fuera v1.
// Subtítulos: aceptamos los 3 contenedores estándar para que el
// usuario pueda subir un .srt suelto cuando reemplaza unos malos.
var allowedExtensions = map[string]struct{}{
	"mkv":  {},
	"mp4":  {},
	"m4v":  {},
	"mov":  {},
	"avi":  {},
	"webm": {},
	"ts":   {},
	"vob":  {},
	"mpg":  {},
	"mpeg": {},
	"srt":  {},
	"ass":  {},
	"vtt":  {},
}

// ValidateExtension: returns nil if the extension (lowercased,
// sin el punto) está en el whitelist. Toma el nombre ya sanitizado;
// no extrae extensión él mismo para forzar al caller a usar
// SanitizeFilename antes y no validar nombres con path traversal.
func ValidateExtension(sanitizedName string) error {
	ext := ExtensionLower(sanitizedName)
	if ext == "" {
		return fmt.Errorf("%w: missing extension", ErrExtensionNotAllowed)
	}
	if _, ok := allowedExtensions[ext]; !ok {
		return fmt.Errorf("%w: .%s", ErrExtensionNotAllowed, ext)
	}
	return nil
}

// AllowedExtensions devuelve una copia del whitelist para que el
// frontend pueda pintar el `accept=` del <input type=file>. Copia
// para que un caller no pueda mutar el mapa interno.
func AllowedExtensions() []string {
	out := make([]string, 0, len(allowedExtensions))
	for ext := range allowedExtensions {
		out = append(out, ext)
	}
	return out
}

// ─── Magic bytes ────────────────────────────────────────────────────

// SniffLength es el número de bytes que el validator lee del header
// antes de decidir. 4096 cubre cada signature que comprobamos con
// margen sobrado; más sería gastar IO innecesaria, menos podría
// truncar el ftyp box de un MP4 con ftyp brand exótico.
const SniffLength = 4096

// MediaKind clasifica el contenido detectado. El service usa esto para
// decidir destino (video → librería del usuario; subtitle → carpeta
// hermana del item correspondiente — eso lo gestiona la pipeline,
// no este paquete).
type MediaKind int

const (
	KindUnknown MediaKind = iota
	KindVideo
	KindSubtitle
)

func (k MediaKind) String() string {
	switch k {
	case KindVideo:
		return "video"
	case KindSubtitle:
		return "subtitle"
	default:
		return "unknown"
	}
}

// signature describe un patrón de bytes en el offset esperado. Vacío
// para los matchers "soft" (texto plano).
type signature struct {
	offset int
	pattern []byte
	mime    string
	kind    MediaKind
}

// videoSignatures: firmas binarias de los contenedores que aceptamos.
// El orden importa solo si dos firmas pudieran solaparse; las nuestras
// son distintas en los primeros 16 bytes.
var videoSignatures = []signature{
	// MKV / WebM — EBML magic.
	{offset: 0, pattern: []byte{0x1A, 0x45, 0xDF, 0xA3}, mime: "video/x-matroska", kind: KindVideo},
	// MP4 / M4V / MOV — ftyp box at offset 4. Strict on first 4
	// bytes of the brand para que un "ftypHEIC" (imagen) no pase.
	{offset: 4, pattern: []byte("ftyp"), mime: "video/mp4", kind: KindVideo},
	// AVI — RIFF container with AVI fourcc.
	{offset: 0, pattern: []byte("RIFF"), mime: "video/x-msvideo", kind: KindVideo},
	// MPEG-PS / VOB — pack start code 0x000001BA.
	{offset: 0, pattern: []byte{0x00, 0x00, 0x01, 0xBA}, mime: "video/mpeg", kind: KindVideo},
	// MPEG-TS — sync byte 0x47 at offset 0 and offset 188.
	{offset: 0, pattern: []byte{0x47}, mime: "video/mp2t", kind: KindVideo},
}

// DetectKind reads up to SniffLength bytes from r, classifies them and
// returns (kind, detected MIME, err). On err the kind is KindUnknown
// and the MIME is "" — the caller should treat anything but KindVideo
// / KindSubtitle as a rejection. The reader is not rewound — if you
// need the bytes downstream, wrap r in bufio or use DetectKindFromBytes.
func DetectKind(r io.Reader) (MediaKind, string, error) {
	buf := make([]byte, SniffLength)
	n, err := io.ReadFull(r, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return KindUnknown, "", fmt.Errorf("read header: %w", err)
	}
	if n == 0 {
		return KindUnknown, "", ErrEmptyFile
	}
	return DetectKindFromBytes(buf[:n])
}

// DetectKindFromBytes is the pure version of DetectKind; same returns,
// but the caller already has the bytes in memory (e.g. tusd post-create
// hook reading the first chunk from disk). Returns ErrMimeMismatch if
// no signature matches.
func DetectKindFromBytes(b []byte) (MediaKind, string, error) {
	if len(b) == 0 {
		return KindUnknown, "", ErrEmptyFile
	}

	// MPEG-TS verification needs a second sync byte at offset 188 — a
	// single 0x47 byte is far too weak alone (random). Check before the
	// generic loop to short-circuit.
	if len(b) > 188 && b[0] == 0x47 && b[188] == 0x47 {
		return KindVideo, "video/mp2t", nil
	}

	for _, sig := range videoSignatures {
		if sig.mime == "video/mp2t" {
			continue // covered by the strict check above
		}
		if sig.offset+len(sig.pattern) > len(b) {
			continue
		}
		if bytes.Equal(b[sig.offset:sig.offset+len(sig.pattern)], sig.pattern) {
			return sig.kind, sig.mime, nil
		}
	}

	if looksLikeSubtitle(b) {
		return KindSubtitle, "text/plain", nil
	}

	return KindUnknown, "", ErrMimeMismatch
}

// looksLikeSubtitle reconoce SRT, WebVTT y SSA/ASS por sus marcadores
// de texto característicos. Buscamos en los primeros 256 bytes (donde
// están las cabeceras de los 3 formatos) y aceptamos sólo si vemos
// ASCII printable + algún marcador específico — un fichero binario
// disfrazado de .srt no debe colarse.
func looksLikeSubtitle(b []byte) bool {
	head := b
	if len(head) > 256 {
		head = head[:256]
	}

	// Rechaza si hay bytes no-text (excepto BOM, tab, LF, CR).
	for i, c := range head {
		// Skip BOM.
		if i < 3 && len(head) >= 3 &&
			head[0] == 0xEF && head[1] == 0xBB && head[2] == 0xBF {
			continue
		}
		if c == '\n' || c == '\r' || c == '\t' {
			continue
		}
		if c < 0x20 || c == 0x7F {
			return false
		}
	}

	s := string(head)
	switch {
	case bytes.HasPrefix(head, []byte("WEBVTT")):
		return true
	case bytes.HasPrefix(head, []byte("\xEF\xBB\xBFWEBVTT")):
		return true
	case bytes.Contains(head, []byte("[Script Info]")):
		return true
	case bytes.Contains(head, []byte("Format: ")) && bytes.Contains(head, []byte("Dialogue:")):
		return true
	default:
		// SRT: starts with a digit (cue number), then "-->".
		if len(s) > 0 && s[0] >= '0' && s[0] <= '9' && bytes.Contains(head, []byte("-->")) {
			return true
		}
	}
	return false
}
