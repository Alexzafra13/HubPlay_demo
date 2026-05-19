package db

import (
	"database/sql"
)

// Repositories groups all repository instances for dependency injection.
type Repositories struct {
	Users              *UserRepository
	Sessions           *SessionRepository
	SigningKeys        *SigningKeyRepository
	Libraries          *LibraryRepository
	Items              *ItemRepository
	MediaStreams       *MediaStreamRepository
	Images             *ImageRepository
	UserData           *UserDataRepository
	Channels           *ChannelRepository
	ChannelFavorites   *ChannelFavoritesRepository
	ChannelOrder       *UserChannelOrderRepository
	LibraryChannelOrder *LibraryChannelOrderRepository
	ChannelWatchHistory *ChannelWatchHistoryRepository
	EPGPrograms        *EPGProgramRepository
	LibraryEPGSources  *LibraryEPGSourceRepository
	ChannelOverrides   *ChannelOverrideRepository
	ChannelLogoOverrides *ChannelLogoOverrideRepository
	IPTVSchedules      *IPTVScheduleRepository
	UserPreferences    *UserPreferenceRepository
	Providers          *ProviderRepository
	Metadata           *MetadataRepository
	ExternalIDs        *ExternalIDRepository
	Chapters           *ChapterRepository
	Settings           *SettingsRepository
	People             *PeopleRepository
	DeviceCodes        *DeviceCodeRepository
	Home               *HomeRepository
	ItemValues         *ItemValueRepository
	Studios            *StudioRepository
	Collections        *CollectionRepository
	EpisodeSegments    *EpisodeSegmentRepository
	ItemMetadataLocks  *ItemMetadataLockRepository
	CollectionImageOverrides *CollectionImageOverrideRepository
	// UploadAudit registra cada upload (PR2 feature upload). Append-only;
	// el service de upload inserta una fila al cerrar el pipeline. El
	// admin panel lo consulta via /admin/uploads/audit (no expuesto en v1).
	UploadAudit        *UploadAuditRepository
}

// NewRepositories creates all repositories from a database connection.
//
// `driver` selects the dual-dialect backend per repo. "postgres" wires
// repos against the sqlc_pg generated package; anything else
// (typically "sqlite") wires against sqlc — the project's default
// backend. Until Sesión E finishes refactoring every repo, only the
// ones already migrated honour the driver; the rest ignore it and
// stay SQLite-only. The signature keeps the driver param so callers
// (main.go) don't have to change again as each repo lands.
func NewRepositories(driver string, database *sql.DB) *Repositories {
	return &Repositories{
		Users:              NewUserRepository(driver, database),
		Sessions:           NewSessionRepository(driver, database),
		SigningKeys:        NewSigningKeyRepository(driver, database),
		Libraries:          NewLibraryRepository(driver, database),
		Items:              NewItemRepository(driver, database),
		MediaStreams:       NewMediaStreamRepository(driver, database),
		Images:             NewImageRepository(driver, database),
		UserData:           NewUserDataRepository(driver, database),
		Channels:           NewChannelRepository(driver, database),
		ChannelFavorites:   NewChannelFavoritesRepository(driver, database),
		ChannelOrder:       NewUserChannelOrderRepository(driver, database),
		LibraryChannelOrder: NewLibraryChannelOrderRepository(driver, database),
		ChannelWatchHistory: NewChannelWatchHistoryRepository(driver, database),
		EPGPrograms:        NewEPGProgramRepository(driver, database),
		LibraryEPGSources:  NewLibraryEPGSourceRepository(driver, database),
		ChannelOverrides:   NewChannelOverrideRepository(driver, database),
		ChannelLogoOverrides: NewChannelLogoOverrideRepository(driver, database),
		IPTVSchedules:      NewIPTVScheduleRepository(driver, database),
		UserPreferences:    NewUserPreferenceRepository(driver, database),
		Providers:          NewProviderRepository(driver, database),
		Metadata:           NewMetadataRepository(driver, database),
		ExternalIDs:        NewExternalIDRepository(driver, database),
		Chapters:           NewChapterRepository(driver, database),
		Settings:           NewSettingsRepository(driver, database),
		People:             NewPeopleRepository(driver, database),
		DeviceCodes:        NewDeviceCodeRepository(driver, database),
		Home:               NewHomeRepository(driver, database),
		ItemValues:         NewItemValueRepository(driver, database),
		Studios:            NewStudioRepository(driver, database),
		Collections:        NewCollectionRepository(driver, database),
		EpisodeSegments:    NewEpisodeSegmentRepository(driver, database),
		ItemMetadataLocks:  NewItemMetadataLockRepository(driver, database),
		CollectionImageOverrides: NewCollectionImageOverrideRepository(driver, database),
		UploadAudit:        NewUploadAuditRepository(driver, database),
	}
}
