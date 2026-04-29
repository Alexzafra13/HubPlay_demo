package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/db"
	"hubplay/internal/domain"
)

// fakePeopleRepo is a minimal in-memory stand-in for the real repo so
// the handler tests don't need to spin up SQLite + scan a fake library
// just to verify response shapes. The handler is a thin DTO layer; the
// repo's behaviour is covered by people_repository_test.go (which DOES
// hit a real DB).
type fakePeopleRepo struct {
	people     map[string]*db.Person
	filmography map[string][]*db.FilmographyEntry
	getErr     error
	listErr    error
}

func (f *fakePeopleRepo) GetByID(_ context.Context, id string) (*db.Person, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	p, ok := f.people[id]
	if !ok {
		return nil, fmt.Errorf("person %s: %w", id, domain.ErrNotFound)
	}
	return p, nil
}

func (f *fakePeopleRepo) ListFilmographyByPerson(_ context.Context, id string) ([]*db.FilmographyEntry, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.filmography[id], nil
}

func newPeopleRig(t *testing.T, repo *fakePeopleRepo) (http.Handler, string) {
	t.Helper()
	imageDir := t.TempDir()
	h := NewPeopleHandler(repo, imageDir, newQuietLogger())
	r := chi.NewRouter()
	r.Get("/api/v1/people/{id}", h.Get)
	return r, imageDir
}

func TestPeople_Get_404WhenPersonMissing(t *testing.T) {
	router, _ := newPeopleRig(t, &fakePeopleRepo{
		people: map[string]*db.Person{},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/people/p-missing", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status: got %d want 404 body=%s", rr.Code, rr.Body.String())
	}
}

func TestPeople_Get_500WhenFilmographyLookupFails(t *testing.T) {
	router, _ := newPeopleRig(t, &fakePeopleRepo{
		people: map[string]*db.Person{
			"p-1": {ID: "p-1", Name: "Tom", Type: "actor"},
		},
		listErr: errors.New("db disappeared"),
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/people/p-1", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d want 500 body=%s", rr.Code, rr.Body.String())
	}
}

func TestPeople_Get_ReturnsCoreFields(t *testing.T) {
	router, _ := newPeopleRig(t, &fakePeopleRepo{
		people: map[string]*db.Person{
			"p-1": {ID: "p-1", Name: "Tom Hanks", Type: "actor"},
		},
		filmography: map[string][]*db.FilmographyEntry{
			"p-1": {
				{ItemID: "m-1", Type: "movie", Title: "Forrest Gump", Year: 1994,
					Role: "actor", CharacterName: "Forrest Gump", SortOrder: 0},
				// Year 0 should serialise as omitted, not as "year":0,
				// because TMDb genuinely returns no year for some titles
				// and we don't want the UI rendering "(0)".
				{ItemID: "m-2", Type: "movie", Title: "Untitled WIP",
					Role: "actor", SortOrder: 1},
			},
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/people/p-1", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200 body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Data struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Type        string `json:"type"`
			ImageURL    string `json:"image_url"`
			Filmography []struct {
				ItemID    string `json:"item_id"`
				Type      string `json:"type"`
				Title     string `json:"title"`
				Year      *int   `json:"year"`
				Character string `json:"character"`
				Role      string `json:"role"`
			} `json:"filmography"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v\n%s", err, rr.Body.String())
	}
	if resp.Data.ID != "p-1" || resp.Data.Name != "Tom Hanks" || resp.Data.Type != "actor" {
		t.Errorf("core fields: got %+v", resp.Data)
	}
	if resp.Data.ImageURL != "" {
		t.Errorf("image_url should be omitted when ThumbPath is empty: got %q", resp.Data.ImageURL)
	}
	if len(resp.Data.Filmography) != 2 {
		t.Fatalf("filmography len: got %d want 2", len(resp.Data.Filmography))
	}
	if resp.Data.Filmography[0].Year == nil || *resp.Data.Filmography[0].Year != 1994 {
		t.Errorf("first entry year: got %v want 1994", resp.Data.Filmography[0].Year)
	}
	if resp.Data.Filmography[0].Character != "Forrest Gump" {
		t.Errorf("character: got %q", resp.Data.Filmography[0].Character)
	}
	if resp.Data.Filmography[1].Year != nil {
		t.Errorf("Year=0 should serialise as omitted, got %v", *resp.Data.Filmography[1].Year)
	}
}

// image_url surfaces only when the thumb file actually exists on disk
// AND lives under the configured imageDir. A poisoned thumb_path that
// escapes imageDir must not be advertised — the handler defends the
// /thumb endpoint with the same check; surfacing it here would invite
// a wasted round-trip.
func TestPeople_Get_ImageURLGatedByOnDiskFile(t *testing.T) {
	repo := &fakePeopleRepo{
		people: map[string]*db.Person{
			"p-1": {ID: "p-1", Name: "Tom", Type: "actor"},
		},
		filmography: map[string][]*db.FilmographyEntry{},
	}
	router, imageDir := newPeopleRig(t, repo)

	// Case 1: ThumbPath set but file does NOT exist → no image_url.
	repo.people["p-1"].ThumbPath = filepath.Join(imageDir, ".people", "p-1", "thumb.jpg")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/people/p-1", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	var resp struct {
		Data struct {
			ImageURL string `json:"image_url"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Data.ImageURL != "" {
		t.Errorf("image_url should be empty when thumb file missing: got %q", resp.Data.ImageURL)
	}

	// Case 2: thumb file exists under imageDir → image_url surfaces.
	if err := os.MkdirAll(filepath.Dir(repo.people["p-1"].ThumbPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(repo.people["p-1"].ThumbPath, []byte{0xff, 0xd8, 0xff}, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req)
	_ = json.Unmarshal(rr2.Body.Bytes(), &resp)
	if resp.Data.ImageURL != "/api/v1/people/p-1/thumb" {
		t.Errorf("image_url surfaces when thumb on disk: got %q", resp.Data.ImageURL)
	}

	// Case 3: ThumbPath escapes imageDir → no image_url.
	escape := filepath.Join(imageDir, "..", "elsewhere.jpg")
	repo.people["p-1"].ThumbPath = escape
	rr3 := httptest.NewRecorder()
	router.ServeHTTP(rr3, req)
	resp.Data.ImageURL = ""
	_ = json.Unmarshal(rr3.Body.Bytes(), &resp)
	if resp.Data.ImageURL != "" {
		t.Errorf("escape should be hidden: got %q", resp.Data.ImageURL)
	}
}
