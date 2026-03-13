package library

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/domain"
	"hubplay/internal/scanner"

	"github.com/google/uuid"
)

type Service struct {
	libraries *db.LibraryRepository
	items     *db.ItemRepository
	streams   *db.MediaStreamRepository
	images    *db.ImageRepository
	scanner   *scanner.Scanner
	logger    *slog.Logger

	// Track active scans to prevent concurrent scans of the same library
	mu       sync.Mutex
	scanning map[string]bool
}

func NewService(
	libraries *db.LibraryRepository,
	items *db.ItemRepository,
	streams *db.MediaStreamRepository,
	images *db.ImageRepository,
	scnr *scanner.Scanner,
	logger *slog.Logger,
) *Service {
	return &Service{
		libraries: libraries,
		items:     items,
		streams:   streams,
		images:    images,
		scanner:   scnr,
		logger:    logger.With("module", "library"),
		scanning:  make(map[string]bool),
	}
}

type CreateRequest struct {
	Name        string   `json:"name"`
	ContentType string   `json:"content_type"` // movies, shows, music
	Paths       []string `json:"paths"`
	ScanMode    string   `json:"scan_mode"` // auto, manual
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (*db.Library, error) {
	if err := validateCreateRequest(req); err != nil {
		return nil, err
	}

	if req.ScanMode == "" {
		req.ScanMode = "auto"
	}

	now := time.Now()
	lib := &db.Library{
		ID:           uuid.NewString(),
		Name:         req.Name,
		ContentType:  req.ContentType,
		ScanMode:     req.ScanMode,
		ScanInterval: "6h",
		CreatedAt:    now,
		UpdatedAt:    now,
		Paths:        req.Paths,
	}

	if err := s.libraries.Create(ctx, lib); err != nil {
		return nil, fmt.Errorf("creating library: %w", err)
	}

	s.logger.Info("library created", "id", lib.ID, "name", lib.Name, "type", lib.ContentType)
	return lib, nil
}

func (s *Service) GetByID(ctx context.Context, id string) (*db.Library, error) {
	return s.libraries.GetByID(ctx, id)
}

func (s *Service) List(ctx context.Context) ([]*db.Library, error) {
	return s.libraries.List(ctx)
}

func (s *Service) ListForUser(ctx context.Context, userID string) ([]*db.Library, error) {
	return s.libraries.ListForUser(ctx, userID)
}

type UpdateRequest struct {
	Name        string   `json:"name"`
	ContentType string   `json:"content_type"`
	Paths       []string `json:"paths"`
	ScanMode    string   `json:"scan_mode"`
}

func (s *Service) Update(ctx context.Context, id string, req UpdateRequest) (*db.Library, error) {
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

// Scan triggers an async scan for a library. Returns immediately.
func (s *Service) Scan(ctx context.Context, id string) error {
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

	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.scanning, id)
			s.mu.Unlock()
		}()

		// Use a background context so scan isn't tied to the HTTP request
		scanCtx := context.Background()
		if _, err := s.scanner.ScanLibrary(scanCtx, lib); err != nil {
			s.logger.Error("scan failed", "library_id", id, "error", err)
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

// Items delegates to the item repository with filters.
func (s *Service) ListItems(ctx context.Context, filter db.ItemFilter) ([]*db.Item, int, error) {
	if filter.Limit <= 0 {
		filter.Limit = 20
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}
	return s.items.List(ctx, filter)
}

func (s *Service) GetItem(ctx context.Context, id string) (*db.Item, error) {
	return s.items.GetByID(ctx, id)
}

func (s *Service) GetItemChildren(ctx context.Context, id string) ([]*db.Item, error) {
	return s.items.GetChildren(ctx, id)
}

func (s *Service) GetItemStreams(ctx context.Context, itemID string) ([]*db.MediaStream, error) {
	return s.streams.ListByItem(ctx, itemID)
}

func (s *Service) GetItemImages(ctx context.Context, itemID string) ([]*db.Image, error) {
	return s.images.ListByItem(ctx, itemID)
}

func (s *Service) LatestItems(ctx context.Context, libraryID string, limit int) ([]*db.Item, error) {
	return s.items.LatestItems(ctx, libraryID, limit)
}

func (s *Service) ItemCount(ctx context.Context, libraryID string) (int, error) {
	return s.items.CountByLibrary(ctx, libraryID)
}

func validateCreateRequest(req CreateRequest) error {
	fields := make(map[string]string)
	if req.Name == "" {
		fields["name"] = "is required"
	}
	validTypes := map[string]bool{"movies": true, "shows": true, "music": true}
	if !validTypes[req.ContentType] {
		fields["content_type"] = "must be movies, shows, or music"
	}
	if len(req.Paths) == 0 {
		fields["paths"] = "at least one path is required"
	}
	if len(fields) > 0 {
		return domain.NewValidationError(fields)
	}
	return nil
}
