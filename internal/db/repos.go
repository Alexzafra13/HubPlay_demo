package db

import "database/sql"

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
	EPGPrograms        *EPGProgramRepository
	LibraryEPGSources  *LibraryEPGSourceRepository
	ChannelOverrides   *ChannelOverrideRepository
	UserPreferences    *UserPreferenceRepository
	Providers          *ProviderRepository
	Metadata           *MetadataRepository
	ExternalIDs        *ExternalIDRepository
}

// NewRepositories creates all repositories from a database connection.
func NewRepositories(database *sql.DB) *Repositories {
	return &Repositories{
		Users:              NewUserRepository(database),
		Sessions:           NewSessionRepository(database),
		SigningKeys:        NewSigningKeyRepository(database),
		Libraries:          NewLibraryRepository(database),
		Items:              NewItemRepository(database),
		MediaStreams:       NewMediaStreamRepository(database),
		Images:             NewImageRepository(database),
		UserData:           NewUserDataRepository(database),
		Channels:           NewChannelRepository(database),
		ChannelFavorites:   NewChannelFavoritesRepository(database),
		EPGPrograms:        NewEPGProgramRepository(database),
		LibraryEPGSources:  NewLibraryEPGSourceRepository(database),
		ChannelOverrides:   NewChannelOverrideRepository(database),
		UserPreferences:    NewUserPreferenceRepository(database),
		Providers:          NewProviderRepository(database),
		Metadata:           NewMetadataRepository(database),
		ExternalIDs:        NewExternalIDRepository(database),
	}
}
