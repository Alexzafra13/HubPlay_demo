package imaging

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// IngestedImage describe una imagen ya descargada y guardada en disco.
// El que la llama decide si persistirla en base de datos.
type IngestedImage struct {
	// Ruta absoluta del fichero.
	LocalPath string
	// Sólo el nombre del fichero.
	Filename string
	// Tipo MIME detectado al descargar.
	ContentType string
	// Vista previa borrosa. Vacío si el formato no es PNG ni JPEG.
	Blurhash string
	// Hash del fichero; los primeros 16 caracteres están en el nombre.
	SHA256 string
	// Colores para el degradado del hero. Vacíos si no se pudo extraer.
	DominantColor      string
	DominantColorMuted string
}

// IngestRemoteImage descarga una imagen remota y la guarda en local de
// forma segura: protege contra URLs maliciosas e imágenes "bomba", y si
// algo falla a medias no deja un fichero corrupto en disco.
func IngestRemoteImage(dir, kind, url string, logger *slog.Logger) (*IngestedImage, error) {
	data, contentType, err := SafeGet(url, MaxUploadBytes, 30*time.Second)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	if err := EnforceMaxPixels(data); err != nil {
		return nil, fmt.Errorf("validate dimensions: %w", err)
	}

	sum := sha256.Sum256(data)
	hashHex := hex.EncodeToString(sum[:])
	filename := fmt.Sprintf("%s_%s%s", kind, hashHex[:16], ExtensionForContentType(contentType))

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create dir: %w", err)
	}
	fullPath := filepath.Join(dir, filename)
	if err := AtomicWriteFile(fullPath, data, 0o644); err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}

	vibrant, muted := ExtractDominantColors(data, logger)
	return &IngestedImage{
		LocalPath:          fullPath,
		Filename:           filename,
		ContentType:        contentType,
		Blurhash:           ComputeBlurhash(data, logger),
		SHA256:             hashHex,
		DominantColor:      vibrant,
		DominantColorMuted: muted,
	}, nil
}

// AtomicWriteFile escribe primero a un temporal y luego lo renombra al
// destino, así un fallo a mitad de escritura nunca deja basura.
func AtomicWriteFile(dst string, data []byte, perm os.FileMode) error {
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
