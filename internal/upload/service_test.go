package upload_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	authmodel "hubplay/internal/auth/model"
	"hubplay/internal/domain"
	"hubplay/internal/event"
	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/probe"
	"hubplay/internal/upload"
)

// ─── Test doubles ───────────────────────────────────────────────────

type fakeUserStore struct {
	user   *authmodel.User
	used   int64
	maxRes int64 // -1 = unlimited
}

func (f *fakeUserStore) GetByID(_ context.Context, id string) (*authmodel.User, error) {
	if f.user != nil && f.user.ID == id {
		u := *f.user
		u.UploadUsedBytes = f.used
		return &u, nil
	}
	return nil, errors.New("not found")
}

func (f *fakeUserStore) ReserveUploadBytes(_ context.Context, _ string, delta int64) error {
	if f.user == nil || !f.user.CanUpload {
		return domain.ErrUploadQuotaExceeded
	}
	if f.maxRes >= 0 && f.used+delta > f.maxRes {
		return domain.ErrUploadQuotaExceeded
	}
	f.used += delta
	return nil
}

func (f *fakeUserStore) ReleaseUploadBytes(_ context.Context, _ string, delta int64) error {
	f.used -= delta
	if f.used < 0 {
		f.used = 0
	}
	return nil
}

type fakeAuditStore struct {
	mu   sync.Mutex
	rows []upload.AuditRow
}

func (f *fakeAuditStore) Insert(_ context.Context, row upload.AuditRow) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, row)
	return nil
}

func (f *fakeAuditStore) lastOutcome() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.rows) == 0 {
		return ""
	}
	return f.rows[len(f.rows)-1].Outcome
}

type fakeLibraryStore struct {
	libs []*librarymodel.Library
}

func (f *fakeLibraryStore) GetByID(_ context.Context, id string) (*librarymodel.Library, error) {
	for _, l := range f.libs {
		if l.ID == id {
			return l, nil
		}
	}
	return nil, errors.New("not found")
}

func (f *fakeLibraryStore) ListForUser(_ context.Context, _ string) ([]*librarymodel.Library, error) {
	return f.libs, nil
}

type fakeProber struct {
	durationMs int64
	err        error
}

func (f *fakeProber) Probe(_ context.Context, _ string) (*probe.Result, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &probe.Result{
		Format: probe.Format{Duration: time.Duration(f.durationMs) * time.Millisecond},
	}, nil
}

// captureBus es un sink de eventos para que los asserts puedan ver
// qué fases publicó el pipeline.
type captureBus struct {
	mu     sync.Mutex
	events []event.Event
}

func (b *captureBus) Publish(e event.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, e)
}

func (b *captureBus) phases() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []string
	for _, e := range b.events {
		if e.Type == event.UploadPhase {
			out = append(out, e.Data["phase"].(string))
		}
	}
	return out
}

func (b *captureBus) terminalType() event.Type {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := len(b.events) - 1; i >= 0; i-- {
		if b.events[i].Type == event.UploadDone || b.events[i].Type == event.UploadError {
			return b.events[i].Type
		}
	}
	return ""
}

// ─── Helpers ────────────────────────────────────────────────────────

func newServiceFixture(t *testing.T) (*upload.Service, *fakeUserStore, *fakeAuditStore, *captureBus, string, string) {
	t.Helper()
	stagingRoot := filepath.Join(t.TempDir(), "staging")
	libDir := filepath.Join(t.TempDir(), "movies")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := upload.NewStagingDir(stagingRoot)
	if err != nil {
		t.Fatal(err)
	}
	users := &fakeUserStore{
		user: &authmodel.User{
			ID:               "u-alex",
			Username:         "alex",
			IsActive:         true,
			CanUpload:        true,
			UploadQuotaBytes: 1 << 40,
		},
		maxRes: 1 << 40,
	}
	audit := &fakeAuditStore{}
	bus := &captureBus{}
	libs := &fakeLibraryStore{
		libs: []*librarymodel.Library{
			{ID: "lib-mov", Name: "Movies", ContentType: "movies", Paths: []string{libDir}},
		},
	}
	picker := upload.NewLibraryPicker(libs)
	prober := &fakeProber{durationMs: 60_000}
	svc := upload.NewService(upload.DefaultConfig(), st, users, audit, bus, picker, prober, nil, slog.Default())
	return svc, users, audit, bus, stagingRoot, libDir
}

// writeFakeMKV crea un fichero con la firma EBML de matroska seguida
// de N bytes de relleno. Vale para magic-byte; ffprobe está mockeado
// en estos tests así que no necesita ser un MKV real.
func writeFakeMKV(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	body := make([]byte, size)
	copy(body, []byte{0x1A, 0x45, 0xDF, 0xA3, 0x01, 0x02, 0x03, 0x04})
	if err := os.WriteFile(path, body, 0o640); err != nil {
		t.Fatal(err)
	}
}

// ─── PreCreate ──────────────────────────────────────────────────────

func TestService_PreCreate_Accepts(t *testing.T) {
	svc, users, _, _, _, _ := newServiceFixture(t)
	res, err := svc.PreCreate(context.Background(), upload.PreCreateInput{
		UserID:       "u-alex",
		UploadID:     "up-1",
		OriginalName: "Some Movie (2024).mkv",
		Size:         700 << 20,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.SanitizedName != "Some Movie (2024).mkv" {
		t.Errorf("sanitized = %q", res.SanitizedName)
	}
	if users.used != 700<<20 {
		t.Errorf("bytes not reserved, used=%d", users.used)
	}
}

func TestService_PreCreate_RejectsInactiveUser(t *testing.T) {
	svc, users, _, _, _, _ := newServiceFixture(t)
	users.user.IsActive = false
	_, err := svc.PreCreate(context.Background(), upload.PreCreateInput{
		UserID: "u-alex", UploadID: "up-1", OriginalName: "x.mkv", Size: 100,
	})
	if err == nil {
		t.Error("inactive user passed PreCreate")
	}
	if users.used != 0 {
		t.Errorf("bytes leaked on rejected pre-create: used=%d", users.used)
	}
}

func TestService_PreCreate_RejectsNoPermission(t *testing.T) {
	svc, users, _, _, _, _ := newServiceFixture(t)
	users.user.CanUpload = false
	_, err := svc.PreCreate(context.Background(), upload.PreCreateInput{
		UserID: "u-alex", UploadID: "up-1", OriginalName: "x.mkv", Size: 100,
	})
	if err == nil {
		t.Error("can_upload=false passed PreCreate")
	}
}

func TestService_PreCreate_RejectsBadExtension(t *testing.T) {
	svc, _, _, _, _, _ := newServiceFixture(t)
	_, err := svc.PreCreate(context.Background(), upload.PreCreateInput{
		UserID: "u-alex", UploadID: "up-1", OriginalName: "evil.exe", Size: 100,
	})
	if !errors.Is(err, upload.ErrExtensionNotAllowed) {
		t.Errorf("want ErrExtensionNotAllowed, got %v", err)
	}
}

func TestService_PreCreate_RejectsOversize(t *testing.T) {
	cfg := upload.DefaultConfig()
	// Re-create the service with a tighter cap.
	st, _ := upload.NewStagingDir(filepath.Join(t.TempDir(), "staging"))
	users := &fakeUserStore{
		user: &authmodel.User{ID: "u-alex", IsActive: true, CanUpload: true},
		maxRes: 1 << 40,
	}
	cfg.MaxUploadBytes = 1024
	svc := upload.NewService(cfg, st, users, &fakeAuditStore{}, &captureBus{},
		upload.NewLibraryPicker(&fakeLibraryStore{}), &fakeProber{durationMs: 60000}, nil, slog.Default())

	_, err := svc.PreCreate(context.Background(), upload.PreCreateInput{
		UserID: "u-alex", UploadID: "up-1", OriginalName: "x.mkv", Size: 2048,
	})
	if err == nil {
		t.Error("oversize upload passed PreCreate")
	}
}

func TestService_PreCreate_RejectsBadFilename(t *testing.T) {
	svc, _, _, _, _, _ := newServiceFixture(t)
	_, err := svc.PreCreate(context.Background(), upload.PreCreateInput{
		UserID: "u-alex", UploadID: "up-1", OriginalName: "...", Size: 100,
	})
	if err == nil {
		t.Error("invalid filename passed PreCreate")
	}
}

// ─── Finish: happy path ─────────────────────────────────────────────

func TestService_Finish_HappyPath(t *testing.T) {
	svc, users, audit, bus, stagingRoot, libDir := newServiceFixture(t)

	// Prep: reserve and stage the file as if PreCreate had run.
	_, err := svc.PreCreate(context.Background(), upload.PreCreateInput{
		UserID: "u-alex", UploadID: "up-1", OriginalName: "Movie.mkv", Size: 1024,
	})
	if err != nil {
		t.Fatalf("pre-create: %v", err)
	}
	src := filepath.Join(stagingRoot, "u-alex", "up-1", "Movie.mkv")
	writeFakeMKV(t, src, 1024)

	got := svc.Finish(context.Background(), upload.FinishInput{
		UserID:        "u-alex",
		UploadID:      "up-1",
		OriginalName:  "Movie.mkv",
		SanitizedName: "Movie.mkv",
		Size:          1024,
		SourcePath:    src,
	})

	if got.Outcome != "accepted" {
		t.Fatalf("outcome = %s, want accepted (err=%s)", got.Outcome, got.ErrorMessage)
	}
	if got.LibraryID != "lib-mov" {
		t.Errorf("library = %s", got.LibraryID)
	}
	wantPath := filepath.Join(libDir, "Movie.mkv")
	if got.FinalPath != wantPath {
		t.Errorf("final_path = %s, want %s", got.FinalPath, wantPath)
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("file not at target: %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("source still exists post-move: %v", err)
	}
	if got.SHA256 == "" {
		t.Error("sha256 missing on accepted upload")
	}

	// Bytes reservados PERMANECEN porque el upload aterrizó: la cuota
	// se gasta de verdad. Sólo rejected/error/aborted libera.
	if users.used != 1024 {
		t.Errorf("quota dropped on accepted upload: used=%d", users.used)
	}

	// Audit row está y dice 'accepted'.
	if audit.lastOutcome() != "accepted" {
		t.Errorf("audit outcome = %q", audit.lastOutcome())
	}

	// Eventos: phases en orden + UploadDone terminal.
	wantPhases := []string{"validating", "probing", "moving", "indexing"}
	gotPhases := bus.phases()
	if !equalStrings(gotPhases, wantPhases) {
		t.Errorf("phases = %v, want %v", gotPhases, wantPhases)
	}
	if bus.terminalType() != event.UploadDone {
		t.Errorf("terminal = %s, want UploadDone", bus.terminalType())
	}
}

// ─── Finish: rejection paths ────────────────────────────────────────

func TestService_Finish_RejectsMimeMismatch(t *testing.T) {
	svc, users, audit, bus, stagingRoot, _ := newServiceFixture(t)
	_, _ = svc.PreCreate(context.Background(), upload.PreCreateInput{
		UserID: "u-alex", UploadID: "up-1", OriginalName: "x.mkv", Size: 1024,
	})
	src := filepath.Join(stagingRoot, "u-alex", "up-1", "x.mkv")
	// Bytes random — no magic match.
	_ = os.MkdirAll(filepath.Dir(src), 0o750)
	_ = os.WriteFile(src, bytes.Repeat([]byte{0xCA, 0xFE}, 64), 0o640)

	got := svc.Finish(context.Background(), upload.FinishInput{
		UserID: "u-alex", UploadID: "up-1",
		OriginalName: "x.mkv", SanitizedName: "x.mkv",
		Size: 1024, SourcePath: src,
	})
	if got.Outcome != "rejected" {
		t.Fatalf("outcome = %s, want rejected", got.Outcome)
	}
	if users.used != 0 {
		t.Errorf("bytes not released on rejection: used=%d", users.used)
	}
	if audit.lastOutcome() != "rejected" {
		t.Errorf("audit outcome = %q", audit.lastOutcome())
	}
	if bus.terminalType() != event.UploadError {
		t.Errorf("terminal = %s, want UploadError", bus.terminalType())
	}
}

func TestService_Finish_RejectsTooShort(t *testing.T) {
	svc, _, _, _, stagingRoot, _ := newServiceFixture(t)
	_, _ = svc.PreCreate(context.Background(), upload.PreCreateInput{
		UserID: "u-alex", UploadID: "up-1", OriginalName: "x.mkv", Size: 1024,
	})
	src := filepath.Join(stagingRoot, "u-alex", "up-1", "x.mkv")
	writeFakeMKV(t, src, 1024)

	// Re-create the service with a fake prober that reports a tiny
	// duration so the MinDurationMs gate trips.
	st, _ := upload.NewStagingDir(stagingRoot)
	users := &fakeUserStore{
		user: &authmodel.User{ID: "u-alex", IsActive: true, CanUpload: true},
		maxRes: 1 << 40, used: 1024,
	}
	svc = upload.NewService(upload.DefaultConfig(), st, users, &fakeAuditStore{}, &captureBus{},
		upload.NewLibraryPicker(&fakeLibraryStore{
			libs: []*librarymodel.Library{{ID: "l", ContentType: "movies", Paths: []string{t.TempDir()}}},
		}),
		&fakeProber{durationMs: 500}, nil, slog.Default())

	got := svc.Finish(context.Background(), upload.FinishInput{
		UserID: "u-alex", UploadID: "up-1",
		OriginalName: "x.mkv", SanitizedName: "x.mkv",
		Size: 1024, SourcePath: src,
	})
	if got.Outcome != "rejected" {
		t.Fatalf("outcome = %s (msg=%s)", got.Outcome, got.ErrorMessage)
	}
}

func TestService_Finish_RejectsProbeFailure(t *testing.T) {
	stagingRoot := filepath.Join(t.TempDir(), "staging")
	st, _ := upload.NewStagingDir(stagingRoot)
	users := &fakeUserStore{
		user: &authmodel.User{ID: "u-alex", IsActive: true, CanUpload: true},
		maxRes: 1 << 40, used: 1024,
	}
	libDir := t.TempDir()
	svc := upload.NewService(upload.DefaultConfig(), st, users, &fakeAuditStore{}, &captureBus{},
		upload.NewLibraryPicker(&fakeLibraryStore{
			libs: []*librarymodel.Library{{ID: "l", ContentType: "movies", Paths: []string{libDir}}},
		}),
		&fakeProber{err: fmt.Errorf("ffprobe exit 1")}, nil, slog.Default())

	src := filepath.Join(stagingRoot, "u-alex", "up-1", "x.mkv")
	writeFakeMKV(t, src, 1024)

	got := svc.Finish(context.Background(), upload.FinishInput{
		UserID: "u-alex", UploadID: "up-1",
		OriginalName: "x.mkv", SanitizedName: "x.mkv",
		Size: 1024, SourcePath: src,
	})
	if got.Outcome != "rejected" {
		t.Errorf("outcome = %s", got.Outcome)
	}
	if users.used != 0 {
		t.Errorf("bytes not released: used=%d", users.used)
	}
}

func TestService_Finish_RejectsNoLibrary(t *testing.T) {
	stagingRoot := filepath.Join(t.TempDir(), "staging")
	st, _ := upload.NewStagingDir(stagingRoot)
	users := &fakeUserStore{
		user: &authmodel.User{ID: "u-alex", IsActive: true, CanUpload: true},
		maxRes: 1 << 40, used: 1024,
	}
	svc := upload.NewService(upload.DefaultConfig(), st, users, &fakeAuditStore{}, &captureBus{},
		upload.NewLibraryPicker(&fakeLibraryStore{libs: nil}), // user has nothing
		&fakeProber{durationMs: 60_000}, nil, slog.Default())

	src := filepath.Join(stagingRoot, "u-alex", "up-1", "x.mkv")
	writeFakeMKV(t, src, 1024)

	got := svc.Finish(context.Background(), upload.FinishInput{
		UserID: "u-alex", UploadID: "up-1",
		OriginalName: "x.mkv", SanitizedName: "x.mkv",
		Size: 1024, SourcePath: src,
	})
	if got.Outcome != "rejected" {
		t.Errorf("outcome = %s (msg=%s)", got.Outcome, got.ErrorMessage)
	}
}

// ─── Finish: name collision ─────────────────────────────────────────

func TestService_Finish_NameCollisionSuffixes(t *testing.T) {
	svc, _, _, _, stagingRoot, libDir := newServiceFixture(t)

	// Pre-existing file at the target path.
	existing := filepath.Join(libDir, "Movie.mkv")
	if err := os.WriteFile(existing, []byte("prev"), 0o640); err != nil {
		t.Fatal(err)
	}

	_, _ = svc.PreCreate(context.Background(), upload.PreCreateInput{
		UserID: "u-alex", UploadID: "up-1", OriginalName: "Movie.mkv", Size: 1024,
	})
	src := filepath.Join(stagingRoot, "u-alex", "up-1", "Movie.mkv")
	writeFakeMKV(t, src, 1024)

	got := svc.Finish(context.Background(), upload.FinishInput{
		UserID: "u-alex", UploadID: "up-1",
		OriginalName: "Movie.mkv", SanitizedName: "Movie.mkv",
		Size: 1024, SourcePath: src,
	})
	if got.Outcome != "accepted" {
		t.Fatalf("outcome = %s", got.Outcome)
	}
	wantFinal := filepath.Join(libDir, "Movie-1.mkv")
	if got.FinalPath != wantFinal {
		t.Errorf("final = %s, want %s (suffix not added)", got.FinalPath, wantFinal)
	}
	// El fichero pre-existente sigue intacto.
	prev, _ := os.ReadFile(existing)
	if string(prev) != "prev" {
		t.Errorf("pre-existing file pisado: %q", prev)
	}
}

// TestService_Finish_OverwriteFlag verifica que con Overwrite=true el
// pipeline PISA el fichero destino en vez de añadir sufijo -NNN. Es el
// camino del modal de colisión cuando el operador elige "Sobrescribir".
func TestService_Finish_OverwriteFlag(t *testing.T) {
	svc, _, _, _, stagingRoot, libDir := newServiceFixture(t)

	existing := filepath.Join(libDir, "Movie.mkv")
	if err := os.WriteFile(existing, []byte("prev"), 0o640); err != nil {
		t.Fatal(err)
	}

	_, _ = svc.PreCreate(context.Background(), upload.PreCreateInput{
		UserID: "u-alex", UploadID: "up-1", OriginalName: "Movie.mkv", Size: 1024,
	})
	src := filepath.Join(stagingRoot, "u-alex", "up-1", "Movie.mkv")
	writeFakeMKV(t, src, 1024)

	got := svc.Finish(context.Background(), upload.FinishInput{
		UserID: "u-alex", UploadID: "up-1",
		OriginalName: "Movie.mkv", SanitizedName: "Movie.mkv",
		Overwrite:    true,
		Size: 1024, SourcePath: src,
	})
	if got.Outcome != "accepted" {
		t.Fatalf("outcome = %s msg=%s", got.Outcome, got.ErrorMessage)
	}
	// Final path: el mismo Movie.mkv original — sin sufijo.
	want := filepath.Join(libDir, "Movie.mkv")
	if got.FinalPath != want {
		t.Errorf("final = %s, want %s", got.FinalPath, want)
	}
	// El contenido nuevo (cuerpo del fake MKV) reemplaza al "prev".
	body, _ := os.ReadFile(want)
	if string(body) == "prev" {
		t.Errorf("overwrite did NOT replace content: %q", body)
	}
}

// ─── Aborted ────────────────────────────────────────────────────────

func TestService_Aborted_ReleasesQuotaAndAudits(t *testing.T) {
	svc, users, audit, bus, stagingRoot, _ := newServiceFixture(t)
	_, _ = svc.PreCreate(context.Background(), upload.PreCreateInput{
		UserID: "u-alex", UploadID: "up-1", OriginalName: "x.mkv", Size: 1024,
	})
	// Crear directorio del upload (RemoveUpload no debe romper si ya
	// hay algo dentro).
	src := filepath.Join(stagingRoot, "u-alex", "up-1", "x.mkv")
	writeFakeMKV(t, src, 512)

	svc.Aborted(context.Background(), upload.FinishInput{
		UserID: "u-alex", UploadID: "up-1",
		OriginalName: "x.mkv", Size: 1024,
	})
	if users.used != 0 {
		t.Errorf("bytes not released on abort: used=%d", users.used)
	}
	if audit.lastOutcome() != "aborted" {
		t.Errorf("audit outcome = %q", audit.lastOutcome())
	}
	if bus.terminalType() != event.UploadError {
		t.Errorf("terminal = %s", bus.terminalType())
	}
}

// ─── helpers ────────────────────────────────────────────────────────

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
