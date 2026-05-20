// Package updates implementa el checker que sondea GitHub Releases
// periódicamente para detectar nuevas versiones de HubPlay.
//
// Diseño:
//
//   - Una goroutine en background corre un ticker cada 24h. La primera
//     comprobación se hace tras un jitter aleatorio (0-30 min) — si 1000
//     instalaciones se reinician a la vez (apagón general, cluster de
//     contenedores) NO golpean la API de GitHub al unísono.
//
//   - Cada comprobación es una llamada HTTP a la GitHub API. La response
//     trae cabecera ETag; la guardamos y la enviamos en la siguiente como
//     If-None-Match. Si no hay versión nueva, GitHub responde 304 con
//     cuerpo vacío — gastamos ~200 bytes en lugar de los ~5 KB del JSON
//     completo. Buena ciudadanía con su rate-limit (60 req/h anónimo).
//
//   - El estado vive en memoria, protegido por RWMutex. Los handlers
//     leen sin esperar — el checker NUNCA bloquea las peticiones HTTP.
//
//   - El servicio es opcional. Si NewService recibe repo=="" lo deja
//     desactivado: Start() es no-op, Status() devuelve check_enabled=false.
//     Útil para deshabilitar updates en builds privados/forks.
//
//   - El comparador semver es básico pero suficiente para nuestro tagging
//     vX.Y.Z[-prerelease]. Por defecto IGNORAMOS prereleases (alphas,
//     betas, rc, nightly) — sólo notifica releases estables. Setting
//     opt-in para incluir prereleases queda para una iteración futura.
//
// Privacy: la única request saliente va a api.github.com con un
// User-Agent que identifica HubPlay + versión. NO mandamos UUID de
// instalación, ni IP, ni nada que identifique al operador. Si el
// operador no quiere ni eso, puede deshabilitar el servicio en el
// config (UpdateCheck.Enabled = false) — Start() respeta el flag.
package updates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CheckInterval es cada cuánto el ticker dispara una comprobación.
// 24h es el sweet spot: suficiente para que un operador vea avisos
// del día siguiente sin sentir que la app está "espiando" GitHub
// constantemente. Lo hicimos var en lugar de const para que los
// tests puedan reducirlo a 100ms sin necesidad de exponer setters.
var CheckInterval = 24 * time.Hour

// MaxInitialJitter es el rango aleatorio del primer check. 30 min
// reparte la carga sobre la API de GitHub si muchas instalaciones
// arrancan a la vez (e.g. tras apagón general).
var MaxInitialJitter = 30 * time.Minute

// requestTimeout es el cap a una sola llamada HTTP. Si GitHub está
// lento, no queremos que la goroutine quede colgada.
const requestTimeout = 15 * time.Second

// defaultBaseURL es el endpoint canónico de la GitHub Releases API.
// Lo extraemos a const para que SetBaseURL pueda apuntar a un
// httptest.Server en tests E2E sin tener que mockear el http.Client.
const defaultBaseURL = "https://api.github.com"

// Status es el snapshot público del checker. Lo serializa el handler
// a JSON tal cual; los nombres de campo van con tags json explícitos
// para evitar drift accidental al renombrar.
type Status struct {
	// Current es la versión actualmente en ejecución (inyectada por
	// ldflags al binario). Si es "dev", el checker no compara y deja
	// HasUpdate=false — un build de desarrollo no se "actualiza" a un
	// release oficial.
	Current string `json:"current"`

	// Latest es la última versión estable conocida en GitHub Releases.
	// Vacía si aún no se ha hecho ningún check.
	Latest string `json:"latest"`

	// HasUpdate es true si Current < Latest según comparación semver.
	HasUpdate bool `json:"has_update"`

	// ReleaseURL apunta a la página del release en GitHub.
	ReleaseURL string `json:"release_url,omitempty"`

	// ReleaseNotes son las release notes en markdown crudas.  El
	// frontend puede renderizarlas o linkar al release.
	ReleaseNotes string `json:"release_notes,omitempty"`

	// PublishedAt es cuando el release fue publicado.
	PublishedAt time.Time `json:"published_at,omitempty"`

	// LastChecked es cuando hicimos la última comprobación exitosa.
	// Zero si nunca se ha hecho un check exitoso.
	LastChecked time.Time `json:"last_checked,omitempty"`

	// LastError es el mensaje del último fallo (vacío si no hay error
	// reciente). Útil para diagnosticar — no debería romper la UI.
	LastError string `json:"last_error,omitempty"`

	// CheckEnabled es false si el checker está deshabilitado
	// (Start nunca arrancó por config o repo vacío).
	CheckEnabled bool `json:"check_enabled"`
}

// Service es el checker. Construirlo con New, arrancarlo con Start.
type Service struct {
	current string
	repo    string
	client  *http.Client
	logger  *slog.Logger

	mu      sync.RWMutex
	baseURL string // raíz de la GitHub API (apuntable a httptest en tests)
	state   Status
	etag    string // último ETag de la API, para If-None-Match
}

// New construye el checker. currentVersion es la versión inyectada al
// binario (e.g. "v0.1.0", "dev"). repo es el slug "owner/name" de
// GitHub (e.g. "Alexzafra13/HubPlay_demo"). Si repo=="" el servicio
// queda deshabilitado.
func New(currentVersion, repo string, logger *slog.Logger) *Service {
	s := &Service{
		current: currentVersion,
		repo:    repo,
		client: &http.Client{
			Timeout: requestTimeout,
		},
		logger:  logger.With("module", "updates"),
		baseURL: defaultBaseURL,
	}
	s.state = Status{
		Current:      currentVersion,
		CheckEnabled: repo != "" && currentVersion != "dev",
	}
	return s
}

// SetBaseURL apunta el checker a un endpoint distinto de
// api.github.com. Pensado para tests E2E que levantan un httptest
// server con el shape de /repos/<owner>/<repo>/releases/latest;
// llamar a SetBaseURL("") restaura el default. Seguro concurrentemente.
//
// NO está pensado para apuntar a proxies de GitHub en producción
// (entre otras cosas porque el User-Agent y los rate-limits de la
// real API son los que asumen los tests del comparador semver).
func (s *Service) SetBaseURL(u string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u == "" {
		s.baseURL = defaultBaseURL
		return
	}
	s.baseURL = strings.TrimRight(u, "/")
}

// Start arranca la goroutine del checker en background. El context se
// usa para parar la goroutine cuando el server hace shutdown.  Llamar
// a Start más de una vez es seguro: las llamadas posteriores son no-op.
// Si el servicio está deshabilitado (repo vacío o version=dev), Start
// no hace nada.
func (s *Service) Start(ctx context.Context) {
	if !s.state.CheckEnabled {
		s.logger.Info("update checker disabled",
			"reason", "no repo configured or dev build")
		return
	}
	// Capturar el jitter ANTES de spawnear la goroutine. Si lo
	// leyéramos dentro de run() vs un test que sobrescribe
	// MaxInitialJitter en defer habría data race (-race lo cazaba).
	jitter := time.Duration(rand.Int64N(int64(MaxInitialJitter)))
	interval := CheckInterval
	go s.run(ctx, jitter, interval)
}

func (s *Service) run(ctx context.Context, jitter, interval time.Duration) {
	s.logger.Info("update checker started",
		"interval", interval,
		"first_check_in", jitter)

	select {
	case <-time.After(jitter):
	case <-ctx.Done():
		return
	}

	// Primer check inmediato tras el jitter.
	if err := s.Check(ctx); err != nil {
		s.logger.Warn("initial update check failed", "error", err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.logger.Info("update checker stopped")
			return
		case <-ticker.C:
			if err := s.Check(ctx); err != nil {
				s.logger.Warn("scheduled update check failed", "error", err)
			}
		}
	}
}

// Check fuerza una comprobación inmediata. Devuelve error si la
// llamada HTTP falla. Actualiza el estado interno antes de retornar.
// Seguro llamar concurrentemente: serializado por la implementación.
func (s *Service) Check(ctx context.Context) error {
	if !s.state.CheckEnabled {
		return errors.New("update check disabled")
	}

	s.mu.RLock()
	url := fmt.Sprintf("%s/repos/%s/releases/latest", s.baseURL, s.repo)
	etag := s.etag
	s.mu.RUnlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "HubPlay/"+s.current+" (update-check)")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		s.recordError(fmt.Sprintf("github request: %v", err))
		return err
	}
	defer resp.Body.Close()

	// 304 = no cambios. Mantener cache, sólo actualizar LastChecked.
	if resp.StatusCode == http.StatusNotModified {
		s.mu.Lock()
		s.state.LastChecked = time.Now().UTC()
		s.state.LastError = ""
		s.mu.Unlock()
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		// 403 = probable rate-limit (60/h anónimo). Lo logueamos pero
		// no rompemos — siguiente tick lo reintentará.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		msg := fmt.Sprintf("github responded %d: %s", resp.StatusCode, string(body))
		s.recordError(msg)
		return errors.New(msg)
	}

	var payload struct {
		TagName     string    `json:"tag_name"`
		Name        string    `json:"name"`
		Body        string    `json:"body"`
		HTMLURL     string    `json:"html_url"`
		Prerelease  bool      `json:"prerelease"`
		PublishedAt time.Time `json:"published_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		s.recordError(fmt.Sprintf("decode: %v", err))
		return err
	}

	// IMPORTANTE: la API "/releases/latest" devuelve el último release
	// ESTABLE (no prereleases ni drafts) — GitHub ya filtra por nosotros.
	// El campo Prerelease es defensa adicional por si el repo cambiara
	// la convención. Tampoco notificamos del tag "nightly" (es
	// prerelease) — sólo releases tageados v*.
	if payload.Prerelease {
		// No actualizar Latest pero sí LastChecked.
		s.mu.Lock()
		s.state.LastChecked = time.Now().UTC()
		s.state.LastError = ""
		s.etag = resp.Header.Get("ETag")
		s.mu.Unlock()
		return nil
	}

	newer := isNewer(payload.TagName, s.current)

	s.mu.Lock()
	s.state.Latest = payload.TagName
	s.state.HasUpdate = newer
	s.state.ReleaseURL = payload.HTMLURL
	s.state.ReleaseNotes = payload.Body
	s.state.PublishedAt = payload.PublishedAt
	s.state.LastChecked = time.Now().UTC()
	s.state.LastError = ""
	s.etag = resp.Header.Get("ETag")
	s.mu.Unlock()

	if newer {
		s.logger.Info("update available",
			"current", s.current,
			"latest", payload.TagName,
			"published", payload.PublishedAt)
	}
	return nil
}

// Status devuelve un snapshot del estado actual. Seguro de llamar
// concurrentemente; el caller recibe una copia, no la referencia
// interna.
func (s *Service) Status() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

func (s *Service) recordError(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.LastError = msg
}

// ─── semver comparator ───────────────────────────────────────────────

// isNewer devuelve true si remote > local en semver. Reglas:
//
//   - Prefijo "v" opcional, ambos lados.
//   - Compara major.minor.patch numéricamente (no como string para no
//     confundir "10" < "9").
//   - Prereleases (e.g. "v1.0.0-alpha.1") se consideran MENORES que
//     la versión estable equivalente. Una alpha de v1.0.0 < v1.0.0.
//   - Si local es "dev" o vacío, devuelve false — un build de desarrollo
//     no se actualiza a un release oficial (los devs ya saben qué corren).
//
// Es deliberadamente simple. Para casos exóticos (build metadata,
// rangos de versiones, etc.) usaríamos golang.org/x/mod/semver, pero
// para tags de la forma vX.Y.Z[-prerelease] esto basta.
func isNewer(remote, local string) bool {
	if local == "dev" || local == "" {
		return false
	}
	rMaj, rMin, rPatch, rPre := parseSemver(remote)
	lMaj, lMin, lPatch, lPre := parseSemver(local)

	if rMaj != lMaj {
		return rMaj > lMaj
	}
	if rMin != lMin {
		return rMin > lMin
	}
	if rPatch != lPatch {
		return rPatch > lPatch
	}
	// Mismas tres cifras: si remote es estable y local es prerelease,
	// remote es newer (v1.0.0 > v1.0.0-alpha.1).
	if rPre == "" && lPre != "" {
		return true
	}
	return false
}

// parseSemver descompone "v1.2.3-alpha.4" en (1, 2, 3, "alpha.4").
// Si una cifra falla al parsear, queda en 0 — degrada elegante en
// vez de panicar. La parte prerelease incluye TODO lo que sigue al
// guión (incluyendo el guión inicial no, eso lo separamos).
func parseSemver(v string) (major, minor, patch int, prerelease string) {
	v = strings.TrimPrefix(v, "v")
	// Separar prerelease/build (lo que sigue al primer "-" o "+").
	// build metadata después de "+" lo ignoramos.
	main := v
	if idx := strings.IndexAny(v, "-+"); idx >= 0 {
		main = v[:idx]
		if v[idx] == '-' {
			rest := v[idx+1:]
			// Cortar el "+" del build metadata si lo hay.
			if p := strings.IndexByte(rest, '+'); p >= 0 {
				rest = rest[:p]
			}
			prerelease = rest
		}
	}
	parts := strings.Split(main, ".")
	if len(parts) > 0 {
		major, _ = strconv.Atoi(parts[0])
	}
	if len(parts) > 1 {
		minor, _ = strconv.Atoi(parts[1])
	}
	if len(parts) > 2 {
		patch, _ = strconv.Atoi(parts[2])
	}
	return
}
