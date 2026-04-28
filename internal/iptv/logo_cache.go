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

// Why this exists
// ───────────────
// Channel logos in M3U_PLUS playlists point to arbitrary internet
// hosts (`lo1.in`, `i.imgur.com`, `i.ibb.co`, …). Three separate
// problems with letting the browser load them directly:
//
//  1. CSP. Our img-src directive is locked to first-party + tmdb +
//     fanart. Loosening it to `https:` would weaken the XSS posture
//     materially (any compromised script could exfiltrate via beacon
//     pixels). Listing every free image-host the playlist authors
//     happen to use is unmaintainable.
//
//  2. Privacy. Those hosts see the user's IP, User-Agent, and
//     timing every time the channel grid renders. A self-hosted
//     server should not leak that to dozens of third parties.
//
//  3. Resilience. Free image hosts come and go (lo1.in, ibb.co are
//     not banks). Caching once means the logo survives the upstream
//     dying.
//
// LogoCache fixes all three: the frontend asks for
// `/api/v1/iptv/channels/{id}/logo`, this layer fetches the upstream
// once with the same SSRF guards as VOD image ingestion, writes the
// bytes to disk under `<cache_dir>/iptv-logos/<sha>` and serves
// from there forever after. No TTL: if a logo changes, the operator
// triggers a refresh; channel logos are extremely stable in
// practice.

// ErrLogoUnavailable is returned when the upstream URL is empty,
// unsafe (SSRF), unreachable, or the response body isn't a real
// image. Handlers turn this into 404 so the frontend falls back to
// the existing initials/colour avatar (already wired in
// ChannelCard.tsx).
var ErrLogoUnavailable = errors.New("iptv-logo: unavailable")

// logoFetchMaxBytes caps the upstream fetch size. Real channel logos
// are tiny PNGs (<50 KB); 2 MiB is generous slack for the
// occasionally-uploaded SVG-as-PNG monstrosity, and small enough
// that a malicious provider can't fill our disk.
const logoFetchMaxBytes = 2 * 1024 * 1024

// logoFetchTimeout bounds the upstream HTTP transaction. Logo hosts
// are static-file servers; if they don't respond in 10 s they're
// effectively dead and we'd rather show the fallback initials than
// stall the channel grid.
const logoFetchTimeout = 10 * time.Second

// LogoCache fetches and serves channel logos from a same-origin URL.
// Goroutine-safe; the only mutable state is the on-disk cache, which
// we write atomically.
type LogoCache struct {
	cacheDir string
	logger   *slog.Logger
}

// NewLogoCache constructs a logo cache rooted at dir. The directory
// is created with 0o755 if missing. Returns an error if creation
// fails — the caller is expected to skip wiring the cache rather
// than crash the server, so the channel grid still works without
// proxied logos (the frontend renders the initials fallback).
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

// Path returns the on-disk path of the cached logo for upstreamURL,
// fetching it from upstream on cache miss. Returns ErrLogoUnavailable
// for any reason the caller should surface as 404 (empty URL, SSRF,
// upstream error, non-image response).
//
// Caching is content-addressed (sha256 of the URL): the same
// upstream URL across channels resolves to one file. No TTL — the
// cache only grows, but channel-logo total size for a 10k-channel
// library is on the order of single-digit MB.
func (c *LogoCache) Path(ctx context.Context, upstreamURL string) (string, error) {
	if upstreamURL == "" {
		return "", ErrLogoUnavailable
	}

	cachedPath := c.cachedPathFor(upstreamURL)
	if _, err := os.Stat(cachedPath); err == nil {
		return cachedPath, nil
	}

	// Reuses imaging.SafeGet for SSRF + size + content-type guards
	// — same protections the VOD image ingestion path enforces.
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
		// We still have the bytes in memory, but couldn't persist:
		// surface the error so the handler returns 500 (operator
		// problem — disk full, permissions). Don't fall back to
		// "serve from RAM": consistency between handler and cache
		// state matters more than a single served logo.
		return "", fmt.Errorf("iptv-logo: write cache: %w", err)
	}

	c.logger.Debug("logo cached",
		"url", upstreamURL, "path", cachedPath, "bytes", len(data))
	return cachedPath, nil
}

// cachedPathFor maps a URL to its on-disk filename. Hashing the URL
// gives a stable, filesystem-safe name and avoids leaking the URL
// (with credentials in some cases) into directory listings.
func (c *LogoCache) cachedPathFor(upstreamURL string) string {
	sum := sha256.Sum256([]byte(upstreamURL))
	// 16 bytes (32 hex chars) is enough to push collision probability
	// below 1-in-2^64 even for a million-logo cache; full sha256
	// would just make filenames longer for no operational gain.
	return filepath.Join(c.cacheDir, hex.EncodeToString(sum[:16]))
}

// looksLikeImage uses http.DetectContentType (a content-sniffing
// implementation of the WHATWG MIME-sniffing algorithm) to confirm
// the upstream returned an actual image instead of an HTML error
// page or a tracking GIF. Same defence-in-depth check the imaging
// package does for VOD posters.
func looksLikeImage(data []byte) bool {
	if len(data) < 12 {
		return false
	}
	ct := http.DetectContentType(data)
	return strings.HasPrefix(ct, "image/")
}
