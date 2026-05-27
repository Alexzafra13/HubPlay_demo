package library

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"hubplay/internal/clock"
	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/db"
	"hubplay/internal/domain"
	"hubplay/internal/scanner"

	"github.com/google/uuid"
)

// validateHTTPURL rejects empty / non-http(s) URLs so we don't persist
// payloads like `file:///etc/passwd`, `gopher://...` or plain typos.
// The downstream fetchers (M3U / EPG) already enforce this when they
// run, but checking at the create boundary turns a deferred preflight
// error into an immediate 400 the form can render inline.
func validateHTTPURL(raw string) bool {
	s := strings.TrimSpace(raw)
	if s == "" {
		return false
	}
	parsed, err := url.Parse(s)
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	if parsed.Host == "" {
		return false
	}
	return true
}

// normaliseLanguageFilter trims, lower-cases and de-duplicates the
// language codes coming from a CreateRequest / UpdateRequest payload,
// then joins them with "," for storage in libraries.language_filter.
// Non-ISO-shaped tokens (anything other than 2–3 letters) are dropped
// — keeps the column free of typos and "español" -style human inputs
// while still accepting the rare 3-letter ISO 639-2 code.
func normaliseLanguageFilter(codes []string) string {
	if len(codes) == 0 {
		return ""
	}
	seen := make(map[string]struct{}, len(codes))
	out := make([]string, 0, len(codes))
	for _, raw := range codes {
		c := strings.ToLower(strings.TrimSpace(raw))
		if c == "" {
			continue
		}
		if l := len(c); l < 2 || l > 3 {
			continue
		}
		for _, r := range c {
			if r < 'a' || r > 'z' {
				c = ""
				break
			}
		}
		if c == "" {
			continue
		}
		if _, dup := seen[c]; dup {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	return strings.Join(out, ",")
}

// Service es el facade que ensambla los sub-services tras el cierre
// del olor Z del audit 2026-05-14 (god-service de 27 métodos en 6
// responsabilidades). El struct mantiene los fields core que CRUD +
// scan + lifecycle usan; ACL e item queries se delegan vía embedding
// a sus sub-services dedicados.
//
// Sub-services embedded (method promotion intra-paquete los expone
// como si fueran métodos del Service, así handlers e interfaces
// LibraryService consumer-side no cambian):
//
//	AccessControl  → ListForUser, UserHasAccess, GrantAccess,
//	                 RevokeAccess, ListAccessByUser, ReplaceAccess
//	ItemQueries    → ListItems, ListGenres, GetItem, GetItemChildren,
//	                 GetItemChildCounts, GetItemStreams,
//	                 GetItemImages, LatestItems, LatestSeriesByActivity,
//	                 ItemCount
//
// Métodos que se mantienen en el facade directamente (core CRUD +
// scan + lifecycle):
//
//	Create, CreatePersonalIPTV, GetByID, List, Update, Delete,
//	Scan, ScanSync, IsScanning, ScanAll, Shutdown
type Service struct {
	*AccessControl
	*ItemQueries

	libraries *db.LibraryRepository
	items     *db.ItemRepository
	channels  *db.ChannelRepository
	scanner   *scanner.Scanner
	clock     clock.Clock
	logger    *slog.Logger

	// Track active scans to prevent concurrent scans of the same library
	mu       sync.Mutex
	scanning map[string]bool

	// Background-goroutine lifecycle. bgCtx is cancelled by Shutdown, bgWG
	// waits for every auto-scan / manual scan goroutine to unwind. Without
	// this, a goroutine started by Create would keep hitting the (already
	// closed) DB after the service owner tore down — the exact race the
	// CI Test Backend was flaking on.
	bgCtx    context.Context
	bgCancel context.CancelFunc
	bgWG     sync.WaitGroup
}

func NewService(
	libraries *db.LibraryRepository,
	items *db.ItemRepository,
	streams *db.MediaStreamRepository,
	images *db.ImageRepository,
	channels *db.ChannelRepository,
	itemValues *db.ItemValueRepository,
	scnr *scanner.Scanner,
	clk clock.Clock,
	logger *slog.Logger,
) *Service {
	if clk == nil {
		clk = clock.New()
	}
	libLogger := logger.With("module", "library")
	bgCtx, bgCancel := context.WithCancel(context.Background())
	return &Service{
		AccessControl: newAccessControl(libraries, libLogger),
		ItemQueries:   newItemQueries(items, streams, images, itemValues, libraries, channels, libLogger),

		libraries: libraries,
		items:     items,
		channels:  channels,
		scanner:   scnr,
		clock:     clk,
		logger:    libLogger,
		scanning:  make(map[string]bool),
		bgCtx:     bgCtx,
		bgCancel:  bgCancel,
	}
}

// Shutdown cancels any in-flight background scans and blocks until every
// goroutine started by this service has returned. Safe to call multiple
// times. Call from the owning main.go before closing the DB handle, and
// from tests via t.Cleanup to stop goroutines leaking across tests.
func (s *Service) Shutdown() {
	s.bgCancel()
	s.bgWG.Wait()
}

type CreateRequest struct {
	Name        string   `json:"name"`
	ContentType string   `json:"content_type"` // movies, shows, music, livetv
	Paths       []string `json:"paths"`
	ScanMode    string   `json:"scan_mode"` // auto, manual

	// IPTV-only (content_type == "livetv"). `M3UURL` is required for livetv;
	// `EPGURL` is optional — if omitted, RefreshM3U will try to auto-discover
	// an XMLTV URL from the playlist's `#EXTM3U url-tvg=...` header.
	M3UURL string `json:"m3u_url,omitempty"`
	EPGURL string `json:"epg_url,omitempty"`

	// LanguageFilter is the set of ISO 639-1 lowercase codes (e.g.
	// ["es", "en"]) the M3U import should keep. Empty / nil means
	// no filter — every channel imports. See iptv.MatchesLanguageFilter
	// for the matching rules. Persisted as comma-separated.
	LanguageFilter []string `json:"language_filter,omitempty"`

	// TLSInsecure, when true, makes the M3U / EPG fetcher skip TLS
	// certificate verification for THIS library's HTTPS URLs. Used
	// for IPTV providers that ship expired Let's Encrypt or self-
	// signed certs. See librarymodel.Library.TLSInsecure for the security
	// caveat.
	TLSInsecure bool `json:"tls_insecure,omitempty"`
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (*librarymodel.Library, error) {
	if err := validateCreateRequest(req); err != nil {
		return nil, err
	}

	if req.ScanMode == "" {
		req.ScanMode = "auto"
	}

	now := s.clock.Now()
	lib := &librarymodel.Library{
		ID:             uuid.NewString(),
		Name:           req.Name,
		ContentType:    req.ContentType,
		ScanMode:       req.ScanMode,
		ScanInterval:   "6h",
		CreatedAt:      now,
		UpdatedAt:      now,
		Paths:          req.Paths,
		M3UURL:         req.M3UURL,
		EPGURL:         req.EPGURL,
		LanguageFilter: normaliseLanguageFilter(req.LanguageFilter),
		TLSInsecure:    req.TLSInsecure,
	}

	if err := s.libraries.Create(ctx, lib); err != nil {
		return nil, fmt.Errorf("creating library: %w", err)
	}

	s.logger.Info("library created", "library_id", lib.ID, "name", lib.Name, "type", lib.ContentType)

	// Auto-scan the new library (like Jellyfin does on library creation).
	// Inherits bgCtx so Shutdown can cancel in-flight scans; the WaitGroup
	// lets Shutdown wait for the goroutine before returning.
	//
	// livetv libraries skip this — there are no filesystem paths to scan.
	// The admin UI triggers the first `iptv/refresh-m3u` right after creation
	// to populate channels, so nothing is lost by not scanning here.
	if lib.ScanMode != "manual" && lib.ContentType != "livetv" {
		log := s.logger.With("library_id", lib.ID)
		s.bgWG.Add(1)
		go func() {
			defer s.bgWG.Done()
			scanCtx, cancel := context.WithTimeout(s.bgCtx, 30*time.Minute)
			defer cancel()
			if _, err := s.scanner.ScanLibrary(scanCtx, lib); err != nil {
				log.Error("auto-scan after creation failed", "error", err)
			}
		}()
	}

	return lib, nil
}

// CreatePersonalIPTV creates a livetv library AND grants access to
// `ownerUserID` in one transaction. Used by the admin "personal IPTV
// list" shortcut so the operator can hand a user their own M3U without
// the two-step "create library → tick checkbox in users matrix"
// dance. The library is invisible to every other non-admin user
// because `library_access` is opt-in (INNER JOIN in ListForUser).
//
// Forces `content_type = "livetv"` regardless of what the caller
// passed; `m3u_url` is still required and validated by the shared
// validator. `paths` is ignored (livetv has no filesystem paths).
//
// `ownerUserID` MUST be a top-level user; the handler resolves
// profile ids to their parent before reaching here.
func (s *Service) CreatePersonalIPTV(ctx context.Context, ownerUserID string, req CreateRequest) (*librarymodel.Library, error) {
	req.ContentType = "livetv"
	req.Paths = nil
	req.ScanMode = "manual"
	if err := validateCreateRequest(req); err != nil {
		return nil, err
	}

	now := s.clock.Now()
	lib := &librarymodel.Library{
		ID:             uuid.NewString(),
		Name:           req.Name,
		ContentType:    req.ContentType,
		ScanMode:       req.ScanMode,
		ScanInterval:   "6h",
		CreatedAt:      now,
		UpdatedAt:      now,
		M3UURL:         req.M3UURL,
		EPGURL:         req.EPGURL,
		LanguageFilter: normaliseLanguageFilter(req.LanguageFilter),
		TLSInsecure:    req.TLSInsecure,
	}

	if err := s.libraries.CreateWithGrant(ctx, lib, ownerUserID); err != nil {
		return nil, fmt.Errorf("creating personal iptv library: %w", err)
	}

	s.logger.Info("personal iptv library created",
		"library_id", lib.ID, "name", lib.Name, "owner_user_id", ownerUserID)
	return lib, nil
}

func (s *Service) GetByID(ctx context.Context, id string) (*librarymodel.Library, error) {
	return s.libraries.GetByID(ctx, id)
}

func (s *Service) List(ctx context.Context) ([]*librarymodel.Library, error) {
	return s.libraries.List(ctx)
}

// (Operaciones ACL — ListForUser, UserHasAccess, GrantAccess,
// RevokeAccess, ListAccessByUser, ReplaceAccess — viven en
// access_control.go. El embedding de *AccessControl en Service
// las promueve para que callers externos las llamen via `s.Method(...)`
// sin cambios.)

type UpdateRequest struct {
	Name        string   `json:"name"`
	ContentType string   `json:"content_type"`
	Paths       []string `json:"paths"`
	ScanMode    string   `json:"scan_mode"`

	// IPTV-only. Pointer so "omitted" (leave existing) is distinguishable
	// from "empty string" (clear the value). json.Decode leaves these nil
	// when the key isn't in the payload, matching that semantic.
	M3UURL *string `json:"m3u_url,omitempty"`
	EPGURL *string `json:"epg_url,omitempty"`

	// LanguageFilter follows the same "nil = leave as-is, present-and-
	// empty = clear all" semantic. *[]string lets a PATCH request opt
	// in or out without forcing the caller to round-trip the existing
	// value.
	LanguageFilter *[]string `json:"language_filter,omitempty"`

	// TLSInsecure: nil = leave as-is, true / false = explicit toggle.
	TLSInsecure *bool `json:"tls_insecure,omitempty"`
}

func (s *Service) Update(ctx context.Context, id string, req UpdateRequest) (*librarymodel.Library, error) {
	lib, err := s.libraries.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if req.Name != "" {
		lib.Name = req.Name
	}
	if req.ContentType != "" {
		lib.ContentType = req.ContentType
	}
	if req.Paths != nil {
		lib.Paths = req.Paths
	}
	if req.ScanMode != "" {
		lib.ScanMode = req.ScanMode
	}
	if req.M3UURL != nil {
		if *req.M3UURL != "" && !validateHTTPURL(*req.M3UURL) {
			return nil, domain.NewValidationError(map[string]string{
				"m3u_url": "must be a valid http:// or https:// URL",
			})
		}
		lib.M3UURL = *req.M3UURL
	}
	if req.EPGURL != nil {
		if *req.EPGURL != "" && !validateHTTPURL(*req.EPGURL) {
			return nil, domain.NewValidationError(map[string]string{
				"epg_url": "must be a valid http:// or https:// URL",
			})
		}
		lib.EPGURL = *req.EPGURL
	}
	if req.LanguageFilter != nil {
		lib.LanguageFilter = normaliseLanguageFilter(*req.LanguageFilter)
	}
	if req.TLSInsecure != nil {
		lib.TLSInsecure = *req.TLSInsecure
	}
	lib.UpdatedAt = s.clock.Now()

	if err := s.libraries.Update(ctx, lib); err != nil {
		return nil, fmt.Errorf("updating library: %w", err)
	}

	s.logger.Info("library updated", "library_id", id, "name", lib.Name)
	return lib, nil
}

func (s *Service) Delete(ctx context.Context, id string) error {
	if err := s.libraries.Delete(ctx, id); err != nil {
		return fmt.Errorf("deleting library: %w", err)
	}
	s.logger.Info("library deleted", "library_id", id)
	return nil
}

// Scan triggers an async scan for a library. Returns immediately.
// If refreshMetadata is true, all items will have their metadata and images
// re-fetched from providers after the scan completes.
func (s *Service) Scan(ctx context.Context, id string, refreshMetadata ...bool) error {
	lib, err := s.libraries.GetByID(ctx, id)
	if err != nil {
		return err
	}

	s.mu.Lock()
	if s.scanning[id] {
		s.mu.Unlock()
		return fmt.Errorf("library %s: %w", id, domain.ErrConflict)
	}
	s.scanning[id] = true
	s.mu.Unlock()

	refresh := len(refreshMetadata) > 0 && refreshMetadata[0]
	log := s.logger.With("library_id", id)

	s.bgWG.Add(1)
	go func() {
		defer s.bgWG.Done()
		defer func() {
			s.mu.Lock()
			delete(s.scanning, id)
			s.mu.Unlock()
		}()

		scanCtx, cancel := context.WithTimeout(s.bgCtx, 30*time.Minute)
		defer cancel()
		if _, err := s.scanner.ScanLibrary(scanCtx, lib); err != nil {
			log.Error("scan failed", "error", err)
		}

		if refresh {
			if err := s.scanner.RefreshMetadata(scanCtx, lib); err != nil {
				log.Error("metadata refresh failed", "error", err)
			}
		}
	}()

	return nil
}

// ScanSync runs a scan synchronously (useful for tests).
func (s *Service) ScanSync(ctx context.Context, id string) (*scanner.ScanResult, error) {
	lib, err := s.libraries.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.scanner.ScanLibrary(ctx, lib)
}

// IsScanning returns whether a library is currently being scanned.
func (s *Service) IsScanning(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.scanning[id]
}

// (Queries de items — ListItems, ListGenres, GetItem, GetItemChildren,
// GetItemChildCounts, GetItemStreams, GetItemImages, LatestItems,
// LatestSeriesByActivity, ItemCount — viven en item_queries.go.
// El embedding de *ItemQueries en Service las promueve para
// callers externos.)

// ScanAll triggers an async scan for all libraries with auto scan mode.
func (s *Service) ScanAll(ctx context.Context) {
	libs, err := s.libraries.List(ctx)
	if err != nil {
		s.logger.Error("failed to list libraries for scan-all", "error", err)
		return
	}
	for _, lib := range libs {
		if lib.ScanMode == "manual" {
			continue
		}
		if err := s.Scan(ctx, lib.ID); err != nil {
			s.logger.Warn("scan-all: skipping library", "library_id", lib.ID, "name", lib.Name, "error", err)
		}
	}
}

func validateCreateRequest(req CreateRequest) error {
	fields := make(map[string]string)
	if req.Name == "" {
		fields["name"] = "is required"
	}
	// `livetv` is accepted here so the generic Create endpoint can be used
	// for IPTV libraries (public country playlists via iptv-org or fully
	// custom M3U URLs). Validation differs: livetv requires `m3u_url`;
	// everything else requires at least one filesystem path.
	validTypes := map[string]bool{
		"movies": true,
		"shows":  true,
		"music":  true,
		"livetv": true,
	}
	if !validTypes[req.ContentType] {
		fields["content_type"] = "must be movies, shows, music, or livetv"
	}
	if req.ContentType == "livetv" {
		if req.M3UURL == "" {
			fields["m3u_url"] = "is required for livetv libraries"
		} else if !validateHTTPURL(req.M3UURL) {
			fields["m3u_url"] = "must be a valid http:// or https:// URL"
		}
		if strings.TrimSpace(req.EPGURL) != "" && !validateHTTPURL(req.EPGURL) {
			fields["epg_url"] = "must be a valid http:// or https:// URL"
		}
	} else if len(req.Paths) == 0 {
		fields["paths"] = "at least one path is required"
	}
	if len(fields) > 0 {
		return domain.NewValidationError(fields)
	}
	return nil
}
