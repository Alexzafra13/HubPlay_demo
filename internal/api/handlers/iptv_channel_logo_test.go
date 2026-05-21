package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	iptvmodel "hubplay/internal/iptv/model"
	"hubplay/internal/testutil"
)

// fakeLogoIPTVService — minimal stub que ejercita sólo los métodos del
// flujo logo override. El iptvFakeService grande del archivo principal
// devuelve nil para todo, lo que no nos deja inspeccionar lo que el
// handler pasó al service. Aquí guardamos los args para los asserts.
type fakeLogoIPTVService struct {
	iptvFakeService // promovido para satisfacer la interfaz entera
	mu              sync.Mutex
	setURLCalls     []struct{ channelID, logoURL string }
	setFileCalls    []struct{ channelID, basename string }
	clearCalls      []string
	currentOverride *iptvmodel.ChannelLogoOverride
}

func (f *fakeLogoIPTVService) SetChannelLogoURL(_ context.Context, channelID, logoURL string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setURLCalls = append(f.setURLCalls, struct{ channelID, logoURL string }{channelID, logoURL})
	return nil
}

func (f *fakeLogoIPTVService) SetChannelLogoFile(_ context.Context, channelID, basename string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setFileCalls = append(f.setFileCalls, struct{ channelID, basename string }{channelID, basename})
	return "", nil
}

func (f *fakeLogoIPTVService) ClearChannelLogo(_ context.Context, channelID string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clearCalls = append(f.clearCalls, channelID)
	return "", nil
}

func (f *fakeLogoIPTVService) GetChannelLogoOverride(_ context.Context, _ string) (*iptvmodel.ChannelLogoOverride, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.currentOverride, nil
}

func newLogoTestEnv(t *testing.T, imageDir string) (*IPTVHandler, *fakeLogoIPTVService, chi.Router) {
	t.Helper()
	fake := &fakeLogoIPTVService{iptvFakeService: *newIPTVFakeService()}
	h := NewIPTVHandler(fake, &iptvFakeProxy{}, nil, nil, imageDir, &iptvFakeLibraryRepo{}, &iptvFakeAccess{}, nil, nil, testutil.NopLogger())
	r := chi.NewRouter()
	r.Route("/api/v1/channels/{channelId}", func(r chi.Router) {
		r.Put("/logo", h.SetChannelLogo)
		r.Post("/logo/upload", h.UploadChannelLogo)
		r.Delete("/logo", h.ClearChannelLogo)
	})
	return h, fake, r
}

func TestSetChannelLogo_AcceptsHTTPURL(t *testing.T) {
	_, fake, router := newLogoTestEnv(t, "")

	body := bytes.NewBufferString(`{"logo_url":"https://example.com/logo.png"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/channels/ch-1/logo", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200, body=%s", w.Code, w.Body.String())
	}
	if len(fake.setURLCalls) != 1 || fake.setURLCalls[0].channelID != "ch-1" || fake.setURLCalls[0].logoURL != "https://example.com/logo.png" {
		t.Fatalf("SetChannelLogoURL called wrong: %+v", fake.setURLCalls)
	}
}

func TestSetChannelLogo_RejectsEmptyURL(t *testing.T) {
	_, fake, router := newLogoTestEnv(t, "")

	body := bytes.NewBufferString(`{"logo_url":""}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/channels/ch-1/logo", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty url should 400, got %d", w.Code)
	}
	if len(fake.setURLCalls) != 0 {
		t.Fatalf("service should not have been called: %+v", fake.setURLCalls)
	}
}

func TestSetChannelLogo_RejectsFTPScheme(t *testing.T) {
	// Empty/relative URLs and non-http(s) schemes get rejected at the
	// handler — no point in upserting a row the proxy would 404 on
	// every fetch attempt because the SSRF guard refuses non-http
	// schemes anyway.
	_, _, router := newLogoTestEnv(t, "")
	body := bytes.NewBufferString(`{"logo_url":"ftp://example.com/logo.png"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/channels/ch-1/logo", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("ftp url should 400, got %d", w.Code)
	}
}

func TestClearChannelLogo_Returns204(t *testing.T) {
	_, fake, router := newLogoTestEnv(t, "")

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/channels/ch-1/logo", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status: got %d want 204", w.Code)
	}
	if len(fake.clearCalls) != 1 || fake.clearCalls[0] != "ch-1" {
		t.Fatalf("ClearChannelLogo called wrong: %+v", fake.clearCalls)
	}
}

func TestUploadChannelLogo_AcceptsPNG(t *testing.T) {
	dir := t.TempDir()
	_, fake, router := newLogoTestEnv(t, dir)

	// 4×4 PNG real — pasa el guard de EnforceMaxPixels y el sniff
	// de Content-Type (`image/png`) sin trampa.
	var pngBuf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: 255, G: 0, B: 0, A: 255})
		}
	}
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "logo.png")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(pngBuf.Bytes()); err != nil {
		t.Fatalf("write part: %v", err)
	}
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/channels/ch-1/logo/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200, body=%s", w.Code, w.Body.String())
	}
	if len(fake.setFileCalls) != 1 || fake.setFileCalls[0].channelID != "ch-1" || !strings.HasPrefix(fake.setFileCalls[0].basename, "ch-1-") {
		t.Fatalf("SetChannelLogoFile called wrong: %+v", fake.setFileCalls)
	}

	var resp struct {
		Data struct {
			ChannelID string `json:"channel_id"`
			LogoFile  string `json:"logo_file"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.ChannelID != "ch-1" || !strings.HasSuffix(resp.Data.LogoFile, ".png") {
		t.Fatalf("response payload wrong: %+v", resp.Data)
	}
}

func TestUploadChannelLogo_RejectsNonImage(t *testing.T) {
	dir := t.TempDir()
	_, fake, router := newLogoTestEnv(t, dir)

	// Bytes que no son ninguna imagen válida — el sniff de la primera
	// línea ("not an image at all") da text/plain, que IsValidContentType
	// rechaza.
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "fake.png")
	_, _ = part.Write([]byte("not an image at all, just plain text bytes"))
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/channels/ch-1/logo/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("non-image should 400, got %d body=%s", w.Code, w.Body.String())
	}
	if len(fake.setFileCalls) != 0 {
		t.Fatalf("service should not have been called: %+v", fake.setFileCalls)
	}
}

func TestUploadChannelLogo_503WhenStorageMissing(t *testing.T) {
	// imageDir="" → el handler debe rehusarse en vez de escribir en cwd.
	_, _, router := newLogoTestEnv(t, "")

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "logo.png")
	_, _ = part.Write([]byte("\x89PNG\r\n\x1a\n"))
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/channels/ch-1/logo/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("no imageDir should 503, got %d", w.Code)
	}
}
