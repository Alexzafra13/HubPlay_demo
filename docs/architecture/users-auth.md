# Users, Authentication & Watch Progress — Design Document

## Overview

HubPlay is multiuser from day one. Each user has their own profile, watch progress, favorites, and settings. Authentication is local by default, with plugin support for LDAP/SSO.

---

## 1. User Model

```go
type User struct {
    ID            uuid.UUID
    Username      string
    DisplayName   string
    PasswordHash  string        // bcrypt
    AvatarPath    string        // Path in cache dir
    Role          UserRole
    IsActive      bool          // Disabled accounts can't log in
    MaxSessions   int           // 0 = unlimited
    CreatedAt     time.Time
    LastLoginAt   *time.Time
}

type UserRole string
const (
    RoleAdmin  UserRole = "admin"   // Full access: manage server, users, libraries
    RoleUser   UserRole = "user"    // Watch content, manage own profile
)
```

### First Run Setup
1. Server starts with no users
2. First person to access gets the setup wizard
3. Creates the admin account (username + password)
4. Configures initial libraries
5. After setup, new users are created by admin or via invitation links

---

## 2. Authentication

### Local Auth (Default)
```go
type AuthService interface {
    // Register new user (admin only, or first-run setup)
    Register(ctx context.Context, req RegisterRequest) (*User, error)

    // Login with username + password
    Login(ctx context.Context, username, password string) (*AuthToken, error)

    // Validate a JWT token
    ValidateToken(ctx context.Context, token string) (*User, error)

    // Refresh an expiring token
    RefreshToken(ctx context.Context, refreshToken string) (*AuthToken, error)

    // Change password
    ChangePassword(ctx context.Context, userID uuid.UUID, oldPass, newPass string) error

    // Logout (invalidate refresh token)
    Logout(ctx context.Context, refreshToken string) error
}

type AuthToken struct {
    AccessToken  string        // JWT, short-lived (15 min)
    RefreshToken string        // Opaque token, long-lived (30 days)
    ExpiresAt    time.Time
    User         *User
}
```

### JWT Structure
```json
{
  "sub": "user-uuid",
  "username": "alex",
  "role": "admin",
  "iat": 1710000000,
  "exp": 1710000900
}
```

- Access token: **15 minutes** — short-lived, no need to revoke
- Refresh token: **30 days** — stored in DB, can be revoked on logout
- Signing: HMAC-SHA256 with a server-generated secret stored in config

### QuickConnect (PIN Pairing)

For TVs, game consoles, and devices without a keyboard:

```go
type QuickConnectService interface {
    // Generate a 6-digit PIN (shown on the TV)
    GenerateCode(ctx context.Context, deviceName string) (*QuickConnectCode, error)

    // User authorizes the PIN from their phone/browser
    Authorize(ctx context.Context, userID uuid.UUID, code string) error

    // TV polls until authorized, then gets a token
    Poll(ctx context.Context, code string) (*AuthToken, error)
}

type QuickConnectCode struct {
    Code      string    // "482951"
    DeviceName string
    ExpiresAt time.Time // 5 minutes
}
```

**Flow:**
1. TV app shows: "Go to hubplay.local/quickconnect and enter code: 482951"
2. User opens their phone, logs in, enters the code
3. TV app polls every 2 seconds → once authorized, receives JWT
4. TV is now authenticated as that user

### External Auth (Via Plugins)

Plugins can provide auth — e.g., LDAP, OAuth/SSO. The auth plugin interface:

```protobuf
service AuthProviderPlugin {
  rpc Authenticate(AuthRequest) returns (AuthResponse);
}

message AuthRequest {
  string username = 1;
  string password = 2;
}

message AuthResponse {
  bool success = 1;
  string user_id = 2;
  string display_name = 3;
}
```

Auth flow with plugins:
1. Try local auth first
2. If local fails AND external auth plugins are configured → try each plugin
3. If plugin authenticates the user → create/update local user record + issue JWT
4. All sessions are still managed by HubPlay (JWT), plugins only verify credentials

---

## 3. Permissions

### Library Access Control
Admins can restrict which libraries each user can access:

```go
type UserLibraryAccess struct {
    UserID    uuid.UUID
    LibraryID uuid.UUID
    CanAccess bool
}
```

By default, new users can access all libraries. Admin can restrict per user.

### Permission Matrix

| Action | Admin | User |
|--------|-------|------|
| Watch content | Yes | Yes (allowed libraries) |
| Manage own profile | Yes | Yes |
| View watch history | Yes (all) | Yes (own) |
| Create/edit libraries | Yes | No |
| Manage users | Yes | No |
| Server settings | Yes | No |
| Install plugins | Yes | No |
| Force scan | Yes | No |
| Manage webhooks | Yes | No |

---

## 4. Watch Progress (Continue Watching)

Tracks where each user is in each movie/episode.

```go
type WatchProgress struct {
    UserID     uuid.UUID
    ItemID     uuid.UUID
    Position   time.Duration  // Current playback position
    Duration   time.Duration  // Total item duration
    Percentage float64        // 0.0 - 1.0
    Completed  bool           // Marked when > 90% watched
    UpdatedAt  time.Time
}

type WatchProgressService interface {
    // Update progress (called periodically during playback, every 10 seconds)
    UpdateProgress(ctx context.Context, userID, itemID uuid.UUID, position time.Duration) error

    // Get progress for an item
    GetProgress(ctx context.Context, userID, itemID uuid.UUID) (*WatchProgress, error)

    // Get "Continue Watching" list
    GetContinueWatching(ctx context.Context, userID uuid.UUID, limit int) ([]WatchProgress, error)

    // Get recently watched (completed items)
    GetRecentlyWatched(ctx context.Context, userID uuid.UUID, limit int) ([]WatchProgress, error)

    // Mark as watched/unwatched manually
    MarkWatched(ctx context.Context, userID, itemID uuid.UUID) error
    MarkUnwatched(ctx context.Context, userID, itemID uuid.UUID) error

    // Get next episode to watch in a series
    GetNextUp(ctx context.Context, userID uuid.UUID, seriesID uuid.UUID) (*MediaItem, error)
}
```

### Progress Rules
- **Continue Watching**: items with 5% < progress < 90%
- **Completed**: progress > 90% → mark as watched, remove from "Continue Watching"
- **Next Up**: for series, find the next unwatched episode after the last completed one
- Progress syncs every **10 seconds** during playback (client sends update)
- Credits detection (future): auto-mark as complete when credits start

### Series Progress

```go
type SeriesProgress struct {
    SeriesID        uuid.UUID
    TotalEpisodes   int
    WatchedEpisodes int
    NextEpisode     *MediaItem    // Next episode to watch
    LastWatchedAt   time.Time
}
```

---

## 5. User Preferences

Per-user settings that affect their experience:

```go
type UserPreferences struct {
    UserID              uuid.UUID
    Language            string    // Preferred metadata/UI language
    SubtitleLanguage    string    // Preferred subtitle language
    AudioLanguage       string    // Preferred audio track language
    MaxStreamingQuality string    // "auto", "1080p", "720p", "480p"
    Theme               string    // "dark", "light", "auto"
    EnableAutoPlay      bool      // Auto-play next episode
    ShowWatchedItems    bool      // Show/hide watched items in library
}
```

---

## 6. Favorites & Lists

```go
type Favorite struct {
    UserID    uuid.UUID
    ItemID    uuid.UUID
    AddedAt   time.Time
}

type FavoriteService interface {
    Add(ctx context.Context, userID, itemID uuid.UUID) error
    Remove(ctx context.Context, userID, itemID uuid.UUID) error
    GetAll(ctx context.Context, userID uuid.UUID, opts ListOptions) ([]MediaItem, int, error)
    IsFavorite(ctx context.Context, userID, itemID uuid.UUID) (bool, error)
}
```

Future: custom lists ("Want to Watch", "Best Sci-Fi", etc.)

---

## 7. Sessions & Devices

Track active sessions for security and device management:

```go
type Session struct {
    ID          uuid.UUID
    UserID      uuid.UUID
    DeviceName  string      // "Chrome on Linux", "Samsung TV"
    DeviceID    string      // Unique device identifier
    IPAddress   string
    RefreshToken string     // Hashed
    CreatedAt   time.Time
    LastActiveAt time.Time
    ExpiresAt   time.Time
}

type SessionService interface {
    // List active sessions for a user
    ListSessions(ctx context.Context, userID uuid.UUID) ([]Session, error)

    // Revoke a specific session (logout remote device)
    RevokeSession(ctx context.Context, userID uuid.UUID, sessionID uuid.UUID) error

    // Revoke all sessions except current (security: "log out everywhere")
    RevokeAllOther(ctx context.Context, userID uuid.UUID, currentSessionID uuid.UUID) error
}
```

---

## 8. API Endpoints (Auth & User)

```
# Auth
POST   /api/v1/auth/login              → Login, get tokens
POST   /api/v1/auth/refresh             → Refresh access token
POST   /api/v1/auth/logout              → Logout, revoke refresh token
POST   /api/v1/auth/quickconnect/code   → Generate QuickConnect PIN
POST   /api/v1/auth/quickconnect/auth   → Authorize a PIN
GET    /api/v1/auth/quickconnect/poll   → Poll for authorization

# Users (admin)
GET    /api/v1/users                    → List users
POST   /api/v1/users                    → Create user
GET    /api/v1/users/{id}               → Get user
PUT    /api/v1/users/{id}               → Update user
DELETE /api/v1/users/{id}               → Delete user

# Current user
GET    /api/v1/me                       → Get current user profile
PUT    /api/v1/me                       → Update own profile
PUT    /api/v1/me/password              → Change password
GET    /api/v1/me/preferences           → Get preferences
PUT    /api/v1/me/preferences           → Update preferences
GET    /api/v1/me/sessions              → List active sessions
DELETE /api/v1/me/sessions/{id}         → Revoke a session

# Watch Progress
PUT    /api/v1/me/progress/{itemId}     → Update watch progress
GET    /api/v1/me/progress/{itemId}     → Get progress for item
GET    /api/v1/me/continue-watching     → Continue watching list
GET    /api/v1/me/recently-watched      → Recently watched list
GET    /api/v1/me/next-up               → Next episodes to watch
POST   /api/v1/me/watched/{itemId}      → Mark as watched
DELETE /api/v1/me/watched/{itemId}      → Mark as unwatched

# Favorites
GET    /api/v1/me/favorites             → List favorites
POST   /api/v1/me/favorites/{itemId}    → Add to favorites
DELETE /api/v1/me/favorites/{itemId}    → Remove from favorites
```

---

## 9. Directory Structure

```
internal/
├── auth/
│   ├── service.go          # AuthService implementation
│   ├── jwt.go              # JWT creation + validation
│   ├── quickconnect.go     # QuickConnect PIN flow
│   └── middleware.go       # HTTP auth middleware
├── user/
│   ├── service.go          # User CRUD
│   ├── preferences.go      # User preferences
│   └── session.go          # Session management
├── progress/
│   ├── service.go          # WatchProgressService
│   ├── nextup.go           # Next episode logic
│   └── favorites.go        # Favorites
└── db/
    ├── user_repo.go        # User persistence
    ├── session_repo.go     # Session persistence
    ├── progress_repo.go    # Watch progress persistence
    └── favorite_repo.go    # Favorites persistence
```
