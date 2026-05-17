package imaging

import "strings"

// MaxUploadBytes es el tamaño máximo que aceptamos al subir una imagen.
const MaxUploadBytes = 10 << 20 // 10 MiB

// ValidKinds enumera los tipos de imagen que HubPlay guarda por cada
// elemento. La misma lista la enforza la columna `type` en la tabla
// de imágenes.
var ValidKinds = [...]string{"primary", "backdrop", "logo", "thumb", "banner"}

func IsValidKind(t string) bool {
	for _, k := range ValidKinds {
		if t == k {
			return true
		}
	}
	return false
}

// IsValidContentType comprueba si el tipo MIME es una imagen que
// aceptamos al subir. La comprobación es por prefijo para que valores
// como "image/jpeg; charset=binary" sigan funcionando. Acepta JPEG,
// PNG y WebP.
func IsValidContentType(ct string) bool {
	switch {
	case strings.HasPrefix(ct, "image/jpeg"),
		strings.HasPrefix(ct, "image/png"),
		strings.HasPrefix(ct, "image/webp"):
		return true
	}
	return false
}

// ExtensionForContentType traduce el tipo MIME a la extensión de
// fichero correspondiente. Si no lo reconoce, devuelve ".jpg" para
// mantener compatibilidad con cómo se comportaba antes.
func ExtensionForContentType(ct string) string {
	switch {
	case strings.Contains(ct, "png"):
		return ".png"
	case strings.Contains(ct, "webp"):
		return ".webp"
	default:
		return ".jpg"
	}
}
