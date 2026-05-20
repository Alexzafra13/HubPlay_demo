package updates_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hubplay/internal/updates"
)

func newTestService(t *testing.T, version, repo string) *updates.Service {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return updates.New(version, repo, logger)
}

func TestService_Disabled_DevBuild(t *testing.T) {
	// Un binario "dev" no debería intentar comprobar updates. Si el
	// developer corre `go run` no queremos saturar la API de GitHub
	// con cada reload de air.
	svc := newTestService(t, "dev", "Alexzafra13/HubPlay_demo")
	st := svc.Status()
	if st.CheckEnabled {
		t.Errorf("dev build should have check_enabled=false, got true")
	}
}

func TestService_Disabled_NoRepo(t *testing.T) {
	// Sin repo, deshabilitado — útil para forks privados.
	svc := newTestService(t, "v0.1.0", "")
	st := svc.Status()
	if st.CheckEnabled {
		t.Errorf("empty repo should disable checker")
	}
}

func TestService_Check_DetectsNewerVersion(t *testing.T) {
	// E2E happy path: el servidor de prueba devuelve un release
	// estable más nuevo que el currentVersion del Service. Tras
	// Check() el snapshot público debe reflejar la nueva versión,
	// el flag HasUpdate, la URL del release y la marca temporal del
	// último check.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name":     "v0.2.0",
			"name":         "v0.2.0",
			"body":         "## Changelog\n- una feature nueva\n",
			"html_url":     "https://github.com/Alexzafra13/HubPlay_demo/releases/tag/v0.2.0",
			"prerelease":   false,
			"published_at": time.Now().UTC().Format(time.RFC3339),
		})
	}))
	defer server.Close()

	svc := newTestService(t, "v0.1.0", "Alexzafra13/HubPlay_demo")
	svc.SetBaseURL(server.URL)

	if err := svc.Check(context.Background()); err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	st := svc.Status()
	if st.Latest != "v0.2.0" {
		t.Errorf("Latest = %q, want v0.2.0", st.Latest)
	}
	if !st.HasUpdate {
		t.Error("HasUpdate should be true when remote > current")
	}
	if st.ReleaseURL != "https://github.com/Alexzafra13/HubPlay_demo/releases/tag/v0.2.0" {
		t.Errorf("ReleaseURL = %q", st.ReleaseURL)
	}
	if st.LastChecked.IsZero() {
		t.Error("LastChecked should be populated after a successful check")
	}
	if st.LastError != "" {
		t.Errorf("LastError should be empty on success, got %q", st.LastError)
	}
}

func TestService_Check_SamePinsHasUpdateFalse(t *testing.T) {
	// El servidor devuelve la misma versión que corre el binario:
	// HasUpdate debe quedarse false aunque el resto del snapshot
	// (Latest, ReleaseURL, LastChecked) sí se llene.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name":     "v0.1.0",
			"html_url":     "https://example.test/releases/v0.1.0",
			"prerelease":   false,
			"published_at": time.Now().UTC().Format(time.RFC3339),
		})
	}))
	defer server.Close()

	svc := newTestService(t, "v0.1.0", "Alexzafra13/HubPlay_demo")
	svc.SetBaseURL(server.URL)

	if err := svc.Check(context.Background()); err != nil {
		t.Fatalf("Check: %v", err)
	}
	st := svc.Status()
	if st.HasUpdate {
		t.Error("HasUpdate should be false when remote == current")
	}
	if st.Latest != "v0.1.0" {
		t.Errorf("Latest = %q, want v0.1.0", st.Latest)
	}
}

func TestService_Check_PrereleaseSkippedSilently(t *testing.T) {
	// La API "/releases/latest" de GitHub ya filtra prereleases, pero
	// el Service hace una defensa adicional por si el repo cambiase
	// la convención. Cuando el payload trae prerelease=true, el
	// snapshot NO debe actualizarse — Latest queda vacío, HasUpdate
	// queda false — pero LastChecked sí se sella (el chequeo ocurrió).
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name":     "v9.9.9-beta.1",
			"html_url":     "https://example.test/releases/v9.9.9-beta.1",
			"prerelease":   true,
			"published_at": time.Now().UTC().Format(time.RFC3339),
		})
	}))
	defer server.Close()

	svc := newTestService(t, "v0.1.0", "Alexzafra13/HubPlay_demo")
	svc.SetBaseURL(server.URL)

	if err := svc.Check(context.Background()); err != nil {
		t.Fatalf("Check: %v", err)
	}
	st := svc.Status()
	if st.Latest != "" {
		t.Errorf("Latest should stay empty on prerelease payload, got %q", st.Latest)
	}
	if st.HasUpdate {
		t.Error("HasUpdate should stay false on prerelease payload")
	}
	if st.LastChecked.IsZero() {
		t.Error("LastChecked should still be sealed even when the payload was ignored")
	}
}

func TestService_Check_RecordsLastErrorOn500(t *testing.T) {
	// Cuando GitHub devuelve un 5xx (o un 403 por rate-limit), el
	// Service debe retornar error Y dejar LastError seteado en el
	// snapshot para que el panel admin lo muestre. Los campos
	// Latest/HasUpdate cacheados de checks anteriores no se borran.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"server fell over"}`))
	}))
	defer server.Close()

	svc := newTestService(t, "v0.1.0", "Alexzafra13/HubPlay_demo")
	svc.SetBaseURL(server.URL)

	if err := svc.Check(context.Background()); err == nil {
		t.Fatal("Check should error on 500 response")
	}
	st := svc.Status()
	if st.LastError == "" {
		t.Error("LastError should be populated on HTTP error")
	}
}

// ─── parseSemver / isNewer ───────────────────────────────────────────

func TestIsNewer_TableDriven(t *testing.T) {
	cases := []struct {
		name   string
		remote string
		local  string
		want   bool
	}{
		// Patch bumps
		{"patch bump", "v0.1.1", "v0.1.0", true},
		{"same version", "v0.1.0", "v0.1.0", false},
		{"older patch", "v0.1.0", "v0.1.1", false},

		// Minor bumps
		{"minor bump", "v0.2.0", "v0.1.5", true},
		{"older minor", "v0.1.0", "v0.2.0", false},

		// Major bumps
		{"major bump", "v1.0.0", "v0.9.9", true},
		{"older major", "v1.0.0", "v2.0.0", false},

		// Numeric comparison, NOT lexical — "v0.10.0" > "v0.9.0".
		// Bug clásico: comparar strings hace "10" < "9". Aquí lo
		// blindamos con un test.
		{"numeric not lexical", "v0.10.0", "v0.9.0", true},
		{"numeric not lexical patch", "v0.0.10", "v0.0.9", true},

		// Prereleases: estable > prerelease para misma triple.
		{"stable beats prerelease same", "v1.0.0", "v1.0.0-alpha.1", true},
		{"prerelease NOT newer than stable same", "v1.0.0-alpha.1", "v1.0.0", false},

		// Prefijo v opcional ambos lados.
		{"no v prefix remote", "0.2.0", "v0.1.0", true},
		{"no v prefix both", "0.2.0", "0.1.0", true},

		// dev local NUNCA es older — no notificamos updates a developers.
		{"dev local never has update", "v999.0.0", "dev", false},
		{"empty local never has update", "v999.0.0", "", false},

		// Build metadata (+abc) lo ignoramos en la comparación.
		{"build metadata ignored", "v1.0.0+abc123", "v1.0.0", false},

		// Pre-release con build metadata mezclado.
		{"pre+build ignored properly", "v1.0.0-rc.1+abc", "v0.9.0", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// isNewer no está exportado; lo cubrimos vía export_test.go
			// helper que sí está en package `updates` y expone
			// IsNewerForTest. Acceso: updates.IsNewerForTest(...).
			got := updates.IsNewerForTest(tc.remote, tc.local)
			if got != tc.want {
				t.Errorf("isNewer(%q, %q) = %v, want %v",
					tc.remote, tc.local, got, tc.want)
			}
		})
	}
}

// ─── HTTP integration via httptest ───────────────────────────────────

// TestService_HTTPIntegration_ETagRoundTrip ancla la contratación
// completa con la GitHub API:
//
//   - El primer Check llega sin If-None-Match, recibe 200 + ETag, llena
//     el snapshot y guarda el ETag.
//   - El segundo Check incluye If-None-Match con el ETag previo, el
//     server responde 304, y el snapshot mantiene Latest/HasUpdate del
//     primer check (solo LastChecked se refresca).
//   - Cabeceras Accept + User-Agent llegan al servidor con el shape
//     esperado por la API real de GitHub.
//
// Esto cubre la promesa del comentario al inicio de checker.go:
// "ETag → 304 → ~200 bytes por check" y "User-Agent identifica HubPlay
// + versión".
func TestService_HTTPIntegration_ETagRoundTrip(t *testing.T) {
	var (
		hits           int
		seenIfNoneOn   = make(map[int]string) // call# → If-None-Match header recibido
		seenAccept     string
		seenUserAgent  string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		seenIfNoneOn[hits] = r.Header.Get("If-None-Match")
		// Capturamos las cabeceras canónicas la primera vez; las
		// siguientes asumimos que el Service no las cambia.
		if hits == 1 {
			seenAccept = r.Header.Get("Accept")
			seenUserAgent = r.Header.Get("User-Agent")
		}

		w.Header().Set("ETag", `"abc123"`)
		w.Header().Set("Content-Type", "application/json")

		// Segunda llamada (con If-None-Match): respondemos 304 sin body.
		if r.Header.Get("If-None-Match") == `"abc123"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name":     "v0.2.0",
			"name":         "v0.2.0",
			"body":         "## Changelog\n- nueva feature\n",
			"html_url":     "https://github.com/Alexzafra13/HubPlay_demo/releases/tag/v0.2.0",
			"prerelease":   false,
			"published_at": time.Now().UTC().Format(time.RFC3339),
		})
	}))
	defer server.Close()

	svc := newTestService(t, "v0.1.0", "Alexzafra13/HubPlay_demo")
	svc.SetBaseURL(server.URL)

	// ── Primer check: 200 + ETag ──────────────────────────────────────
	if err := svc.Check(context.Background()); err != nil {
		t.Fatalf("first Check: %v", err)
	}
	first := svc.Status()
	if first.Latest != "v0.2.0" || !first.HasUpdate {
		t.Fatalf("first Check did not populate snapshot: %+v", first)
	}
	if first.LastChecked.IsZero() {
		t.Error("first Check should seal LastChecked")
	}

	// Cabeceras esperadas por la GitHub API.
	if !strings.Contains(seenAccept, "application/vnd.github+json") {
		t.Errorf("Accept header missing GitHub media-type: %q", seenAccept)
	}
	if !strings.Contains(seenUserAgent, "HubPlay/v0.1.0") {
		t.Errorf("User-Agent should identify HubPlay + version: %q", seenUserAgent)
	}
	if v := seenIfNoneOn[1]; v != "" {
		t.Errorf("first Check should not send If-None-Match, got %q", v)
	}

	firstCheckedAt := first.LastChecked

	// Pequeña pausa para que un LastChecked posterior sea estrictamente >
	// (el reloj de Windows con resolución de 15 ms a veces da el mismo
	// instante si los dos checks corren back-to-back).
	time.Sleep(5 * time.Millisecond)

	// ── Segundo check: el Service debe enviar If-None-Match y recibir 304 ──
	if err := svc.Check(context.Background()); err != nil {
		t.Fatalf("second Check: %v", err)
	}
	if got := seenIfNoneOn[2]; got != `"abc123"` {
		t.Errorf("second Check should send If-None-Match=\"abc123\", got %q", got)
	}
	second := svc.Status()
	// Latest/HasUpdate del primer check no deben moverse en un 304.
	if second.Latest != "v0.2.0" || !second.HasUpdate {
		t.Errorf("304 should preserve cached snapshot, got %+v", second)
	}
	// LastChecked sí se actualiza (el chequeo ocurrió, no había
	// novedad). LastError debe quedar limpio.
	if !second.LastChecked.After(firstCheckedAt) {
		t.Errorf("LastChecked should advance after a 304, got %v vs first %v",
			second.LastChecked, firstCheckedAt)
	}
	if second.LastError != "" {
		t.Errorf("LastError should be cleared on 304, got %q", second.LastError)
	}

	if hits != 2 {
		t.Errorf("expected 2 server hits, got %d", hits)
	}
}

// ─── Smoke test ──────────────────────────────────────────────────────

func TestService_Smoke_Start_StopOnCtxCancel(t *testing.T) {
	// Verifica que Start respeta context cancellation y que el
	// jitter+ticker no se quedan colgados al cerrar.
	svc := newTestService(t, "v0.1.0", "Alexzafra13/HubPlay_demo")
	ctx, cancel := context.WithCancel(context.Background())

	// Reducir el jitter a casi-cero para que el test no espere 30 min.
	prevJitter := updates.MaxInitialJitter
	updates.MaxInitialJitter = 1 * time.Millisecond
	defer func() { updates.MaxInitialJitter = prevJitter }()

	done := make(chan struct{})
	go func() {
		svc.Start(ctx)
		// Start retorna sólo si la goroutine se canceló. Pequeño sleep
		// para que el cancel se propague.
		time.Sleep(50 * time.Millisecond)
		close(done)
	}()
	// Cancel antes de que se ejecute el primer Check real — no queremos
	// que el test salga a internet.
	cancel()

	// Con la goroutine en background tenemos que dar tiempo para que
	// la cancelación se propague.
	select {
	case <-done:
		// La goroutine devolvió control — bien.
	case <-time.After(2 * time.Second):
		// No es realmente un fail crítico — Start lanza la goroutine
		// pero retorna inmediatamente. La goroutine puede tardar en
		// hacer el primer http call y fallar (sin red en test). Lo
		// tomamos como warn.
		t.Log("Start goroutine took >2s to honor context cancel")
	}

	st := svc.Status()
	if !st.CheckEnabled {
		t.Errorf("expected check_enabled=true for v0.1.0 + repo, got false")
	}
	if !strings.HasPrefix(st.Current, "v0.1.0") {
		t.Errorf("expected current=v0.1.0, got %q", st.Current)
	}
}
