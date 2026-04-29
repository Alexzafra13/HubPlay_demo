package imaging

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // register JPEG decoder for image.DecodeConfig
	_ "image/png"  // register PNG decoder for image.DecodeConfig
	"io"

	_ "golang.org/x/image/webp" // register WebP decoder for image.DecodeConfig
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// MaxPixels is the largest decoded image the handler will accept. Rejecting
// oversized dimensions cheaply (via image.DecodeConfig, which reads only the
// header) protects against decompression-bomb payloads that expand a few KB
// of file into multi-GB memory allocations.
//
// 40 megapixels covers any poster/backdrop/banner in practice (a 4K image is
// ~8 MP; 8K is ~33 MP) while blocking pathological 20000×20000 payloads.
const MaxPixels = 40_000_000

// ErrTooLarge is returned by EnforceMaxPixels when the image exceeds MaxPixels.
var ErrTooLarge = errors.New("imaging: image dimensions exceed maximum")

// IsSafePathSegment reports whether s is safe to use as a single filesystem
// path component joined under a trusted root. Rejects empty strings, the
// traversal sentinels "." and "..", and any string containing path
// separators. Callers MUST run this on every URL/user-supplied identifier
// before passing it to filepath.Join.
func IsSafePathSegment(s string) bool {
	if s == "" || s == "." || s == ".." {
		return false
	}
	if strings.ContainsAny(s, `/\`) {
		return false
	}
	if strings.ContainsRune(s, 0) {
		return false
	}
	return true
}

// SniffContentType reads up to the first 512 bytes of body and returns the
// DetectContentType result together with a new reader that re-serves the full
// stream (peeked bytes prepended). Callers should use the sniffed type —
// never the client-supplied Content-Type header — for validation.
//
// Errors from the underlying reader are returned as-is; io.EOF from a short
// body is treated as success (the peek is still valid).
func SniffContentType(body io.Reader) (contentType string, full io.Reader, err error) {
	head := make([]byte, 512)
	n, err := io.ReadFull(body, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", nil, fmt.Errorf("imaging: sniff read: %w", err)
	}
	head = head[:n]
	contentType = http.DetectContentType(head)
	return contentType, io.MultiReader(bytes.NewReader(head), body), nil
}

// EnforceMaxPixels decodes only the header of data (cheap) and returns an
// error if width*height exceeds MaxPixels. Unknown formats (animated GIF,
// AVIF, BMP, …) are accepted — the content-type validator and repo-level
// constraints catch them. JPEG, PNG and WebP all decode their header
// here so a 20000×20000 WebP bomb is rejected the same as a JPEG one.
func EnforceMaxPixels(data []byte) error {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		// Unsupported format — skip the check. The content-type validator
		// and repo-level constraints handle the rest.
		return nil
	}
	if int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
		return fmt.Errorf("%w: %dx%d exceeds %d pixels",
			ErrTooLarge, cfg.Width, cfg.Height, MaxPixels)
	}
	return nil
}

// ErrUnsafeURL is returned by SafeGet when the target URL resolves to an
// address that must not be fetched (loopback, link-local, private RFC1918,
// unspecified, multicast). Blocks SSRF against the host's local network.
var ErrUnsafeURL = errors.New("imaging: unsafe URL")

// SafeGet fetches rawURL with defenses against SSRF:
//
//   - Scheme must be http or https.
//   - Host is resolved and every returned address is checked; if ANY address
//     is private/loopback/link-local/unspecified/multicast, the fetch is
//     rejected before connecting.
//   - The resulting response body is capped at maxBytes.
//
// Returns the body bytes, the response Content-Type (server-supplied), and
// any error. Callers should pass the returned bytes through SniffContentType
// to re-validate — server Content-Type is hint-only.
func SafeGet(rawURL string, maxBytes int64, timeout time.Duration) ([]byte, string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, "", fmt.Errorf("%w: parse: %v", ErrUnsafeURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, "", fmt.Errorf("%w: scheme %q", ErrUnsafeURL, u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return nil, "", fmt.Errorf("%w: missing host", ErrUnsafeURL)
	}
	addrs, err := net.LookupIP(host)
	if err != nil {
		return nil, "", fmt.Errorf("resolve %s: %w", host, err)
	}
	for _, ip := range addrs {
		if BlockedIP(ip) {
			return nil, "", fmt.Errorf("%w: %s resolves to %s", ErrUnsafeURL, host, ip)
		}
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(rawURL) //nolint:gosec // target URL vetted above
	if err != nil {
		return nil, "", fmt.Errorf("get: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return nil, "", fmt.Errorf("read: %w", err)
	}
	return data, resp.Header.Get("Content-Type"), nil
}

// BlockedIP reports whether ip is in a range that clients MUST NOT reach
// from an outbound fetch: loopback (127.0.0.0/8, ::1), link-local
// (169.254/16, fe80::/10), RFC1918 private (10/8, 172.16/12, 192.168/16),
// RFC4193 unique-local (fc00::/7), unspecified (0.0.0.0, ::), and multicast.
//
// This is a variable so tests that need to hit an httptest.Server on
// 127.0.0.1 can temporarily swap it out. Production callers MUST NOT
// reassign it.
var BlockedIP = DefaultBlockedIP

// DefaultBlockedIP is the production implementation of BlockedIP. Preserved
// as an export so tests can restore it after an override.
func DefaultBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast() ||
		ip.IsPrivate() {
		return true
	}
	return false
}
