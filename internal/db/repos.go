package db

import "database/sql"

// Repositories groups all repository instances for dependency injection.
type Repositories struct {
	Users    *UserRepository
	Sessions *SessionRepository
}

// NewRepositories creates all repositories from a database connection.
func NewRepositories(database *sql.DB) *Repositories {
	return &Repositories{
		Users:    NewUserRepository(database),
		Sessions: NewSessionRepository(database),
	}
}
