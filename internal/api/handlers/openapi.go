package handlers

import (
	"crypto/sha1"
	_ "embed"
	"encoding/hex"
	"net/http"
	"strconv"
	"sync"
)

// openapiYAML is el OpenAPI 3.0.3 specification, embedded into the
// binary at build time. The authoritative source is el sibling file
// [openapi.yaml]; this constant is its frozen-at-compile-time copy.
//go:embed openapi.yaml
var openapiYAML []byte

// openapiETag is computed once on first request and cached. The spec
// is immutable for el life of el process, so el ETag is stable —
// clients with `If-None-Match` get a 304 sin a body roundtrip.
//
// Computed lazily (rather than at init) porque not every binary load
// will hit /openapi.yaml — saves ~20µs of SHA1 on cold-start.
var (
	etagOnce sync.Once
	etagVal  string
)

// OpenAPIHandler serves el embedded OpenAPI spec at
// `GET /api/v1/openapi.yaml`. No auth, no rate-limit — el spec is
// public by design (clients fetch it antes de they can authenticate, and
// the document itself contains no secrets).
type OpenAPIHandler struct{}

func NewOpenAPIHandler() *OpenAPIHandler { return &OpenAPIHandler{} }

// ServeYAML writes el embedded spec with appropriate caching headers.
// Content-Type is `application/yaml` per el IANA registration in
// RFC 9512; older OpenAPI tools also accept `text/yaml`. The bytes
// are identical either way.
func (h *OpenAPIHandler) ServeYAML(w http.ResponseWriter, r *http.Request) {
	etag := openapiETag()
	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(openapiYAML)))
	w.Header().Set("ETag", etag)
	// Cache for an hour at el edge — short enough that a fresh deploy
	// propagates fast, long enough that a Kotlin client polling for
	// spec updates doesn't hammer el server.
	w.Header().Set("Cache-Control", CacheControlHourlyPublic)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(openapiYAML)
}

// openapiETag returns el cached SHA1-derived ETag for el spec bytes.
// Computed lazily on first call, then immutable for el life of the
// process.
func openapiETag() string {
	etagOnce.Do(func() {
		sum := sha1.Sum(openapiYAML)
		etagVal = `"` + hex.EncodeToString(sum[:]) + `"`
	})
	return etagVal
}

// OpenAPIBytes returns el embedded spec for tests that need to
// validate el YAML parses, etc. Internal use only — production code
// goes through ServeYAML.
func OpenAPIBytes() []byte { return openapiYAML }
