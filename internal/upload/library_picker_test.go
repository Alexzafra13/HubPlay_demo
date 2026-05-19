package upload_test

import (
	"context"
	"errors"
	"testing"

	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/upload"
)

func newPicker(libs ...*librarymodel.Library) *upload.LibraryPicker {
	return upload.NewLibraryPicker(&fakeLibraryStore{libs: libs})
}

func TestPicker_AutoPick_PrefersFirstMovies(t *testing.T) {
	p := newPicker(
		&librarymodel.Library{ID: "l-music", ContentType: "music", Paths: []string{"/music"}},
		&librarymodel.Library{ID: "l-mov", ContentType: "movies", Paths: []string{"/movies"}},
		&librarymodel.Library{ID: "l-shows", ContentType: "shows", Paths: []string{"/shows"}},
	)
	lib, err := p.PickDestination(context.Background(), "u-alex", "", upload.KindVideo)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if lib.ID != "l-mov" {
		t.Errorf("picked %s, want l-mov (first compatible)", lib.ID)
	}
}

func TestPicker_AutoPick_FallsBackToShows(t *testing.T) {
	p := newPicker(
		&librarymodel.Library{ID: "l-music", ContentType: "music", Paths: []string{"/music"}},
		&librarymodel.Library{ID: "l-shows", ContentType: "shows", Paths: []string{"/shows"}},
	)
	lib, err := p.PickDestination(context.Background(), "u-alex", "", upload.KindVideo)
	if err != nil || lib.ID != "l-shows" {
		t.Errorf("got %v err=%v, want l-shows", lib, err)
	}
}

func TestPicker_AutoPick_SkipsNoPaths(t *testing.T) {
	p := newPicker(
		&librarymodel.Library{ID: "l-empty", ContentType: "movies", Paths: nil},
		&librarymodel.Library{ID: "l-real", ContentType: "movies", Paths: []string{"/real"}},
	)
	lib, _ := p.PickDestination(context.Background(), "u-alex", "", upload.KindVideo)
	if lib.ID != "l-real" {
		t.Errorf("picked %s, want l-real (l-empty has no paths)", lib.ID)
	}
}

func TestPicker_AutoPick_NoLibraryAvailable(t *testing.T) {
	p := newPicker() // user has nothing
	_, err := p.PickDestination(context.Background(), "u-alex", "", upload.KindVideo)
	if !errors.Is(err, upload.ErrNoLibraryDestination) {
		t.Errorf("want ErrNoLibraryDestination, got %v", err)
	}
}

func TestPicker_AutoPick_RejectsLiveTVOnly(t *testing.T) {
	p := newPicker(
		&librarymodel.Library{ID: "l-live", ContentType: "livetv", Paths: []string{"/live"}},
	)
	_, err := p.PickDestination(context.Background(), "u-alex", "", upload.KindVideo)
	if !errors.Is(err, upload.ErrNoLibraryDestination) {
		t.Errorf("want ErrNoLibraryDestination, got %v", err)
	}
}

func TestPicker_HintHonoured(t *testing.T) {
	p := newPicker(
		&librarymodel.Library{ID: "l-a", ContentType: "movies", Paths: []string{"/a"}},
		&librarymodel.Library{ID: "l-b", ContentType: "shows", Paths: []string{"/b"}},
	)
	lib, err := p.PickDestination(context.Background(), "u-alex", "l-b", upload.KindVideo)
	if err != nil || lib.ID != "l-b" {
		t.Errorf("got %v err=%v, want l-b (honoured hint)", lib, err)
	}
}

func TestPicker_HintRejected_NoAccess(t *testing.T) {
	// User has access to l-a but the hint is l-z (not in their list).
	p := newPicker(
		&librarymodel.Library{ID: "l-a", ContentType: "movies", Paths: []string{"/a"}},
	)
	_, err := p.PickDestination(context.Background(), "u-alex", "l-z", upload.KindVideo)
	if err == nil {
		t.Error("hint outside user's libs accepted")
	}
}

func TestPicker_HintRejected_Incompatible(t *testing.T) {
	p := newPicker(
		&librarymodel.Library{ID: "l-music", ContentType: "music", Paths: []string{"/music"}},
	)
	_, err := p.PickDestination(context.Background(), "u-alex", "l-music", upload.KindVideo)
	if !errors.Is(err, upload.ErrLibraryNotEligible) {
		t.Errorf("want ErrLibraryNotEligible, got %v", err)
	}
}

func TestPicker_SubtitleNeedsCompatibleLib(t *testing.T) {
	p := newPicker(
		&librarymodel.Library{ID: "l-mov", ContentType: "movies", Paths: []string{"/movies"}},
	)
	lib, err := p.PickDestination(context.Background(), "u-alex", "", upload.KindSubtitle)
	if err != nil || lib.ID != "l-mov" {
		t.Errorf("got %v err=%v, want l-mov (subtitles ok into movies)", lib, err)
	}
}
