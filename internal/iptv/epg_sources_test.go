package iptv

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/testutil"
)

// ─── Catalog ─────────────────────────────────────────────────────

func TestPublicEPGSources_HasEntries(t *testing.T) {
	t.Parallel()
	sources := PublicEPGSources()
	if len(sources) == 0 {
		t.Fatal("catalog is empty")
	}
	for _, src := range sources {
		if src.ID == "" || src.Name == "" || src.URL == "" {
			t.Errorf("incomplete catalog entry: %+v", src)
		}
	}
}

func TestFindEPGSource(t *testing.T) {
	t.Parallel()
	if _, ok := FindEPGSource("davidmuma-guiatv"); !ok {
		t.Error("davidmuma-guiatv should exist in the catalog")
	}
	if _, ok := FindEPGSource("not-a-real-source"); ok {
		t.Error("unknown id should return ok=false")
	}
}

// TestPublicEPGSources_NoKnownBrokenURLs documents the URLs we already
// know return 404 at upstream and that must never be added back to the
// catalog. Previously we shipped guiaiptvmovistar.xml, tdtsat.xml and
// the epg.pw endpoints — all confirmed 404 / 403 under real usage and
// removed in the cleanup commit. This test keeps a regression guard so
// a future accidental revert is caught at build time.
func TestPublicEPGSources_NoKnownBrokenURLs(t *testing.T) {
	t.Parallel()
	banned := []string{
		"guiaiptvmovistar.xml",
		"tdtsat.xml",
		"epg.pw/api/epg.xml.gz?lang=",
	}
	for _, src := range PublicEPGSources() {
		for _, bad := range banned {
			if strings.Contains(src.URL, bad) {
				t.Errorf("catalog entry %q still uses banned URL fragment %q: %s",
					src.ID, bad, src.URL)
			}
		}
	}
}

// ─── Service integration ─────────────────────────────────────────

func newEPGTestService(t *testing.T) (*Service, *db.Repositories, string) {
	t.Helper()
	unblockLoopback(t)

	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(database)
	ctx := context.Background()

	now := time.Now()
	libID := "lib-epg-sources"
	if err := repos.Libraries.Create(ctx, &db.Library{
		ID: libID, Name: "LiveTV", ContentType: "livetv", ScanMode: "manual",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed library: %v", err)
	}
	// Seed a channel the XMLTV fixtures will match against by tvg-id.
	if err := repos.Channels.Create(ctx, &db.Channel{
		ID: "ch-la1", LibraryID: libID, Name: "La 1 HD", Number: 1,
		StreamURL: "http://example/la1.m3u8", TvgID: "la1",
		IsActive: true, AddedAt: now,
	}); err != nil {
		t.Fatalf("seed channel la1: %v", err)
	}
	if err := repos.Channels.Create(ctx, &db.Channel{
		ID: "ch-a3", LibraryID: libID, Name: "Antena 3", Number: 2,
		StreamURL: "http://example/a3.m3u8", TvgID: "a3",
		IsActive: true, AddedAt: now,
	}); err != nil {
		t.Fatalf("seed channel a3: %v", err)
	}

	svc := NewService(repos.Channels, repos.EPGPrograms, repos.Libraries,
		repos.ChannelFavorites, repos.LibraryEPGSources, repos.ChannelOverrides,
		slog.New(slog.NewTextHandler(new(discard), nil)))
	return svc, repos, libID
}

// fakeEPGServer serves a minimal XMLTV document. `programs` is a map
// of tvg-id → program title so each fixture can claim or skip specific
// channels deterministically.
func fakeEPGServer(t *testing.T, programs map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		body := `<?xml version="1.0"?><tv>`
		for tvgID := range programs {
			body += fmt.Sprintf(`<channel id=%q><display-name>%s</display-name></channel>`, tvgID, tvgID)
		}
		for tvgID, title := range programs {
			body += fmt.Sprintf(
				`<programme channel=%q start="20260424120000 +0000" stop="20260424130000 +0000">`+
					`<title>%s</title></programme>`,
				tvgID, title)
		}
		body += `</tv>`
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRefreshEPG_SingleSource_ReplacesPrograms(t *testing.T) {
	svc, repos, libID := newEPGTestService(t)
	ctx := context.Background()

	srv := fakeEPGServer(t, map[string]string{
		"la1": "Telediario",
		"a3":  "Antena 3 Noticias",
	})
	if _, err := svc.AddEPGSource(ctx, libID, "", srv.URL); err != nil {
		t.Fatalf("add source: %v", err)
	}

	total, err := svc.RefreshEPG(ctx, libID)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected 2 programs, got %d", total)
	}

	sched, _ := repos.EPGPrograms.Schedule(ctx, "ch-la1",
		time.Date(2026, 4, 24, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 24, 23, 59, 0, 0, time.UTC))
	if len(sched) != 1 || sched[0].Title != "Telediario" {
		t.Errorf("la1 schedule: %v", sched)
	}
}

// Priority 0 covers La 1, priority 1 covers Antena 3 — merge should
// yield both channels populated, each from the source that matched it.
func TestRefreshEPG_MultiSource_FillsGaps(t *testing.T) {
	svc, repos, libID := newEPGTestService(t)
	ctx := context.Background()

	primary := fakeEPGServer(t, map[string]string{"la1": "From Primary"})
	fallback := fakeEPGServer(t, map[string]string{"a3": "From Fallback"})

	if _, err := svc.AddEPGSource(ctx, libID, "", primary.URL); err != nil {
		t.Fatalf("add primary: %v", err)
	}
	if _, err := svc.AddEPGSource(ctx, libID, "", fallback.URL); err != nil {
		t.Fatalf("add fallback: %v", err)
	}

	total, err := svc.RefreshEPG(ctx, libID)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected 2 programs (one per channel), got %d", total)
	}

	la1Sched, _ := repos.EPGPrograms.Schedule(ctx, "ch-la1",
		time.Date(2026, 4, 24, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 24, 23, 59, 0, 0, time.UTC))
	if len(la1Sched) != 1 || la1Sched[0].Title != "From Primary" {
		t.Errorf("la1 should come from primary: %v", la1Sched)
	}
	a3Sched, _ := repos.EPGPrograms.Schedule(ctx, "ch-a3",
		time.Date(2026, 4, 24, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 24, 23, 59, 0, 0, time.UTC))
	if len(a3Sched) != 1 || a3Sched[0].Title != "From Fallback" {
		t.Errorf("a3 should come from fallback: %v", a3Sched)
	}
}

// Both sources claim the same channel — the higher-priority one must
// win. Prevents the fallback from stomping an authoritative guide.
func TestRefreshEPG_MultiSource_PriorityWins(t *testing.T) {
	svc, repos, libID := newEPGTestService(t)
	ctx := context.Background()

	primary := fakeEPGServer(t, map[string]string{"la1": "Priority 0 version"})
	fallback := fakeEPGServer(t, map[string]string{"la1": "Priority 1 version"})

	if _, err := svc.AddEPGSource(ctx, libID, "", primary.URL); err != nil {
		t.Fatalf("add primary: %v", err)
	}
	if _, err := svc.AddEPGSource(ctx, libID, "", fallback.URL); err != nil {
		t.Fatalf("add fallback: %v", err)
	}

	if _, err := svc.RefreshEPG(ctx, libID); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	sched, _ := repos.EPGPrograms.Schedule(ctx, "ch-la1",
		time.Date(2026, 4, 24, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 24, 23, 59, 0, 0, time.UTC))
	if len(sched) != 1 || sched[0].Title != "Priority 0 version" {
		t.Errorf("expected priority-0 version to win, got: %v", sched)
	}
}

// When a source 404s the refresh must not abort — the fallback still
// runs and the broken source keeps its status recorded.
func TestRefreshEPG_OneSourceFails_OthersStillRun(t *testing.T) {
	svc, repos, libID := newEPGTestService(t)
	ctx := context.Background()

	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "gone", http.StatusGone)
	}))
	defer broken.Close()
	working := fakeEPGServer(t, map[string]string{"la1": "Show"})

	brokenSrc, err := svc.AddEPGSource(ctx, libID, "", broken.URL)
	if err != nil {
		t.Fatalf("add broken: %v", err)
	}
	workingSrc, err := svc.AddEPGSource(ctx, libID, "", working.URL)
	if err != nil {
		t.Fatalf("add working: %v", err)
	}

	total, err := svc.RefreshEPG(ctx, libID)
	if err != nil {
		t.Fatalf("refresh should succeed if any source works: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected 1 program from the working source, got %d", total)
	}

	updated, _ := repos.LibraryEPGSources.GetByID(ctx, brokenSrc.ID)
	if updated.LastStatus != "error" || updated.LastError == "" {
		t.Errorf("broken source status not recorded: %+v", updated)
	}
	okSrc, _ := repos.LibraryEPGSources.GetByID(ctx, workingSrc.ID)
	if okSrc.LastStatus != "ok" || okSrc.LastProgramCount != 1 {
		t.Errorf("working source status not recorded: %+v", okSrc)
	}
}

// Every source fails → refresh returns an error (vs. silently ok with
// zero programs). Gives the admin UI a clear "all broken" signal.
func TestRefreshEPG_AllSourcesFail_ReturnsError(t *testing.T) {
	svc, _, libID := newEPGTestService(t)
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	if _, err := svc.AddEPGSource(ctx, libID, "", srv.URL); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := svc.RefreshEPG(ctx, libID); err == nil {
		t.Fatal("expected error when every source fails")
	}
}

// Adding a catalog source by id pulls the URL from PublicEPGSources
// (authoritative) rather than trusting whatever the caller pasted.
func TestAddEPGSource_CatalogIDWinsOverURL(t *testing.T) {
	svc, _, libID := newEPGTestService(t)
	ctx := context.Background()

	src, err := svc.AddEPGSource(ctx, libID, "davidmuma-guiatv", "http://stale.example/x.xml")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	entry, _ := FindEPGSource("davidmuma-guiatv")
	if src.URL != entry.URL {
		t.Errorf("URL = %q, want catalog URL %q", src.URL, entry.URL)
	}
	if src.CatalogID != "davidmuma-guiatv" {
		t.Errorf("CatalogID = %q, want davidmuma-guiatv", src.CatalogID)
	}
}

func TestAddEPGSource_UnknownCatalogID_Rejects(t *testing.T) {
	svc, _, libID := newEPGTestService(t)
	ctx := context.Background()
	if _, err := svc.AddEPGSource(ctx, libID, "not-a-real-id", ""); err == nil {
		t.Fatal("expected error on unknown catalog id")
	}
}

func TestAddEPGSource_NoCatalogNoURL_Rejects(t *testing.T) {
	svc, _, libID := newEPGTestService(t)
	ctx := context.Background()
	if _, err := svc.AddEPGSource(ctx, libID, "", ""); err == nil {
		t.Fatal("expected error when neither catalog_id nor url is provided")
	}
}

// ReorderEPGSources must reject a list that adds, drops, or includes
// ids from another library — any drift would produce partial writes.
func TestReorderEPGSources_RejectsMismatchedList(t *testing.T) {
	svc, _, libID := newEPGTestService(t)
	ctx := context.Background()

	srv := fakeEPGServer(t, map[string]string{"la1": "X"})
	a, _ := svc.AddEPGSource(ctx, libID, "", srv.URL)
	// Add a second source via a different URL to satisfy the UNIQUE constraint.
	srv2 := fakeEPGServer(t, map[string]string{"a3": "Y"})
	b, _ := svc.AddEPGSource(ctx, libID, "", srv2.URL)

	if err := svc.ReorderEPGSources(ctx, libID, []string{a.ID}); err == nil {
		t.Error("expected error when reorder list has fewer ids than configured")
	}
	if err := svc.ReorderEPGSources(ctx, libID, []string{a.ID, b.ID, "ghost"}); err == nil {
		t.Error("expected error when reorder list includes unknown id")
	}

	// Valid reorder should succeed and flip priorities.
	if err := svc.ReorderEPGSources(ctx, libID, []string{b.ID, a.ID}); err != nil {
		t.Fatalf("valid reorder: %v", err)
	}
	list, _ := svc.ListEPGSources(ctx, libID)
	if list[0].ID != b.ID || list[1].ID != a.ID {
		t.Errorf("order after reorder: %s, %s; want %s, %s",
			list[0].ID, list[1].ID, b.ID, a.ID)
	}
}

// Back-compat: a library upgraded from pre-007 still has `epg_url`
// set and no rows in library_epg_sources. The refresher must still
// work without any admin intervention.
func TestRefreshEPG_LegacyEPGURLFallback(t *testing.T) {
	unblockLoopback(t)
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(database)
	ctx := context.Background()

	srv := fakeEPGServer(t, map[string]string{"la1": "Legacy show"})

	now := time.Now()
	libID := "lib-legacy"
	// Insert directly with epg_url populated, bypassing the multi-source
	// path to reproduce a pre-upgrade library exactly.
	if err := repos.Libraries.Create(ctx, &db.Library{
		ID: libID, Name: "L", ContentType: "livetv", ScanMode: "manual",
		EPGURL: srv.URL, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_ = repos.Channels.Create(ctx, &db.Channel{
		ID: "ch-la1", LibraryID: libID, Name: "La 1", Number: 1,
		StreamURL: "http://x", TvgID: "la1", IsActive: true, AddedAt: now,
	})

	svc := NewService(repos.Channels, repos.EPGPrograms, repos.Libraries,
		repos.ChannelFavorites, repos.LibraryEPGSources, repos.ChannelOverrides,
		slog.New(slog.NewTextHandler(new(discard), nil)))

	total, err := svc.RefreshEPG(ctx, libID)
	if err != nil {
		t.Fatalf("legacy refresh: %v", err)
	}
	if total != 1 {
		t.Errorf("expected 1 program from legacy URL, got %d", total)
	}
}
