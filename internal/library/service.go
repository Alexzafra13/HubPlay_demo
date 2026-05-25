package library

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/db"
	"hubplay/internal/domain"
	"hubplay/internal/scanner"

	"github.com/google/uuid"
)

// validateHTTPURL rechaza URLs vacías o no-http(s) para evitar persistir
// payloads como `file:///etc/passwd`. Validar en el boundary de creación
// convierte un error diferido en un 400 inmediato.
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

// normaliseLanguageFilter normaliza códigos de idioma: trim, lowercase,
// dedup, descarta tokens que no sean 2-3 letras ISO. Devuelve CSV.
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

// Service es el facade que ensambla los sub-services de library.
// CRUD + scan + lifecycle viven aquí directamente; ACL e item queries
// se delegan via embedding a AccessControl e ItemQueries respectivamente,
// promoviendo sus métodos sin cambiar la interfaz LibraryService.
type Service struct {
	*AccessControl
	*ItemQueries

	libraries *db.LibraryRepository
	items     *db.ItemRepository
	channels  *db.ChannelRepository
	scanner   *scanner.Scanner
	logger    *slog.Logger

	mu       sync.Mutex
	scanning map[string]bool

	// bgCtx/bgWG: ciclo de vida de goroutines background. bgCtx se
	// cancela en Shutdown; bgWG espera que cada scan termine. Sin esto,
	// goroutines golpearían la DB ya cerrada.
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
	logger *slog.Logger,
) *Service {
	libLogger := logger.With("module", "library")
	bgCtx, bgCancel := context.WithCancel(context.Background())
	return &Service{
		AccessControl: newAccessControl(libraries, libLogger),
		ItemQueries:   newItemQueries(items, streams, images, itemValues, libraries, channels, libLogger),

		libraries: libraries,
		items:     items,
		channels:  channels,
		scanner:   scnr,
		logger:    libLogger,
		scanning:  make(map[string]bool),
		bgCtx:     bgCtx,
		bgCancel:  bgCancel,
	}
}

// Shutdown cancela scans en vuelo y espera que todas las goroutines terminen.
// Seguro llamar múltiples veces. Llamar antes de cerrar el handle de DB.
func (s *Service) Shutdown() {
	s.bgCancel()
	s.bgWG.Wait()
}

type CreateRequest struct {
	Name        string   `json:"name"`
	ContentType string   `json:"content_type"` // movies, shows, music, livetv
	Paths       []string `json:"paths"`
	ScanMode    string   `json:"scan_mode"` // auto, manual

	// Solo IPTV (content_type == "livetv"). M3UURL requerido; EPGURL opcional
	// (RefreshM3U intenta auto-discover desde `#EXTM3U url-tvg=...`).
	M3UURL string `json:"m3u_url,omitempty"`
	EPGURL string `json:"epg_url,omitempty"`

	// LanguageFilter: códigos ISO 639-1 (ej. ["es","en"]) para filtrar canales M3U.
	// Vacío = sin filtro. Persistido como CSV.
	LanguageFilter []string `json:"language_filter,omitempty"`

	// TLSInsecure salta verificación de certificado TLS para URLs de ESTA library.
	TLSInsecure bool `json:"tls_insecure,omitempty"`
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (*librarymodel.Library, error) {
	if err := validateCreateRequest(req); err != nil {
		return nil, err
	}

	if req.ScanMode == "" {
		req.ScanMode = "auto"
	}

	now := time.Now()
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

	s.logger.Info("library created", "id", lib.ID, "name", lib.Name, "type", lib.ContentType)

	// Auto-scan la nueva library (como Jellyfin al crear).
	// livetv se salta — no hay paths filesystem; el admin UI dispara
	// iptv/refresh-m3u tras la creación para popular canales.
	if lib.ScanMode != "manual" && lib.ContentType != "livetv" {
		s.bgWG.Add(1)
		go func() {
			defer s.bgWG.Done()
			scanCtx, cancel := context.WithTimeout(s.bgCtx, 30*time.Minute)
			defer cancel()
			if _, err := s.scanner.ScanLibrary(scanCtx, lib); err != nil {
				s.logger.Error("auto-scan after creation failed", "library_id", lib.ID, "error", err)
			}
		}()
	}

	return lib, nil
}

// CreatePersonalIPTV crea una library livetv Y otorga acceso a ownerUserID
// en una transacción. Atajo admin "IPTV personal" — la library queda
// invisible para otros users no-admin (library_access es opt-in INNER JOIN).
// ownerUserID DEBE ser top-level user.
func (s *Service) CreatePersonalIPTV(ctx context.Context, ownerUserID string, req CreateRequest) (*librarymodel.Library, error) {
	req.ContentType = "livetv"
	req.Paths = nil
	req.ScanMode = "manual"
	if err := validateCreateRequest(req); err != nil {
		return nil, err
	}

	now := time.Now()
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
		"id", lib.ID, "name", lib.Name, "owner_user_id", ownerUserID)
	return lib, nil
}

func (s *Service) GetByID(ctx context.Context, id string) (*librarymodel.Library, error) {
	return s.libraries.GetByID(ctx, id)
}

func (s *Service) List(ctx context.Context) ([]*librarymodel.Library, error) {
	return s.libraries.List(ctx)
}

type UpdateRequest struct {
	Name        string   `json:"name"`
	ContentType string   `json:"content_type"`
	Paths       []string `json:"paths"`
	ScanMode    string   `json:"scan_mode"`

	// Pointer para distinguir "omitido" (dejar existente) de "vacío" (limpiar).
	M3UURL *string `json:"m3u_url,omitempty"`
	EPGURL *string `json:"epg_url,omitempty"`

	// nil = dejar como está, presente-y-vacío = limpiar todo.
	LanguageFilter *[]string `json:"language_filter,omitempty"`

	// nil = dejar como está, true/false = toggle explícito.
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
	lib.UpdatedAt = time.Now()

	if err := s.libraries.Update(ctx, lib); err != nil {
		return nil, fmt.Errorf("updating library: %w", err)
	}

	s.logger.Info("library updated", "id", id, "name", lib.Name)
	return lib, nil
}

func (s *Service) Delete(ctx context.Context, id string) error {
	if err := s.libraries.Delete(ctx, id); err != nil {
		return fmt.Errorf("deleting library: %w", err)
	}
	s.logger.Info("library deleted", "id", id)
	return nil
}

// Scan dispara un scan asíncrono. Si refreshMetadata es true, re-fetch
// metadata e imágenes de providers tras el scan.
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
			s.logger.Error("scan failed", "library_id", id, "error", err)
		}

		if refresh {
			if err := s.scanner.RefreshMetadata(scanCtx, lib); err != nil {
				s.logger.Error("metadata refresh failed", "library_id", id, "error", err)
			}
		}
	}()

	return nil
}

// ScanSync ejecuta un scan síncrono (útil para tests).
func (s *Service) ScanSync(ctx context.Context, id string) (*scanner.ScanResult, error) {
	lib, err := s.libraries.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.scanner.ScanLibrary(ctx, lib)
}

func (s *Service) IsScanning(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.scanning[id]
}

// ScanAll dispara scan asíncrono para todas las libraries en modo auto.
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
			s.logger.Warn("scan-all: skipping library", "id", lib.ID, "name", lib.Name, "error", err)
		}
	}
}

func validateCreateRequest(req CreateRequest) error {
	fields := make(map[string]string)
	if req.Name == "" {
		fields["name"] = "is required"
	}
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
