package imaging

import "strings"

// MaxUploadBytes is the largest multipart body accepted by the image upload
// handler. Shared here so callers (and future helpers) agree on a single value.
const MaxUploadBytes = 10 << 20 // 10 MiB

// ValidKinds is the canonical set of image kinds HubPlay stores per item.
// The same list is enforced at the DB layer (see images.type column).
var ValidKinds = [...]string{"primary", "backdrop", "logo", "thumb", "banner"}

// IsValidKind reports whether t is one of the accepted image kinds.
func IsValidKind(t string) bool {
	for _, k := range ValidKinds {
		if t == k {
			return true
		}
	}
	return false
}

// IsValidContentType reports whether ct describes an image MIME type the
// handler accepts for upload. The match is a prefix check so charset
// parameters (e.g. "image/jpeg; charset=binary") are tolerated.
//
// Accepted: image/jpeg, image/png, image/webp.
func IsValidContentType(ct string) bool {
	switch {
	case strings.HasPrefix(ct, "image/jpeg"),
		strings.HasPrefix(ct, "image/png"),
		strings.HasPrefix(ct, "image/webp"):
		return true
	}
	return false
}

// ExtensionForContentType maps a MIME type to the on-disk file extension.
// Unknown types fall back to ".jpg" to preserve historical handler behavior.
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
