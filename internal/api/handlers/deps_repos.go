package handlers

// Este fichero declara las interfaces "broad" que `api.Dependencies`
// expone para los repositorios de DB. Cierran la fase 2 del olor H del
// audit 2026-05-14: `Dependencies` tenía 18 campos `*db.XRepository`
// concretos cuando cada handler ya consume interfaces estrechas
// localmente — el contrato quedaba "doblemente expresado" (concreto
// arriba, interface abajo).
//
// Convención: una interface por repo, conteniendo la UNIÓN de los
// métodos que cualquier consumidor (handler, mount_*.go, router.go,
// constructores) invoca a través de `deps.X`. Cuando un handler ya
// tiene su interface estrecha (ej. `ItemRepository`, `ImageRepository`,
// `MetadataRepository` en `interfaces.go`), la interface "broad" la
// re-exporta como subset por composición — el handler sigue
// consumiendo el contrato estrecho que ya conocía.
//
// Los nombres tienen sufijo `Repo` para distinguirlos de las
// interfaces handler-side ya existentes (`ItemRepository`, etc.) sin
// colisionar.

import (
	"context"
	"time"

	"hubplay/internal/db"
	iptvmodel "hubplay/internal/iptv/model"
	librarymodel "hubplay/internal/library/model"
	providermodel "hubplay/internal/provider/model"
)

// ItemsRepo es la unión de métodos del repositorio de items que
// consumen los handlers a través de `Dependencies.Items`. Incluye los
// métodos de la `ItemRepository` estrecha + los que el handler admin
// streams + me_home + el flow de identify metadata invocan.
type ItemsRepo interface {
	GetByID(ctx context.Context, id string) (*librarymodel.Item, error)
	List(ctx context.Context, filter librarymodel.ItemFilter) ([]*librarymodel.Item, int, error)
	GetChildren(ctx context.Context, parentID string) ([]*librarymodel.Item, error)
	ChildCountsByParents(ctx context.Context, parentIDs []string) (map[string]int, error)
	LatestItems(ctx context.Context, libraryID string, itemType string, limit int, allowedRatings ...string) ([]*librarymodel.Item, error)
	LatestSeriesByActivity(ctx context.Context, libraryID string, limit int) ([]*librarymodel.LatestSeriesActivity, error)
	CountByLibrary(ctx context.Context, libraryID string) (int, error)
	SumItemSizesByLibrary(ctx context.Context) (map[string]db.LibrarySizeRow, error)
	Update(ctx context.Context, item *librarymodel.Item) error
}

// MediaStreamsRepo es el contrato del repo de media streams visible
// para los handlers vía Dependencies.MediaStreams.
type MediaStreamsRepo interface {
	ListByItem(ctx context.Context, itemID string) ([]*librarymodel.MediaStream, error)
}

// ImagesRepo es el contrato amplio del repo de imágenes — handlers
// que mutan (admin curation, image upload) + los que sólo leen
// (item detail, federation image) lo comparten.
type ImagesRepo interface {
	GetPrimaryURLs(ctx context.Context, itemIDs []string) (map[string]map[string]librarymodel.PrimaryImageRef, error)
	ListByItem(ctx context.Context, itemID string) ([]*librarymodel.Image, error)
	Create(ctx context.Context, img *librarymodel.Image) error
	SetPrimary(ctx context.Context, itemID, imgType, imageID string) error
	SetLocked(ctx context.Context, imageID string, locked bool) error
	GetByID(ctx context.Context, id string) (*librarymodel.Image, error)
	DeleteByID(ctx context.Context, id string) error
	GetPrimary(ctx context.Context, itemID, imgType string) (*librarymodel.Image, error)
	HasLockedForKind(ctx context.Context, itemID, kind string) (bool, error)
}

// MetadataRepo expone el repo de metadata.
type MetadataRepo interface {
	GetByItemID(ctx context.Context, itemID string) (*librarymodel.Metadata, error)
	GetMetadataBatch(ctx context.Context, itemIDs []string) (map[string]*librarymodel.Metadata, error)
	GetOverviewBatch(ctx context.Context, itemIDs []string) (map[string]string, error)
	Upsert(ctx context.Context, m *librarymodel.Metadata) error
}

// UserDataRepo es el contrato amplio del repo de user_data (progress,
// favoritos, continue watching, next up).
type UserDataRepo interface {
	Get(ctx context.Context, userID, itemID string) (*librarymodel.UserData, error)
	GetBatch(ctx context.Context, userID string, itemIDs []string) (map[string]*librarymodel.UserData, error)
	UpdateProgress(ctx context.Context, userID, itemID string, positionTicks int64, completed bool) error
	MarkPlayed(ctx context.Context, userID, itemID string) error
	SetFavorite(ctx context.Context, userID, itemID string, favorite bool) error
	ContinueWatching(ctx context.Context, userID string, limit int) ([]*librarymodel.ContinueWatchingItem, error)
	Favorites(ctx context.Context, userID string, limit, offset int) ([]*librarymodel.FavoriteItem, error)
	NextUp(ctx context.Context, userID string, limit int) ([]*librarymodel.NextUpItem, error)
	SeriesEpisodeProgress(ctx context.Context, userID, seriesID string) (total, watched int, err error)
	Delete(ctx context.Context, userID, itemID string) error
	ClearProgress(ctx context.Context, userID, itemID string) error
}

// ChaptersRepo expone el repo de chapters.
type ChaptersRepo interface {
	ListByItem(ctx context.Context, itemID string) ([]*librarymodel.Chapter, error)
}

// EpisodeSegmentsRepo expone el repo de skip-intro / skip-credits
// markers a los handlers.
type EpisodeSegmentsRepo interface {
	ListByItem(ctx context.Context, itemID string) ([]librarymodel.EpisodeSegment, error)
}

// PeopleRepo es el contrato amplio del repo de personas — incluye
// listing por item (ItemHandler) + por persona (PeopleHandler admin).
type PeopleRepo interface {
	GetByID(ctx context.Context, id string) (*librarymodel.Person, error)
	ListByItem(ctx context.Context, itemID string) ([]*librarymodel.ItemPersonCredit, error)
	ListFilmographyByPerson(ctx context.Context, personID string) ([]*librarymodel.FilmographyEntry, error)
}

// StudiosRepo expone el repo de studios (StudioHandler).
type StudiosRepo interface {
	List(ctx context.Context) ([]*librarymodel.StudioListEntry, error)
	GetBySlug(ctx context.Context, slug string) (*librarymodel.Studio, error)
	ListItemsForStudio(ctx context.Context, studioID string) ([]*librarymodel.StudioItem, error)
}

// CollectionsRepo es el contrato amplio del repo de colecciones.
type CollectionsRepo interface {
	GetByID(ctx context.Context, id string) (*librarymodel.Collection, error)
	List(ctx context.Context) ([]*librarymodel.CollectionListEntry, error)
	ListItemsForCollection(ctx context.Context, collectionID string) ([]*librarymodel.CollectionItem, error)
}

// CollectionImageOverridesRepo expone el repo de overrides admin de
// imágenes de colección.
type CollectionImageOverridesRepo interface {
	Get(ctx context.Context, collectionID, imageType string) (*librarymodel.CollectionImageOverride, error)
	ListByCollection(ctx context.Context, collectionID string) ([]librarymodel.CollectionImageOverride, error)
	UpsertURL(ctx context.Context, collectionID, imageType, imageURL string) error
	UpsertFile(ctx context.Context, collectionID, imageType, basename string) error
	Delete(ctx context.Context, collectionID, imageType string) error
}

// UserPreferencesRepoForDeps expone el repo de preferencias de usuario.
// Sufijo "ForDeps" para no chocar con la interface handler-side
// `UserPreferencesRepo`.
type UserPreferencesRepoForDeps interface {
	ListByUser(ctx context.Context, userID string) ([]db.UserPreference, error)
	Set(ctx context.Context, userID, key, value string) (*db.UserPreference, error)
	Delete(ctx context.Context, userID, key string) error
}

// HomeRepo expone el repo del home dashboard (Trending, Recommended,
// BecauseYouWatched, LiveNow).
type HomeRepo interface {
	Trending(ctx context.Context, userID string, windowDays, limit int) ([]librarymodel.HomeTrendingItem, error)
	Recommended(ctx context.Context, userID string, limit int) ([]librarymodel.HomeRecommendation, error)
	BecauseYouWatched(ctx context.Context, userID string, limit int) (*librarymodel.HomeBecauseResult, error)
	LiveNow(ctx context.Context, userID string, limit int) ([]librarymodel.HomeLiveNowChannel, error)
}

// ExternalIDsRepo es el contrato amplio del repo de external IDs.
type ExternalIDsRepo interface {
	ListByItem(ctx context.Context, itemID string) ([]*librarymodel.ExternalID, error)
	GetItemIDByExternalID(ctx context.Context, provider, externalID string) (string, error)
	GetByProvider(ctx context.Context, itemID, prov string) (*librarymodel.ExternalID, error)
	HasExternalID(ctx context.Context, itemID string) (bool, error)
	Upsert(ctx context.Context, e *librarymodel.ExternalID) error
}

// LibrariesRepo es el contrato amplio del repo de libraries (no
// confundir con `library.Service`).
type LibrariesRepo interface {
	GetByID(ctx context.Context, id string) (*librarymodel.Library, error)
	List(ctx context.Context) ([]*librarymodel.Library, error)
	ListForUser(ctx context.Context, userID string) ([]*librarymodel.Library, error)
	Create(ctx context.Context, lib *librarymodel.Library) error
	Update(ctx context.Context, lib *librarymodel.Library) error
	Delete(ctx context.Context, id string) error
	GrantAccess(ctx context.Context, userID, libraryID string) error
	RevokeAccess(ctx context.Context, userID, libraryID string) error
	ListAccessByUser(ctx context.Context, userID string) ([]string, error)
	ReplaceAccess(ctx context.Context, userID string, libraryIDs []string) error
	UserHasAccess(ctx context.Context, userID, libraryID string) (bool, error)
	CreateWithGrant(ctx context.Context, lib *librarymodel.Library, ownerUserID string) error
}

// ProvidersConfigRepo expone el repo de configuración de providers
// (config persistente: API keys, enabled). Distinto de
// `ProviderManager` que es el handle del runtime.
type ProvidersConfigRepo interface {
	ListAll(ctx context.Context) ([]*providermodel.ProviderConfig, error)
	GetByName(ctx context.Context, name string) (*providermodel.ProviderConfig, error)
	Upsert(ctx context.Context, p *providermodel.ProviderConfig) error
	Delete(ctx context.Context, name string) error
	SetStatus(ctx context.Context, name, status string) error
	ListByType(ctx context.Context, providerType string) ([]*providermodel.ProviderConfig, error)
	ListActive(ctx context.Context) ([]*providermodel.ProviderConfig, error)
}

// SettingsRepo expone el repo de app_settings (overlay clave/valor
// sobre el YAML).
type SettingsRepo interface {
	Get(ctx context.Context, key string) (string, error)
	GetOr(ctx context.Context, key, def string) (string, error)
	Set(ctx context.Context, key, value string) error
	Delete(ctx context.Context, key string) error
	All(ctx context.Context) (map[string]string, error)
}

// ActivityRepo expone el repo de activity (TopItems +
// DailyWatchActivity).
type ActivityRepo interface {
	DailyWatchActivity(ctx context.Context, cutoff time.Time) ([]db.DailyWatchBucket, error)
	TopItems(ctx context.Context, cutoff time.Time, limit int) ([]db.TopItemRow, error)
}

// IPTVSchedulesRepo expone el repo de IPTV schedules (cron jobs).
type IPTVSchedulesRepo interface {
	Get(ctx context.Context, libraryID, kind string) (*iptvmodel.IPTVScheduledJob, error)
	ListByLibrary(ctx context.Context, libraryID string) ([]*iptvmodel.IPTVScheduledJob, error)
	ListDue(ctx context.Context, now time.Time) ([]*iptvmodel.IPTVScheduledJob, error)
	Upsert(ctx context.Context, job *iptvmodel.IPTVScheduledJob) error
	Delete(ctx context.Context, libraryID, kind string) error
}
