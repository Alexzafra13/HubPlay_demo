package handlers

import (
	"crypto/sha1"
	_ "embed"
	"encoding/hex"
	"net/http"
	"strconv"
	"sync"
)

// openapiYAML is the OpenAPI 3.0.3 specification, embedded into the
// binary at build time. The authoritative source is the sibling file
// [openapi.yaml]; this constant is its frozen-at-compile-time copy.
//
// Why embed instead of serve from disk:
//
//  1. The spec ships with whatever build the operator deployed —
//     impossible for the spec to drift from the running code's wire
//     format due to a forgotten file copy or mounted volume oddity.
//  2. Single-binary deployment story (Go embed pattern HubPlay already
//     uses for the React bundle) extends to the API contract.
//  3. Hot-reload of the spec doesn't make sense — clients cache it
//     anyway, and a spec change implies a code change which implies
//     a restart.
//
//go:embed openapi.yaml
var openapiYAML []byte

// openapiETag is computed once on first request and cached. The spec
// is immutable for the life of the process, so the ETag is stable —
// clients with `If-None-Match` get a 304 without a body roundtrip.
//
// Computed lazily (rather than at init) because not every binary load
// will hit /openapi.yaml — saves ~20µs of SHA1 on cold-start.
var (
	etagOnce sync.Once
	etagVal  string
)

// OpenAPIHandler serves the embedded OpenAPI spec at
// `GET /api/v1/openapi.yaml`. No auth, no rate-limit — the spec is
// public by design (clients fetch it before they can authenticate, and
// the document itself contains no secrets).
type OpenAPIHandler struct{}

func NewOpenAPIHandler() *OpenAPIHandler { return &OpenAPIHandler{} }

// ServeYAML writes the embedded spec with appropriate caching headers.
// Content-Type is `application/yaml` per the IANA registration in
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
	// Cache for an hour at the edge — short enough that a fresh deploy
	// propagates fast, long enough that a Kotlin client polling for
	// spec updates doesn't hammer the server.
	w.Header().Set("Cache-Control", "public, max-age=3600")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(openapiYAML)
}

// openapiETag returns the cached SHA1-derived ETag for the spec bytes.
// Computed lazily on first call, then immutable for the life of the
// process.
func openapiETag() string {
	etagOnce.Do(func() {
		sum := sha1.Sum(openapiYAML)
		etagVal = `"` + hex.EncodeToString(sum[:]) + `"`
	})
	return etagVal
}

// OpenAPIBytes returns the embedded spec for tests that need to
// validate the YAML parses, etc. Internal use only — production code
// goes through ServeYAML.
func OpenAPIBytes() []byte { return openapiYAML }
