package libhandler

// Tests de integración del LibraryHandler contra una DB real (SQLite
// file-per-test, o Postgres bajo HUBPLAY_TEST_DRIVER=postgres). A
// diferencia de library_test.go — que usa un libFakeService para fijar
// el comportamiento del handler aisladamente — aquí cableamos el
// library.Service real sobre repos sqlc reales. Esto ejercita el camino
// completo Handler → Service → Repository → DB y cubre lo que los fakes
// no pueden: traducción de ItemFilter a SQL, paginación keyset, el cap
// por content-rating (cláusula IN materializada), el ACL de ListForUser
// (INNER JOIN library_access) y el round-trip real de persistencia.
//
// Cierra F15-5 del audit 2026-05-14 (medium). Con las micro-interfaces
// ya cerradas (NN), el handler depende de contratos estrechos, pero el
// service concreto sigue sin estar cubierto end-to-end desde HTTP.

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"hubplay/internal/auth"
	authmodel "hubplay/internal/auth/model"
	"hubplay/internal/db"
	"hubplay/internal/event"
	"hubplay/internal/library"
	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/probe"
	"hubplay/internal/scanner"
	"hubplay/internal/testutil"
)

// integProber: prober inerte. Las pruebas de integración insertan items
// directamente vía repo (no escanean filesystem), así que el scanner
// nunca llega a invocarlo — pero el constructor lo exige.
type integProber struct{}

func (integProber) Probe(context.Context, string) (*probe.Result, error) {
	return &probe.Result{Format: probe.Format{Size: 1, FormatName: "matroska,webm"}}, nil
}

type libIntegEnv struct {
	t      *testing.T
	db     *sql.DB
	repos  *db.Repositories
	svc    *library.Service
	router chi.Router
}

func newLibIntegEnv(t *testing.T) *libIntegEnv {
	t.Helper()
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(testutil.Driver(), database)
	bus := event.NewBus(testutil.NopLogger())
	scnr := scanner.New(scanner.Config{
		Items: repos.Items, Streams: repos.MediaStreams, Metadata: repos.Metadata,
		ExternalIDs: repos.ExternalIDs, Images: repos.Images, Chapters: repos.Chapters,
		People: repos.People, ItemValues: repos.ItemValues, Studios: repos.Studios,
		Collections: repos.Collections, MetaLocks: repos.ItemMetadataLocks,
		Prober: integProber{}, Bus: bus, Logger: testutil.NopLogger(),
	})
	svc := library.NewService(repos.Libraries, repos.Items, repos.MediaStreams,
		repos.Images, repos.Channels, repos.ItemValues, scnr, nil, testutil.NopLogger())
	// Drena cualquier goroutine de auto-scan antes del teardown de la DB
	// (LIFO de t.Cleanup garantiza que corre antes del Close de NewTestDB).
	t.Cleanup(svc.Shutdown)

	h := NewLibraryHandler(svc, repos.Images, repos.Metadata, repos.UserData,
		repos.Users, nil, testutil.NopLogger())

	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/libraries", h.Create)
		r.Get("/libraries", h.List)
		r.Get("/libraries/latest-items", h.LatestItems)
		r.Get("/libraries/genres", h.Genres)
		r.Get("/libraries/{id}", h.Get)
		r.Put("/libraries/{id}", h.Update)
		r.Delete("/libraries/{id}", h.Delete)
		r.Get("/libraries/{id}/items", h.Items)
	})

	return &libIntegEnv{t: t, db: database, repos: repos, svc: svc, router: r}
}

func (e *libIntegEnv) do(method, path, body string, claims *auth.Claims) *httptest.ResponseRecorder {
	e.t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if claims != nil {
		req = req.WithContext(auth.WithClaims(req.Context(), claims))
	}
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	return rr
}

// createLibrary inserta una biblioteca vía el service real en modo
// manual (sin disparar auto-scan; las pruebas siembran items a mano).
func (e *libIntegEnv) createLibrary(name, contentType string) *librarymodel.Library {
	e.t.Helper()
	lib, err := e.svc.Create(context.Background(), library.CreateRequest{
		Name: name, ContentType: contentType, ScanMode: "manual",
		Paths: []string{"/tmp/hubplay-integ-" + uuid.NewString()},
	})
	if err != nil {
		e.t.Fatalf("createLibrary(%q): %v", name, err)
	}
	return lib
}

// insertItem persiste un item top-level (parent_id NULL) disponible.
func (e *libIntegEnv) insertItem(libID, title, itemType, contentRating string, addedAt time.Time) *librarymodel.Item {
	e.t.Helper()
	it := &librarymodel.Item{
		ID:            uuid.NewString(),
		LibraryID:     libID,
		Type:          itemType,
		Title:         title,
		SortTitle:     title,
		ContentRating: contentRating,
		AddedAt:       addedAt,
		UpdatedAt:     addedAt,
		IsAvailable:   true,
	}
	if err := e.repos.Items.Create(context.Background(), it); err != nil {
		e.t.Fatalf("insertItem(%q): %v", title, err)
	}
	return it
}

// createUser crea una fila de usuario y, si maxRating != "", fija su cap
// por content-rating (la columna no la escribe UserRepository.Create).
func (e *libIntegEnv) createUser(id, role, maxRating string) {
	e.t.Helper()
	u := &authmodel.User{
		ID: id, Username: id, DisplayName: id,
		PasswordHash: "x", Role: role, CreatedAt: time.Now(),
	}
	if err := e.repos.Users.Create(context.Background(), u); err != nil {
		e.t.Fatalf("createUser(%q): %v", id, err)
	}
	if maxRating != "" {
		testutil.Exec(e.t, e.db, "UPDATE users SET max_content_rating = ? WHERE id = ?", maxRating, id)
	}
}

// decodeDataMap decodifica el envelope {"data": {...}} a un objeto.
func decodeDataMap(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	data, ok := libDecodeData(t, rr).(map[string]any)
	if !ok {
		t.Fatalf("data no es objeto: %s", rr.Body.String())
	}
	return data
}

// ─── Create → Get → persistencia real ────────────────────────────────────────

func TestIntegration_CreateThenGet_Persists(t *testing.T) {
	env := newLibIntegEnv(t)

	body := `{"name":"Pelis","content_type":"movies","scan_mode":"manual","paths":["/srv/pelis"]}`
	rr := env.do(http.MethodPost, "/api/v1/libraries", body, adminClaims())
	if rr.Code != http.StatusCreated {
		t.Fatalf("create: got %d want 201, body: %s", rr.Code, rr.Body.String())
	}
	created := decodeDataMap(t, rr)
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("create: id vacío en %v", created)
	}

	// GET por id debe leer la fila recién persistida con item_count=0.
	rr = env.do(http.MethodGet, "/api/v1/libraries/"+id, "", adminClaims())
	if rr.Code != http.StatusOK {
		t.Fatalf("get: got %d want 200, body: %s", rr.Code, rr.Body.String())
	}
	got := decodeDataMap(t, rr)
	if got["name"] != "Pelis" {
		t.Errorf("get: name = %v, want Pelis", got["name"])
	}
	if got["content_type"] != "movies" {
		t.Errorf("get: content_type = %v, want movies", got["content_type"])
	}
	if cnt, _ := got["item_count"].(float64); cnt != 0 {
		t.Errorf("get: item_count = %v, want 0", got["item_count"])
	}
}

func TestIntegration_Get_NotFound_404(t *testing.T) {
	env := newLibIntegEnv(t)
	rr := env.do(http.MethodGet, "/api/v1/libraries/no-existe", "", adminClaims())
	if rr.Code != http.StatusNotFound {
		t.Fatalf("get inexistente: got %d want 404, body: %s", rr.Code, rr.Body.String())
	}
}

// ─── List: ACL admin vs usuario (INNER JOIN library_access) ───────────────────

func TestIntegration_List_AdminSeesAll_UserScopedByAccess(t *testing.T) {
	env := newLibIntegEnv(t)
	libA := env.createLibrary("A", "movies")
	libB := env.createLibrary("B", "movies")

	// El usuario "u-2" sólo tiene acceso a libA.
	env.createUser("u-2", "user", "")
	if err := env.svc.GrantAccess(context.Background(), "u-2", libA.ID); err != nil {
		t.Fatalf("grant access: %v", err)
	}

	// Admin ve las 2 (List total).
	rr := env.do(http.MethodGet, "/api/v1/libraries", "", adminClaims())
	if rr.Code != http.StatusOK {
		t.Fatalf("admin list: %d", rr.Code)
	}
	if n := len(libDecodeData(t, rr).([]any)); n != 2 {
		t.Errorf("admin ve %d bibliotecas, want 2", n)
	}

	// Usuario sólo ve libA (ListForUser via library_access).
	rr = env.do(http.MethodGet, "/api/v1/libraries", "", userClaims())
	if rr.Code != http.StatusOK {
		t.Fatalf("user list: %d", rr.Code)
	}
	libs := libDecodeData(t, rr).([]any)
	if len(libs) != 1 {
		t.Fatalf("usuario ve %d bibliotecas, want 1", len(libs))
	}
	first := libs[0].(map[string]any)
	if first["id"] != libA.ID {
		t.Errorf("usuario ve %v, want %v (libA)", first["id"], libA.ID)
	}
	_ = libB
}

// ─── Update / Delete round-trip ───────────────────────────────────────────────

func TestIntegration_Update_Persists(t *testing.T) {
	env := newLibIntegEnv(t)
	lib := env.createLibrary("Viejo", "movies")

	rr := env.do(http.MethodPut, "/api/v1/libraries/"+lib.ID,
		`{"name":"Nuevo"}`, adminClaims())
	if rr.Code != http.StatusOK {
		t.Fatalf("update: got %d want 200, body: %s", rr.Code, rr.Body.String())
	}

	rr = env.do(http.MethodGet, "/api/v1/libraries/"+lib.ID, "", adminClaims())
	got := decodeDataMap(t, rr)
	if got["name"] != "Nuevo" {
		t.Errorf("update no persistió: name = %v, want Nuevo", got["name"])
	}
}

func TestIntegration_Delete_RemovesFromDB(t *testing.T) {
	env := newLibIntegEnv(t)
	lib := env.createLibrary("Borrame", "movies")

	rr := env.do(http.MethodDelete, "/api/v1/libraries/"+lib.ID, "", adminClaims())
	if rr.Code != http.StatusNoContent {
		t.Fatalf("delete: got %d want 204, body: %s", rr.Code, rr.Body.String())
	}

	rr = env.do(http.MethodGet, "/api/v1/libraries/"+lib.ID, "", adminClaims())
	if rr.Code != http.StatusNotFound {
		t.Errorf("post-delete get: got %d want 404", rr.Code)
	}
}

// ─── Items: paginación keyset real ────────────────────────────────────────────

func TestIntegration_Items_KeysetPagination(t *testing.T) {
	env := newLibIntegEnv(t)
	lib := env.createLibrary("Cat", "movies")
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	// 5 movies con sort_title A..E → orden determinista por sort_title asc.
	ids := map[string]string{}
	for _, title := range []string{"A", "B", "C", "D", "E"} {
		it := env.insertItem(lib.ID, title, "movie", "", base)
		ids[title] = it.ID
	}

	// Página 1: limit=2 → A, B. total=5, next_cursor presente.
	rr := env.do(http.MethodGet, "/api/v1/libraries/"+lib.ID+"/items?limit=2", "", adminClaims())
	if rr.Code != http.StatusOK {
		t.Fatalf("items p1: got %d, body: %s", rr.Code, rr.Body.String())
	}
	p1 := decodeDataMap(t, rr)
	if total, _ := p1["total"].(float64); total != 5 {
		t.Errorf("p1 total = %v, want 5", p1["total"])
	}
	items1 := p1["items"].([]any)
	if len(items1) != 2 {
		t.Fatalf("p1 items = %d, want 2", len(items1))
	}
	if got := items1[0].(map[string]any)["title"]; got != "A" {
		t.Errorf("p1[0].title = %v, want A", got)
	}
	cursor, _ := p1["next_cursor"].(string)
	if cursor == "" {
		t.Fatalf("p1 sin next_cursor: %v", p1)
	}

	// Página 2 vía cursor → C, D (keyset, no offset).
	rr = env.do(http.MethodGet,
		"/api/v1/libraries/"+lib.ID+"/items?limit=2&cursor="+cursor, "", adminClaims())
	p2 := decodeDataMap(t, rr)
	items2 := p2["items"].([]any)
	if len(items2) != 2 {
		t.Fatalf("p2 items = %d, want 2", len(items2))
	}
	if got := items2[0].(map[string]any)["title"]; got != "C" {
		t.Errorf("p2[0].title = %v, want C", got)
	}
}

// ─── Items: cap por content-rating materializado en SQL ───────────────────────

func TestIntegration_Items_ContentRatingCap(t *testing.T) {
	env := newLibIntegEnv(t)
	lib := env.createLibrary("Familiar", "movies")
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	env.insertItem(lib.ID, "Infantil", "movie", "G", base)
	env.insertItem(lib.ID, "Adolescente", "movie", "PG", base)
	env.insertItem(lib.ID, "Adulto", "movie", "R", base)
	env.insertItem(lib.ID, "SinClasificar", "movie", "", base)

	// Perfil "kid" con cap PG: la cláusula IN debe excluir R y los
	// items sin content_rating (NULL no entra en IN cuando hay cap).
	env.createUser("kid", "user", "PG")
	// Grant the kid library access — the items endpoint now enforces the
	// per-library ACL, so without a grant the request 404s before the
	// content-rating cap (which is what this test exercises) is reached.
	if err := env.svc.GrantAccess(context.Background(), "kid", lib.ID); err != nil {
		t.Fatalf("grant access: %v", err)
	}
	rr := env.do(http.MethodGet, "/api/v1/libraries/"+lib.ID+"/items?limit=50", "",
		&auth.Claims{UserID: "kid", Role: "user"})
	if rr.Code != http.StatusOK {
		t.Fatalf("items cap: got %d, body: %s", rr.Code, rr.Body.String())
	}
	data := decodeDataMap(t, rr)
	got := map[string]bool{}
	for _, raw := range data["items"].([]any) {
		got[raw.(map[string]any)["title"].(string)] = true
	}
	if !got["Infantil"] || !got["Adolescente"] {
		t.Errorf("cap PG debería incluir G y PG, got %v", got)
	}
	if got["Adulto"] {
		t.Error("cap PG no debería incluir R")
	}
	if got["SinClasificar"] {
		t.Error("cap PG no debería incluir items sin clasificar")
	}

	// Admin (sin cap) ve los 4.
	rr = env.do(http.MethodGet, "/api/v1/libraries/"+lib.ID+"/items?limit=50", "", adminClaims())
	all := decodeDataMap(t, rr)
	if total, _ := all["total"].(float64); total != 4 {
		t.Errorf("admin total = %v, want 4 (sin cap)", all["total"])
	}
}

// ─── LatestItems: orden por added_at desc ─────────────────────────────────────

func TestIntegration_LatestItems_NewestFirst(t *testing.T) {
	env := newLibIntegEnv(t)
	lib := env.createLibrary("Recientes", "movies")
	old := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	mid := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	env.insertItem(lib.ID, "Vieja", "movie", "", old)
	env.insertItem(lib.ID, "Media", "movie", "", mid)
	env.insertItem(lib.ID, "Reciente", "movie", "", recent)

	rr := env.do(http.MethodGet,
		"/api/v1/libraries/latest-items?library_id="+lib.ID+"&type=movie&limit=10", "", adminClaims())
	if rr.Code != http.StatusOK {
		t.Fatalf("latest: got %d, body: %s", rr.Code, rr.Body.String())
	}
	data := decodeDataMap(t, rr)
	items := data["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("latest items = %d, want 3", len(items))
	}
	if got := items[0].(map[string]any)["title"]; got != "Reciente" {
		t.Errorf("latest[0] = %v, want Reciente (más nuevo primero)", got)
	}
}

// ─── Genres: vocabulario desde item_value_map ─────────────────────────────────

func TestIntegration_Genres_CountsFromItemValues(t *testing.T) {
	env := newLibIntegEnv(t)
	lib := env.createLibrary("Generos", "movies")
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	a := env.insertItem(lib.ID, "Peli1", "movie", "", base)
	b := env.insertItem(lib.ID, "Peli2", "movie", "", base)
	c := env.insertItem(lib.ID, "Peli3", "movie", "", base)
	ctx := context.Background()
	if err := env.repos.ItemValues.SetGenres(ctx, a.ID, []string{"Action"}); err != nil {
		t.Fatalf("set genres a: %v", err)
	}
	if err := env.repos.ItemValues.SetGenres(ctx, b.ID, []string{"Action", "Drama"}); err != nil {
		t.Fatalf("set genres b: %v", err)
	}
	if err := env.repos.ItemValues.SetGenres(ctx, c.ID, []string{"Drama"}); err != nil {
		t.Fatalf("set genres c: %v", err)
	}

	rr := env.do(http.MethodGet, "/api/v1/libraries/genres?type=movie", "", adminClaims())
	if rr.Code != http.StatusOK {
		t.Fatalf("genres: got %d, body: %s", rr.Code, rr.Body.String())
	}
	counts := map[string]float64{}
	for _, raw := range libDecodeData(t, rr).([]any) {
		g := raw.(map[string]any)
		counts[g["name"].(string)] = g["count"].(float64)
	}
	if counts["Action"] != 2 {
		t.Errorf("Action count = %v, want 2", counts["Action"])
	}
	if counts["Drama"] != 2 {
		t.Errorf("Drama count = %v, want 2", counts["Drama"])
	}
}
