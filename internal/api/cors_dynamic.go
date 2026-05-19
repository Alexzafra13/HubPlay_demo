package api

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/go-chi/cors"

	"hubplay/internal/config"
)

// ─── CorsRegistry ────────────────────────────────────────────────────
//
// CorsRegistry mantiene la lista de orígenes permitidos (statics del
// YAML + dynamics del DB) en una snapshot atómica.  El middleware
// resultante combina ambas listas para validar el Origin de cada
// preflight, y un Reload() recarga los dynamics sin tener que tocar
// el handler chi/cors.
//
// Razón de ser un middleware custom en vez de reconstruir el cors
// handler de chi: el handler de chi/cors captura los orígenes al
// construirse y NO los re-lee.  Reload sin restart obligaría a
// reconstruir el router entero (caro y peligroso).  Aquí ponemos un
// wrapper delgado que sólo intercepta el match de Origin; el resto
// del comportamiento CORS (headers, métodos, max-age) lo deja al
// handler de chi/cors via su AllowOriginFunc.
//
// Lectura concurrente, escritura ocasional → atomic.Pointer es el
// patrón correcto: lectores nunca bloquean, escritor (Reload) hace
// un Store que es visible inmediatamente.

type CorsRegistry struct {
	// statics son los orígenes del YAML — no cambian en runtime.
	// Mantienen su origen separado para que el panel admin pueda
	// pintarlos con candado / read-only.
	statics []string

	// dynamics se actualiza atómicamente cada vez que se inserta o
	// borra un origen en la DB.
	dynamics atomic.Pointer[[]string]
}

// NewCorsRegistry inicializa el registry con los statics dados (los
// dynamics empiezan vacíos; el caller llama a SetDynamics tras un
// fetch inicial del repo).
func NewCorsRegistry(staticOrigins []string) *CorsRegistry {
	r := &CorsRegistry{
		statics: slices.Clone(staticOrigins),
	}
	empty := []string{}
	r.dynamics.Store(&empty)
	return r
}

// AllowedOrigins es el helper EXPORTADO que main.go usa al construir
// el registry — mismo set de statics que ya tenía el handler
// pre-PR4. Wrapper sobre el `allowedOrigins` privado para mantener
// una única fuente de verdad.
func AllowedOrigins(cfg *config.Config) []string {
	return allowedOrigins(cfg)
}

// SetDynamics reemplaza la lista de orígenes dinámicos. Atómico —
// los preflights en curso o las requests inmediatamente posteriores
// ven el nuevo snapshot.
func (r *CorsRegistry) SetDynamics(origins []string) {
	cloned := slices.Clone(origins)
	r.dynamics.Store(&cloned)
}

// Statics devuelve una copia de los statics del YAML (para que el
// panel admin pueda mostrarlos sin riesgo de mutación).
func (r *CorsRegistry) Statics() []string {
	return slices.Clone(r.statics)
}

// IsAllowed comprueba si origin está en cualquiera de las dos listas.
// origin viene tal cual del header HTTP Origin del navegador.
func (r *CorsRegistry) IsAllowed(origin string) bool {
	if origin == "" {
		return false
	}
	for _, o := range r.statics {
		if o == origin {
			return true
		}
	}
	if d := r.dynamics.Load(); d != nil {
		for _, o := range *d {
			if o == origin {
				return true
			}
		}
	}
	return false
}

// ─── Middleware ─────────────────────────────────────────────────────

// CorsMiddleware devuelve un middleware http compatible con chi que
// envuelve el handler de chi/cors usando AllowOriginFunc para que el
// match consulte el registry en cada request. AllowedMethods,
// AllowedHeaders, etc. siguen siendo estáticos (no tiene sentido
// gestionarlos por panel — son las APIs que SIEMPRE necesitamos).
func CorsMiddleware(registry *CorsRegistry, methods, headers, exposed []string, allowCredentials bool, maxAge int) func(http.Handler) http.Handler {
	return cors.Handler(cors.Options{
		AllowOriginFunc: func(r *http.Request, origin string) bool {
			return registry.IsAllowed(origin)
		},
		AllowedMethods:   methods,
		AllowedHeaders:   headers,
		ExposedHeaders:   exposed,
		AllowCredentials: allowCredentials,
		MaxAge:           maxAge,
	})
}

// ─── Validación del input del panel ─────────────────────────────────

// ValidateCorsOrigin parsea y valida que `raw` sea aceptable como
// origin permitido. Reglas:
//   - scheme DEBE ser http o https.
//   - host no vacío.
//   - sin path, sin query, sin fragment.
//   - sin trailing slash.
//   - sin wildcards (*).
//   - no las cadenas "null", "file:", "javascript:", "data:".
//
// Devuelve la forma canónica (lowercased scheme + host) o un error
// descriptivo. El handler usa el error literal en el response body.
//
// Razón de canonicalizar: dos orígenes que difieren sólo en
// mayúsculas/minúsculas del scheme/host son el mismo origen para el
// navegador (RFC 6454), pero comparar string literal NO los iguala.
// Lowercased asegura matching consistente.
func ValidateCorsOrigin(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errCorsOriginEmpty
	}
	if strings.Contains(raw, "*") {
		return "", errCorsOriginWildcard
	}
	if raw == "null" {
		return "", errCorsOriginNullLiteral
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", errCorsOriginInvalid
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", errCorsOriginBadScheme
	}
	if u.Host == "" {
		return "", errCorsOriginNoHost
	}
	if u.Path != "" && u.Path != "/" {
		return "", errCorsOriginHasPath
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", errCorsOriginHasExtras
	}
	host := strings.ToLower(u.Host)

	// Reconstruimos la forma canónica para asegurar que el output no
	// incluye un trailing slash incluso si la entrada lo tenía.
	return scheme + "://" + host, nil
}

// Errores como variables para que los tests puedan errors.Is contra
// ellos y el handler pueda traducir cada uno a un mensaje localizado.
var (
	errCorsOriginEmpty       = newCorsErr("origin is empty")
	errCorsOriginWildcard    = newCorsErr("wildcards (*) are not allowed")
	errCorsOriginNullLiteral = newCorsErr("the literal \"null\" is not a valid origin")
	errCorsOriginInvalid     = newCorsErr("origin is not a valid URL")
	errCorsOriginBadScheme   = newCorsErr("scheme must be http or https")
	errCorsOriginNoHost      = newCorsErr("origin must include a host")
	errCorsOriginHasPath     = newCorsErr("origin must not include a path")
	errCorsOriginHasExtras   = newCorsErr("origin must not include query string or fragment")
)

// corsErr — tipo wrapper con .Error() en castellano operativo para
// que el handler lo devuelva tal cual sin perder localización futura.
type corsErr struct{ msg string }

func newCorsErr(s string) error      { return &corsErr{msg: s} }
func (e *corsErr) Error() string     { return e.msg }
