package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"hubplay/internal/db"
)

// Manager orchestrates all registered providers and persists their config.
type Manager struct {
	mu        sync.RWMutex
	metadata  []MetadataProvider
	images    []ImageProvider
	subtitles []SubtitleProvider
	all       map[string]Provider // keyed by name

	repo   *db.ProviderRepository
	logger *slog.Logger
}

// NewManager creates a new provider manager.
func NewManager(repo *db.ProviderRepository, logger *slog.Logger) *Manager {
	return &Manager{
		all:    make(map[string]Provider),
		repo:   repo,
		logger: logger.With("module", "provider-manager"),
	}
}

// Register adds a provider and initializes it from persisted config.
func (m *Manager) Register(ctx context.Context, p Provider) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := p.Name()
	if _, exists := m.all[name]; exists {
		return fmt.Errorf("provider %q already registered", name)
	}

	// Load persisted config
	cfg, err := m.repo.GetByName(ctx, name)
	if err != nil {
		return fmt.Errorf("load provider config %q: %w", name, err)
	}

	// Persist if not already in DB (do this before init so the provider
	// appears in the admin UI even when it lacks an API key).
	if cfg == nil {
		providerType := resolveType(p)
		cfg = &db.ProviderConfig{
			Name:     name,
			Type:     providerType,
			Version:  "1.0",
			Status:   "active",
			Priority: 100,
		}
		if err := m.repo.Upsert(ctx, cfg); err != nil {
			m.logger.Warn("failed to persist provider", "name", name, "error", err)
		}
	}

	// Skip disabled providers
	if cfg.Status == "disabled" {
		m.logger.Info("provider disabled, skipping", "name", name)
		return nil
	}

	// Build init config from persisted data
	initCfg := make(map[string]string)
	initCfg["api_key"] = cfg.APIKey
	initCfg["config_json"] = cfg.ConfigJSON
	// Parse ConfigJSON and merge individual keys so providers can read them
	if cfg.ConfigJSON != "" {
		var parsed map[string]string
		if err := json.Unmarshal([]byte(cfg.ConfigJSON), &parsed); err == nil {
			for k, v := range parsed {
				initCfg[k] = v
			}
		}
	}

	// Initialize
	if err := p.Init(initCfg); err != nil {
		m.logger.Warn("provider init failed (api key may be missing)", "name", name, "error", err)
		return nil // Don't block startup for optional providers
	}

	m.all[name] = p

	// Register by capability
	if mp, ok := p.(MetadataProvider); ok {
		m.metadata = append(m.metadata, mp)
	}
	if ip, ok := p.(ImageProvider); ok {
		m.images = append(m.images, ip)
	}
	if sp, ok := p.(SubtitleProvider); ok {
		m.subtitles = append(m.subtitles, sp)
	}

	m.logger.Info("provider registered", "name", name)
	return nil
}

// SearchMetadata queries all metadata providers and returns the best results.
func (m *Manager) SearchMetadata(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	m.mu.RLock()
	providers := m.metadata
	m.mu.RUnlock()

	var allResults []SearchResult
	for _, p := range providers {
		results, err := p.Search(ctx, query)
		if err != nil {
			m.logger.Warn("metadata search failed", "provider", p.Name(), "error", err)
			continue
		}
		allResults = append(allResults, results...)
	}

	// Sort by score descending
	sort.Slice(allResults, func(i, j int) bool {
		return allResults[i].Score > allResults[j].Score
	})

	return allResults, nil
}

// FetchMetadata gets metadata from the first provider that has it.
func (m *Manager) FetchMetadata(ctx context.Context, externalID string, itemType ItemType) (*MetadataResult, error) {
	m.mu.RLock()
	providers := m.metadata
	m.mu.RUnlock()

	for _, p := range providers {
		result, err := p.GetMetadata(ctx, externalID, itemType)
		if err != nil {
			m.logger.Warn("metadata fetch failed", "provider", p.Name(), "error", err)
			continue
		}
		if result != nil {
			return result, nil
		}
	}

	return nil, fmt.Errorf("no provider returned metadata for %q", externalID)
}

// FetchImages gets images from all image providers and merges results.
func (m *Manager) FetchImages(ctx context.Context, externalIDs map[string]string, itemType ItemType) ([]ImageResult, error) {
	m.mu.RLock()
	providers := m.images
	m.mu.RUnlock()

	var allImages []ImageResult
	for _, p := range providers {
		images, err := p.GetImages(ctx, externalIDs, itemType)
		if err != nil {
			m.logger.Warn("image fetch failed", "provider", p.Name(), "error", err)
			continue
		}
		// Stamp the source on every result here rather than make each
		// provider implementation set it. The aggregator already knows
		// which provider just spoke, so `Source = p.Name()` is the
		// single point of truth — implementations can't forget to set
		// it and callers downstream don't have to URL-sniff.
		name := p.Name()
		for i := range images {
			if images[i].Source == "" {
				images[i].Source = name
			}
		}
		allImages = append(allImages, images...)
	}

	// Sort by score descending
	sort.Slice(allImages, func(i, j int) bool {
		return allImages[i].Score > allImages[j].Score
	})

	return allImages, nil
}

// SearchSubtitles queries all subtitle providers.
func (m *Manager) SearchSubtitles(ctx context.Context, query SubtitleQuery) ([]SubtitleResult, error) {
	m.mu.RLock()
	providers := m.subtitles
	m.mu.RUnlock()

	var allResults []SubtitleResult
	for _, p := range providers {
		results, err := p.SearchSubtitles(ctx, query)
		if err != nil {
			m.logger.Warn("subtitle search failed", "provider", p.Name(), "error", err)
			continue
		}
		allResults = append(allResults, results...)
	}

	sort.Slice(allResults, func(i, j int) bool {
		return allResults[i].Score > allResults[j].Score
	})

	return allResults, nil
}

// GetProvider returns a provider by name.
func (m *Manager) GetProvider(name string) (Provider, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.all[name]
	return p, ok
}

// ListProviders returns all registered provider names.
func (m *Manager) ListProviders() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.all))
	for name := range m.all {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func resolveType(p Provider) string {
	switch {
	case isMetadata(p) && isImage(p):
		return "metadata"
	case isMetadata(p):
		return "metadata"
	case isImage(p):
		return "image"
	case isSubtitle(p):
		return "subtitle"
	default:
		return "unknown"
	}
}

func isMetadata(p Provider) bool { _, ok := p.(MetadataProvider); return ok }
func isImage(p Provider) bool    { _, ok := p.(ImageProvider); return ok }
func isSubtitle(p Provider) bool { _, ok := p.(SubtitleProvider); return ok }
