package api_test

// OpenAPI ↔ router drift test.
//
// Why this exists: the OpenAPI spec at internal/api/handlers/openapi.yaml is
// the contract the Kotlin Android TV client (and any third-party consumer)
// generates code from. If a handler ships without a matching spec entry, or
// a spec entry references a handler that no longer exists, clients break in
// confusing ways at runtime. This test pins both directions at compile-test
// time:
//
//   1. Every consumer-facing route registered in NewRouter is documented in
//      the spec (or explicitly listed as out-of-scope below).
//   2. Every path declared in the spec is actually registered in the router.
//
// Implementation choice: parse router.go via go/ast rather than stand up
// the live router. Booting the real router needs ~30 services + a SQLite
// DB with migrations; an AST walk is two orders of magnitude faster and
// catches the same drift class. Trade-off: the walker only understands
// the chi idioms NewRouter uses today (Route, Group, Get/Post/Put/Patch/
// Delete/Head). New chi calls (Mount, Method) need the walker extended.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"hubplay/internal/api/handlers"
)

// route is a single (method, path) pair extracted from router.go.
type route struct {
	method string
	path   string // canonical: leading slash, no trailing slash (except "/")
}

func (r route) key() string { return r.method + " " + r.path }

// outOfScopeExact is the set of (method, path) pairs the spec intentionally
// omits from v1. They fall in three buckets:
//
//   - **Admin / mutation surface** — the Kotlin TV client never runs these.
//     Library CRUD, user management, provider config, IPTV admin operations,
//     image upload/lock, signing-key rotation.
//   - **First-run wizard** (/setup/*) — web-only, user types into a browser.
//   - **Server-to-server federation** (/peer/*, /federation/info) —
//     authenticated by Ed25519-signed peer JWTs, not user sessions.
//     Documented separately in docs/architecture/federation.md.
//
// New entry rule: if you're adding a route that the Kotlin TV / a third-party
// consumer won't touch, list it here with a one-line justification. If the
// route IS user-facing, document it in openapi.yaml instead.
var outOfScopeExact = map[string]string{
	// ── Admin auth setup (creates first user) ─────────────────────────
	"POST /auth/setup": "first-run only, web wizard",

	// ── Liveness / readiness probes (infra, not user-facing) ──────────
	// /health is documented in openapi.yaml; /health/live and
	// /health/ready are operator-side probes for Kubernetes /
	// HAProxy / Uptime Kuma — they have no user-facing contract and
	// no client SDK calls them, so they belong here rather than in
	// the public API spec.
	"GET /health/live":  "k8s/LB liveness probe",
	"GET /health/ready": "k8s/LB readiness probe",

	// ── User management (admin) ───────────────────────────────────────
	"GET /users":                      "admin user management",
	"POST /users":                     "admin user creation",
	"DELETE /users/{id}":              "admin user deletion",

	// ── Signing-key lifecycle (admin) ─────────────────────────────────
	"GET /admin/auth/keys":         "admin key rotation",
	"POST /admin/auth/keys/rotate": "admin key rotation",
	"POST /admin/auth/keys/prune":  "admin key rotation",

	// ── DB backup / restore (admin) ───────────────────────────────────
	// Operator-only file transfer endpoints (octet-stream download +
	// multipart upload). No public SDK consumes them; admin UI calls
	// them via direct fetch with credentials.
	"GET /admin/system/backup":           "admin DB snapshot download",
	"POST /admin/system/backup/restore":  "admin DB restore upload",

	// ── Logs viewer (admin) ───────────────────────────────────────────
	// In-process ring + SSE stream tailored to the admin viewer.
	// No public SDK consumer.
	"GET /admin/system/logs":         "admin log ring snapshot",
	"GET /admin/system/logs/stream":  "admin log live SSE stream",

	// ── Device-code pairing SSE (RFC 8628 §3.3.1 sibling) ─────────
	// Server-Sent Events channel a /pair page subscribes to so the
	// in-app device-link flow doesn't need polling. Authenticated
	// by the opaque device_code in the query string; native clients
	// keep using /auth/device/poll. No request body, no JSON
	// response — SSE wire format only. Documenting in openapi.yaml
	// would mean expressing event-stream semantics that the spec's
	// REST primitives can't model cleanly.
	"GET /auth/device/events": "device-code SSE channel for in-app pair flow",

	// ── Federation admin (admin) ──────────────────────────────────────
	"GET /admin/peers":                    "federation pairing admin",
	"GET /admin/peers/identity":           "federation pairing admin",
	"POST /admin/peers/probe":             "federation pairing admin",
	"POST /admin/peers/accept":            "federation pairing admin",
	"GET /admin/peers/{id}":               "federation pairing admin",
	"DELETE /admin/peers/{id}":            "federation pairing admin",
	"GET /admin/peers/invites":            "federation pairing admin",
	"POST /admin/peers/invites":           "federation pairing admin",
	"GET /admin/peers/{id}/shares":        "federation pairing admin",
	"POST /admin/peers/{id}/shares":       "federation pairing admin",
	"DELETE /admin/peers/{id}/shares/{shareID}": "federation pairing admin",

	// ── User management admin ─────────────────────────────────────────
	"POST /users/{id}/reset-password": "admin password reset",
	"PUT /users/{id}/role":            "user role admin",
	"PUT /users/{id}/active":          "user active toggle admin",
	"PUT /users/{id}/access":          "user access window admin",
	"POST /me/password":               "self password change",
	"GET /me/profiles":                "profile selector listing",
	"POST /auth/switch-profile":       "profile switch",

	// ── System / settings admin ───────────────────────────────────────
	"GET /admin/system/stats":            "admin observability",
	"GET /admin/system/stream-activity":  "admin observability",
	"GET /admin/system/top-items":        "admin observability",
	"GET /admin/system/settings":         "admin runtime config",
	"PUT /admin/system/settings":         "admin runtime config",
	"DELETE /admin/system/settings/{key}": "admin runtime config",
	"GET /admin/system/sessions":         "admin now-playing panel",
	"DELETE /admin/system/sessions/{id}": "admin now-playing panel",

	// ── Database driver management (admin) ────────────────────────────
	// Operator-only surface that swaps SQLite ↔ Postgres without
	// touching hubplay.yaml on disk. None of the user-facing SDKs
	// (Kotlin TV, federation) reach for these; the web admin panel
	// drives them directly.
	"GET /admin/system/db":           "admin DB driver/DSN management",
	"GET /admin/system/db/profiles":  "admin DB one-click profiles",
	"POST /admin/system/db/test":     "admin DB connection test",
	"PUT /admin/system/db":           "admin DB driver/DSN save",
	"POST /admin/system/db/migrate":  "admin DB data migration (sqlite→pg)",
	"POST /admin/system/restart":     "admin self-restart trigger",

	// ── Library admin ─────────────────────────────────────────────────
	"POST /libraries":              "admin library creation",
	"GET /libraries/browse":        "admin filesystem picker",
	"PUT /libraries/{id}":          "admin library update",
	"DELETE /libraries/{id}":       "admin library deletion",
	"POST /libraries/{id}/scan":    "admin library scan",

	// ── Image management (admin authoring) ────────────────────────────
	"GET /items/{id}/images":                       "image authoring (admin)",
	"GET /items/{id}/images/available":             "image authoring (admin)",
	"PUT /items/{id}/images/{type}/select":         "image authoring (admin)",
	"POST /items/{id}/images/{type}/upload":        "image authoring (admin)",
	"PUT /items/{id}/images/{imageId}/primary":     "image authoring (admin)",
	"PUT /items/{id}/images/{imageId}/lock":        "image authoring (admin)",
	"DELETE /items/{id}/images/{imageId}":          "image authoring (admin)",
	"POST /libraries/{id}/images/refresh":          "image authoring (admin)",

	// ── Provider config (admin) + provider search (admin authoring) ──
	"GET /providers":            "admin provider config",
	"PUT /providers/{name}":     "admin provider config",
	"GET /providers/search/metadata":           "admin metadata picker",
	"GET /providers/metadata/{externalId}":     "admin metadata picker",
	"GET /providers/images":                    "admin metadata picker",
	"GET /providers/search/subtitles":          "admin metadata picker",

	// ── Setup wizard (web-only) ───────────────────────────────────────
	"GET /setup/status":          "first-run wizard",
	"GET /setup/capabilities":    "first-run wizard",
	"GET /setup/browse":          "first-run wizard",
	"POST /setup/libraries":      "first-run wizard",
	"POST /setup/settings":       "first-run wizard",
	"POST /setup/complete":       "first-run wizard",
	"GET /setup/db/profiles":     "first-run wizard — DB one-click profiles",
	"POST /setup/db/test":        "first-run wizard — DB driver test",
	"POST /setup/db":             "first-run wizard — DB driver save",

	// ── Peer-to-peer federation (server-to-server) ────────────────────
	"GET /federation/info":                                      "p2p server info",
	"POST /peer/handshake":                                      "p2p pairing",
	"GET /peer/ping":                                            "p2p liveness",
	"GET /peer/libraries":                                       "p2p catalog",
	"GET /peer/libraries/{libraryID}/items":                     "p2p catalog",
	"GET /peer/search":                                          "p2p catalog search",
	"GET /peer/recent":                                          "p2p recently-added rail",
	"POST /peer/stream/{itemId}/session":                        "p2p stream session start",
	"GET /peer/stream/session/{sessionId}/master.m3u8":          "p2p HLS master",
	"GET /peer/stream/session/{sessionId}/{quality}/index.m3u8": "p2p HLS quality manifest",
	"GET /peer/stream/session/{sessionId}/{quality}/{segment}":  "p2p HLS segment",
	"GET /peer/stream/session/{sessionId}/subtitles":              "p2p subtitle list (origin side)",
	"GET /peer/stream/session/{sessionId}/subtitles/{trackIndex}": "p2p subtitle WebVTT (origin side)",
	"GET /peer/items/{itemId}/poster":                           "p2p poster bytes (origin side)",

	// ── Legacy global SSE (replaced by /me/events) ───────────────────
	"GET /events": "legacy unscoped SSE; spec only documents /me/events",

	// ── IPTV admin operations ─────────────────────────────────────────
	"POST /iptv/preflight":                              "admin M3U preflight",
	"POST /iptv/public/import":                          "admin public-source import",
	"GET /iptv/public/countries":                       "admin: pick a country for the public-source library",
	"GET /iptv/epg-catalog":                            "admin: pick an EPG catalog source",
	"POST /libraries/{id}/iptv/refresh-m3u":            "admin: force M3U refresh",
	"POST /libraries/{id}/iptv/refresh-epg":            "admin: force EPG refresh",
	"POST /libraries/{id}/epg-sources":                 "admin EPG source mgmt",
	"DELETE /libraries/{id}/epg-sources/{sourceId}":    "admin EPG source mgmt",
	"PATCH /libraries/{id}/epg-sources/reorder":        "admin EPG source mgmt",
	"POST /channels/{channelId}/reset-health":          "admin channel health",
	"POST /channels/{channelId}/disable":               "admin channel mgmt",
	"POST /channels/{channelId}/enable":                "admin channel mgmt",
	"PATCH /channels/{channelId}":                      "admin channel mgmt",
	"GET /libraries/{id}/schedule":                     "admin scheduled jobs panel",
	"PUT /libraries/{id}/schedule/{kind}":              "admin scheduled jobs",
	"DELETE /libraries/{id}/schedule/{kind}":           "admin scheduled jobs",
	"POST /libraries/{id}/schedule/{kind}/run":         "admin scheduled jobs",
	"GET /libraries/{id}/channels/unhealthy":           "admin channel health panel",
	"GET /libraries/{id}/channels/without-epg":         "admin channel health panel",
	"GET /libraries/{id}/channels/health-summary":      "admin channel health panel",
	"GET /libraries/{id}/epg-sources":                  "admin: read of EPG source list",
}

// TestOpenAPISpec_RouterCoverage walks the AST of router.go to enumerate
// every consumer-facing route and asserts each one is either documented
// in openapi.yaml or explicitly listed in outOfScopeExact above.
func TestOpenAPISpec_RouterCoverage(t *testing.T) {
	registered := registeredRoutes(t)
	specPaths := specPathSet(t)

	var missing []string
	for _, r := range registered {
		// Path-level allowlist: anything in the spec.
		if _, ok := specPaths[r.path]; ok {
			continue
		}
		// (method, path) allowlist: explicit out-of-scope.
		if _, ok := outOfScopeExact[r.key()]; ok {
			continue
		}
		missing = append(missing, r.key())
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("router routes not documented in openapi.yaml and not in out-of-scope allowlist:\n  - %s\n\n"+
			"Either add the path to internal/api/handlers/openapi.yaml under `paths:` (preferred — it's user-facing),\n"+
			"or add an entry to outOfScopeExact in this file with a one-line justification.",
			strings.Join(missing, "\n  - "))
	}
}

// TestOpenAPISpec_NoDeadPaths asserts every path in the spec is actually
// registered in the router. Catches stale documentation: a path that was
// renamed or removed in code but still lives in the YAML.
func TestOpenAPISpec_NoDeadPaths(t *testing.T) {
	registered := registeredRoutes(t)
	registeredPaths := make(map[string]struct{}, len(registered))
	for _, r := range registered {
		registeredPaths[r.path] = struct{}{}
	}

	var dead []string
	for path := range specPathSet(t) {
		if _, ok := registeredPaths[path]; !ok {
			dead = append(dead, path)
		}
	}

	if len(dead) > 0 {
		sort.Strings(dead)
		t.Fatalf("openapi.yaml documents paths that are not registered in router.go:\n  - %s\n\n"+
			"Either re-register the route or remove the stale path from the spec.",
			strings.Join(dead, "\n  - "))
	}
}

// ─── helpers ────────────────────────────────────────────────────────────

// registeredRoutes parses router.go and returns every consumer-facing
// (method, path) pair under /api/v1 (the prefix is stripped). The SPA
// fallback at /* and the metrics endpoint outside /api/v1 are excluded.
func registeredRoutes(t *testing.T) []route {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	routerPath := filepath.Join(filepath.Dir(thisFile), "router.go")

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, routerPath, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse router.go: %v", err)
	}

	var newRouter *ast.FuncDecl
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fn.Name.Name == "NewRouter" {
			newRouter = fn
			break
		}
	}
	if newRouter == nil {
		t.Fatal("NewRouter func not found in router.go")
	}

	var all []route
	walkRouterBlock(newRouter.Body, "", &all)

	// Keep only consumer-facing paths under /api/v1; strip the prefix.
	const prefix = "/api/v1"
	out := make([]route, 0, len(all))
	for _, r := range all {
		if r.path == "/*" {
			continue // SPA fallback
		}
		if !strings.HasPrefix(r.path, prefix) {
			continue
		}
		stripped := strings.TrimPrefix(r.path, prefix)
		if stripped == "" {
			stripped = "/"
		}
		out = append(out, route{method: r.method, path: canonPath(stripped)})
	}
	return out
}

// walkRouterBlock walks a chi router function body, accumulating routes
// with their effective path prefix.
func walkRouterBlock(body *ast.BlockStmt, prefix string, out *[]route) {
	if body == nil {
		return
	}
	for _, stmt := range body.List {
		walkStmt(stmt, prefix, out)
	}
}

func walkStmt(stmt ast.Stmt, prefix string, out *[]route) {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		walkCall(s.X, prefix, out)
	case *ast.IfStmt:
		// Conditional registration like `if deps.X != nil { ... }`.
		// For drift purposes we want to see every code path's routes.
		walkRouterBlock(s.Body, prefix, out)
		if s.Else != nil {
			switch e := s.Else.(type) {
			case *ast.BlockStmt:
				walkRouterBlock(e, prefix, out)
			case *ast.IfStmt:
				walkStmt(e, prefix, out)
			}
		}
	case *ast.BlockStmt:
		walkRouterBlock(s, prefix, out)
	case *ast.AssignStmt, *ast.DeclStmt:
		// Locals that don't register routes; ignore.
	}
}

func walkCall(expr ast.Expr, prefix string, out *[]route) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	switch sel.Sel.Name {
	case "Route":
		// r.Route("/prefix", func(r chi.Router) { ... })
		if len(call.Args) >= 2 {
			subPrefix := stringLit(call.Args[0])
			if fn, ok := call.Args[1].(*ast.FuncLit); ok {
				walkRouterBlock(fn.Body, prefix+subPrefix, out)
			}
		}
	case "Group":
		// r.Group(func(r chi.Router) { ... }) — same prefix, just middleware.
		if len(call.Args) >= 1 {
			if fn, ok := call.Args[0].(*ast.FuncLit); ok {
				walkRouterBlock(fn.Body, prefix, out)
			}
		}
	case "Get", "Post", "Put", "Patch", "Delete", "Head", "Options":
		if len(call.Args) >= 1 {
			path := stringLit(call.Args[0])
			*out = append(*out, route{
				method: strings.ToUpper(sel.Sel.Name),
				path:   prefix + path,
			})
		}
	case "Mount":
		// r.Mount("/prefix", subHandler) — not used at the moment.
		// If introduced, we'd need to descend into the sub-router.
	}
}

func stringLit(e ast.Expr) string {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return ""
	}
	v, err := strconv.Unquote(bl.Value)
	if err != nil {
		return ""
	}
	return v
}

// canonPath normalises a chi-flavoured path to OpenAPI's convention:
// no trailing slash unless the path is "/".
func canonPath(p string) string {
	if p == "/" {
		return p
	}
	return strings.TrimRight(p, "/")
}

// specPathSet parses the embedded openapi.yaml and returns the set of
// declared paths. The keys are normalised the same way canonPath does.
func specPathSet(t *testing.T) map[string]struct{} {
	t.Helper()
	var doc struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(handlers.OpenAPIBytes(), &doc); err != nil {
		t.Fatalf("parse openapi.yaml: %v", err)
	}
	out := make(map[string]struct{}, len(doc.Paths))
	for p := range doc.Paths {
		out[canonPath(p)] = struct{}{}
	}
	return out
}
