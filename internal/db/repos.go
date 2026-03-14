package db

import "database/sql"

// Repositories groups all repository instances for dependency injection.
type Repositories struct {
	Users        *UserRepository
	Sessions     *SessionRepository
	Libraries    *LibraryRepository
	Items        *ItemRepository
	MediaStreams  *MediaStreamRepository
	Images       *ImageRepository
	UserData     *UserDataRepository
	Channels     *ChannelRepository
	EPGPrograms  *EPGProgramRepository
}

// NewRepositories creates all repositories from a database connection.
func NewRepositories(database *sql.DB) *Repositories {
	return &Repositories{
		Users:        NewUserRepository(database),
		Sessions:     NewSessionRepository(database),
		Libraries:    NewLibraryRepository(database),
		Items:        NewItemRepository(database),
		MediaStreams:  NewMediaStreamRepository(database),
		Images:       NewImageRepository(database),
		UserData:     NewUserDataRepository(database),
		Channels:     NewChannelRepository(database),
		EPGPrograms:  NewEPGProgramRepository(database),
	}
}
