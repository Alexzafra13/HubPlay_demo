package iptv

// Preflight check for an M3U URL — quick reachability + format probe
// so the admin gets immediate feedback when adding / editing an IPTV
// library, instead of clicking "Save" and watching a silent spinner
// for up to 5 minutes.
//
// Why this exists: the most common modes of "my list doesn't load"
// (provider hung, account suspended, cert expired, IP blocked, list
// returns HTML error page) all fail in the first 1–15s. Bounding the
// probe to 12s and classifying the failure mode means we can show
// "the provider is not responding — wait or contact them" instead
// of "Internal Server Error" three minutes later.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// PreflightStatus is the verdict of one preflight probe. Stable
// strings — the frontend dispatches UI on these values.
type PreflightStatus string

const (
	PreflightOK         PreflightStatus = "ok"          // 200 + body looks like M3U
	PreflightSlow       PreflightStatus = "slow"        // TCP up but no response in budget — common on big providers
	PreflightEmpty      PreflightStatus = "empty"       // 200 OK, empty body
	PreflightHTML       PreflightStatus = "html"        // got HTML (account suspended, IP block, captive portal)
	PreflightAuth       PreflightStatus = "auth"        // 401 / 403
	PreflightNotFound   PreflightStatus = "not_found"   // 404
	PreflightTLS        PreflightStatus = "tls"         // certificate / TLS handshake error
	PreflightDNS        PreflightStatus = "dns"         // host not resolvable
	PreflightConnect    PreflightStatus = "connect"     // TCP refused
	PreflightInvalidURL PreflightStatus = "invalid_url" // URL parse error / wrong scheme
	PreflightUnknown    PreflightStatus = "unknown"     // catch-all so the UI never gets a missing field
)

// PreflightResult is the wire shape returned to the API.
type PreflightResult struct {
	Status        PreflightStatus `json:"status"`
	HTTPStatus    int             `json:"http_status,omitempty"`
	ContentLength int64           `json:"content_length,omitempty"`
	BodyHint      string          `json:"body_hint,omitempty"` // first non-blank line, truncated
	ElapsedMS     int64           `json:"elapsed_ms"`
	Message       string          `json:"message"` // human-readable, ready for the UI
}

const (
	// preflightBudget is the wall-clock cap on a single probe. Long
	// enough to absorb DNS + TLS handshake on a slow link, short
	// enough that "no response yet" → "slow" rather than "perpetual
	// spinner". The 5-min M3U fetch timeout in fetchURL stays as the
	// real import budget; this is just the early-warning probe.
	preflightBudget = 12 * time.Second
	// preflightBodyPeek caps how many body bytes we read for sniffing.
	// Enough to capture #EXTM3U + the first #EXTINF line.
	preflightBodyPeek = 4096
)

// PreflightCheck probes the given URL with a tight time budget and
// returns a structured verdict. It honours `tlsInsecure` exactly the
// same way fetchURL does — the toggle lets the admin verify the
// "Skip TLS" workaround works before saving.
func (s *Service) PreflightCheck(ctx context.Context, m3uURL string, tlsInsecure bool) PreflightResult {
	start := time.Now()
	elapsed := func() int64 { return time.Since(start).Milliseconds() }

	parsed, err := url.Parse(m3uURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return PreflightResult{
			Status:    PreflightInvalidURL,
			Message:   "URL inválida; debe empezar por http:// o https://.",
			ElapsedMS: elapsed(),
		}
	}

	fetchCtx, cancel := context.WithTimeout(ctx, preflightBudget)
	defer cancel()

	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, m3uURL, nil)
	if err != nil {
		return PreflightResult{
			Status:    PreflightInvalidURL,
			Message:   fmt.Sprintf("No se pudo crear la petición: %v", err),
			ElapsedMS: elapsed(),
		}
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "*/*")

	client := s.httpClient
	if tlsInsecure {
		client = s.insecureFetchClient()
	}

	resp, err := client.Do(req)
	if err != nil {
		return classifyPreflightError(err, time.Since(start))
	}
	defer resp.Body.Close() //nolint:errcheck

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return PreflightResult{
			Status:     PreflightAuth,
			HTTPStatus: resp.StatusCode,
			Message:    fmt.Sprintf("El provider rechazó las credenciales (HTTP %d).", resp.StatusCode),
			ElapsedMS:  elapsed(),
		}
	case resp.StatusCode == http.StatusNotFound:
		return PreflightResult{
			Status:     PreflightNotFound,
			HTTPStatus: resp.StatusCode,
			Message:    "El provider devolvió 404. Verifica la URL.",
			ElapsedMS:  elapsed(),
		}
	case resp.StatusCode != http.StatusOK:
		return PreflightResult{
			Status:     PreflightUnknown,
			HTTPStatus: resp.StatusCode,
			Message:    fmt.Sprintf("HTTP %d inesperado.", resp.StatusCode),
			ElapsedMS:  elapsed(),
		}
	}

	// Read up to N bytes for format sniffing. Tolerate short reads
	// and even Content-Length lies (some providers advertise huge
	// sizes then close the socket early): if we got ANY bytes the
	// shape classifier downstream is enough to render a verdict.
	// Only when peek is empty AND the error isn't a clean EOF do we
	// bubble it as a transport failure.
	var peek bytes.Buffer
	_, copyErr := io.CopyN(&peek, resp.Body, int64(preflightBodyPeek))
	if peek.Len() == 0 && copyErr != nil && !errors.Is(copyErr, io.EOF) {
		return classifyPreflightError(copyErr, time.Since(start))
	}
	body := peek.Bytes()
	if len(body) == 0 {
		return PreflightResult{
			Status:     PreflightEmpty,
			HTTPStatus: resp.StatusCode,
			Message:    "El provider devolvió 200 OK pero sin contenido. La cuenta puede no tener canales asignados.",
			ElapsedMS:  elapsed(),
		}
	}
	first := firstNonBlankLine(body)

	// HTML response: surface the typical Spanish IPTV-blocking
	// causes inline so the admin doesn't have to guess.
	if strings.HasPrefix(first, "<") {
		return PreflightResult{
			Status:        PreflightHTML,
			HTTPStatus:    resp.StatusCode,
			ContentLength: resp.ContentLength,
			BodyHint:      truncatePreflight(first, 200),
			Message: "El provider devolvió HTML en lugar de un playlist. Causas habituales: " +
				"cuenta suspendida, IP bloqueada (orden judicial LaLiga / Movistar), " +
				"credenciales inválidas, captive portal.",
			ElapsedMS: elapsed(),
		}
	}

	// Recognised playlist shapes: standard #EXTM3U header, a stray
	// #EXTINF (some providers skip the header), or a URL line (some
	// degenerate exports drop all metadata — our parser will still
	// import these, just without channel names).
	lower := strings.ToLower(first)
	switch {
	case strings.HasPrefix(first, "#EXTM3U"),
		strings.HasPrefix(first, "#EXTINF"),
		strings.HasPrefix(lower, "http://"),
		strings.HasPrefix(lower, "https://"):
		msg := "M3U válido detectado."
		if resp.ContentLength > 100*1024*1024 {
			msg += fmt.Sprintf(" Tamaño anunciado: %d MB — la descarga puede tardar 1-3 min.",
				resp.ContentLength/1024/1024)
		}
		return PreflightResult{
			Status:        PreflightOK,
			HTTPStatus:    resp.StatusCode,
			ContentLength: resp.ContentLength,
			BodyHint:      truncatePreflight(first, 200),
			Message:       msg,
			ElapsedMS:     elapsed(),
		}
	}

	return PreflightResult{
		Status:        PreflightUnknown,
		HTTPStatus:    resp.StatusCode,
		ContentLength: resp.ContentLength,
		BodyHint:      truncatePreflight(first, 200),
		Message:       "200 OK pero el contenido no parece un playlist M3U.",
		ElapsedMS:     elapsed(),
	}
}

// classifyPreflightError maps low-level transport errors to the
// PreflightStatus the UI knows how to render. Messages are written
// for the operator, not the developer — they explain the likely
// cause and what to try next.
func classifyPreflightError(err error, elapsed time.Duration) PreflightResult {
	msg := err.Error()
	lower := strings.ToLower(msg)
	elapsedMS := elapsed.Milliseconds()

	switch {
	case strings.Contains(msg, "x509") ||
		strings.Contains(msg, "certificate") ||
		strings.Contains(msg, "tls:"):
		return PreflightResult{
			Status:    PreflightTLS,
			Message:   "Error TLS: el certificado del provider no es válido (caducado, auto-firmado, o hostname no coincide). Activa \"Saltar verificación TLS\" si confías en el provider.",
			ElapsedMS: elapsedMS,
		}
	case strings.Contains(lower, "no such host"),
		strings.Contains(lower, "dns"):
		return PreflightResult{
			Status:    PreflightDNS,
			Message:   "DNS no resuelve el host. Verifica la URL o si tu ISP está bloqueando el dominio.",
			ElapsedMS: elapsedMS,
		}
	case strings.Contains(lower, "connection refused"),
		strings.Contains(lower, "refused"):
		return PreflightResult{
			Status:    PreflightConnect,
			Message:   "El servidor rechaza la conexión. Servicio caído o puerto incorrecto.",
			ElapsedMS: elapsedMS,
		}
	case errors.Is(err, context.DeadlineExceeded),
		strings.Contains(lower, "timeout"),
		strings.Contains(lower, "deadline"):
		return PreflightResult{
			Status: PreflightSlow,
			Message: fmt.Sprintf(
				"El servidor conectó pero no respondió en %ds. Esto es típico de providers con listas grandes (60-90s para generar). "+
					"Puedes guardar y esperar — el import tiene timeout de 5 min.",
				int(elapsed.Seconds())),
			ElapsedMS: elapsedMS,
		}
	default:
		return PreflightResult{
			Status:    PreflightUnknown,
			Message:   fmt.Sprintf("Error inesperado: %v", err),
			ElapsedMS: elapsedMS,
		}
	}
}

// firstNonBlankLine returns the first stripped line of body that
// isn't whitespace. Used to sniff the playlist's first signal byte.
func firstNonBlankLine(body []byte) string {
	for _, line := range strings.Split(string(body), "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}

// truncatePreflight caps a string at n runes with an ellipsis so the
// JSON response stays small even when the provider returns a 50 KB
// HTML error page on the first line.
func truncatePreflight(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
