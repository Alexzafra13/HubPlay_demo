package iptv

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hubplay/internal/imaging"
)

// LogoCache — proxy de logos de canal para evitar tres problemas
// de cargarlos directo desde hosts M3U: CSP (no podemos abrir
// img-src a https:), privacidad (IP/UA leakeados a terceros), y
// resiliencia (los hosts gratuitos desaparecen). Fetch once con
// guards SSRF, cache en disco, sin TTL.

// ErrLogoUnavailable — URL vacía, SSRF, inalcanzable o no es imagen.
// El handler lo convierte en 404 → fallback a avatar de iniciales.
var ErrLogoUnavailable = errors.New("iptv-logo: unavailable")

// logoFetchMaxBytes — los logos reales son <50 KB; 2 MiB es margen
// generoso sin permitir llenar el disco.
const logoFetchMaxBytes = 2 * 1024 * 1024

// logoFetchTimeout — si no responde en 10s mejor mostrar el fallback.
const logoFetchTimeout = 10 * time.Second

// LogoCache — goroutine-safe; escritura atómica en disco.
type LogoCache struct {
	cacheDir string
	logger   *slog.Logger
}

// NewLogoCache crea el cache. Si falla, el caller debe skip (no crash)
// para que el grid funcione sin logos proxy (fallback de iniciales).
func NewLogoCache(dir string, logger *slog.Logger) (*LogoCache, error) {
	if dir == "" {
		return nil, fmt.Errorf("iptv-logo: empty cache dir")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("iptv-logo: mkdir %s: %w", dir, err)
	}
	return &LogoCache{
		cacheDir: dir,
		logger:   logger.With("module", "iptv-logo-cache"),
	}, nil
}

// Path devuelve el path en disco del logo, descargándolo en cache miss.
// Content-addressed (sha256 de la URL), sin TTL.
func (c *LogoCache) Path(ctx context.Context, upstreamURL string) (string, error) {
	if upstreamURL == "" {
		return "", ErrLogoUnavailable
	}

	cachedPath := c.cachedPathFor(upstreamURL)
	if _, err := os.Stat(cachedPath); err == nil {
		return cachedPath, nil
	}

	// Mismas protecciones SSRF que la ingesta de imágenes VOD.
	data, _, err := imaging.SafeGet(upstreamURL, logoFetchMaxBytes, logoFetchTimeout)
	if err != nil {
		c.logger.Debug("upstream logo fetch failed",
			"url", upstreamURL, "error", err)
		return "", fmt.Errorf("%w: %v", ErrLogoUnavailable, err)
	}

	if !looksLikeImage(data) {
		c.logger.Debug("upstream logo is not an image",
			"url", upstreamURL, "first_bytes", len(data))
		return "", fmt.Errorf("%w: not an image", ErrLogoUnavailable)
	}

	if err := imaging.AtomicWriteFile(cachedPath, data, 0o644); err != nil {
		// No servimos desde RAM: la consistencia cache-handler importa más.
		return "", fmt.Errorf("iptv-logo: write cache: %w", err)
	}

	c.logger.Debug("logo cached",
		"url", upstreamURL, "path", cachedPath, "bytes", len(data))
	return cachedPath, nil
}

// cachedPathFor — hash de la URL para nombre filesystem-safe sin
// leakear credenciales en listados de directorio.
func (c *LogoCache) cachedPathFor(upstreamURL string) string {
	sum := sha256.Sum256([]byte(upstreamURL))
	// 16 bytes → probabilidad de colisión <1/2^64 para millones de logos.
	return filepath.Join(c.cacheDir, hex.EncodeToString(sum[:16]))
}

// looksLikeImage verifica via content-sniffing WHATWG que el upstream
// devolvió una imagen real, no HTML o tracking GIF.
func looksLikeImage(data []byte) bool {
	if len(data) < 12 {
		return false
	}
	ct := http.DetectContentType(data)
	return strings.HasPrefix(ct, "image/")
}
