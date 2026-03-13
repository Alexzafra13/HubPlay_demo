# Frontend UI — Design Document

## Overview

React + TypeScript SPA embebida en el binario Go. Dark theme obligatorio. Diseño inspirado en TiviMate (referencia para IPTV/EPG) y en interfaces de Smart TV modernas (Samsung Tizen, LG webOS).

---

## 1. Tech Stack

| Library | Purpose |
|---------|---------|
| React 18+ | UI framework |
| TypeScript | Type safety |
| Vite | Build tool + HMR |
| React Router | SPA routing |
| hls.js | HLS video playback |
| TanStack Query | API data fetching + cache |
| Tailwind CSS | Styling (dark theme) |
| Planby | EPG timeline grid component (virtual scrolling) |
| react-window | Virtual scrolling for large lists |
| Zustand | Lightweight state management |

---

## 2. Page Structure

```
/                         → Home (Continue Watching, Recently Added, Recommendations)
/movies                   → Movie library (grid/list view)
/movies/{id}              → Movie detail (metadata, cast, stream button)
/series                   → Series library
/series/{id}              → Series detail (seasons, episodes)
/series/{id}/s/{s}/e/{e}  → Episode detail + player
/live-tv                  → Live TV player + channel browser
/live-tv/guide            → Full-screen EPG timeline grid
/search                   → Global search
/favorites                → User favorites
/settings                 → User preferences
/admin                    → Admin panel (libraries, users, plugins, federation)
/admin/activity           → Activity log
/setup                    → First-run setup wizard
/quickconnect             → QuickConnect PIN entry page
```

---

## 3. Design System

### Theme
- **Dark background**: `#0f0f1a` (main), `#1a1a2e` (cards/surfaces), `#16213e` (elevated)
- **Text**: `#ffffff` (primary), `#a0a0b0` (secondary), `#6b6b80` (muted)
- **Accent**: configurable, default `#6366f1` (indigo) for interactive elements
- **Now Playing**: `#ef4444` (red) for live indicator, progress bars, "NOW" badges
- **Success/Error**: `#22c55e` / `#ef4444`

### Typography
- Sans-serif: Inter or system font stack
- Title sizes: movie titles large, metadata secondary size
- Monospace for technical info (codecs, bitrate) in admin views

### Cards
- Rounded corners (`8px`)
- Subtle shadow on hover
- Blurhash placeholder while images load
- Poster ratio: 2:3 (movies), 16:9 (backdrops, episodes)

---

## 4. Home Page (`/`)

```
┌──────────────────────────────────────────────────────────┐
│  [Hero Banner - Featured item with backdrop + play button] │
├──────────────────────────────────────────────────────────┤
│  Continue Watching                               See All →│
│  ┌─────┐ ┌─────┐ ┌─────┐ ┌─────┐ ┌─────┐               │
│  │ ███ │ │ ███ │ │ ███ │ │ ███ │ │ ███ │  ← horizontal  │
│  │ 45% │ │ 12% │ │ 78% │ │ 33% │ │ 60% │    scroll      │
│  └─────┘ └─────┘ └─────┘ └─────┘ └─────┘               │
├──────────────────────────────────────────────────────────┤
│  Recently Added Movies                           See All →│
│  ┌─────┐ ┌─────┐ ┌─────┐ ┌─────┐ ┌─────┐               │
│  │     │ │     │ │     │ │     │ │     │               │
│  │ POST│ │ POST│ │ POST│ │ POST│ │ POST│               │
│  └─────┘ └─────┘ └─────┘ └─────┘ └─────┘               │
├──────────────────────────────────────────────────────────┤
│  Next Up (Series)                                See All →│
│  ┌──────────┐ ┌──────────┐ ┌──────────┐                 │
│  │ S02E05   │ │ S01E03   │ │ S03E11   │                 │
│  │ Show Name│ │ Show Name│ │ Show Name│                 │
│  └──────────┘ └──────────┘ └──────────┘                 │
├──────────────────────────────────────────────────────────┤
│  Live TV - On Now                                See All →│
│  ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐            │
│  │ 🔴 La1 │ │ 🔴 A3  │ │ 🔴 T5  │ │ 🔴 La2 │            │
│  │ News.. │ │ Film.. │ │ Game..│ │ Doc..  │            │
│  └────────┘ └────────┘ └────────┘ └────────┘            │
└──────────────────────────────────────────────────────────┘
```

---

## 5. Live TV — Main View (`/live-tv`)

This is where the Xiaomi/TiviMate experience happens.

### Layout: Player + Channel Browser

```
┌──────────────────────────────────────────────────────────┐
│                                                          │
│                    VIDEO PLAYER                           │
│                    (full width)                           │
│                                                          │
├──────────────────────────────────────────────────────────┤
│  Now: La 1 - Telediario 21:00   ████████░░ 67%  │ Next: El Tiempo │
├──────────────────────────────────────────────────────────┤
│  [All] [Favoritos] [Nacionales] [Deportes] [Noticias] [Películas] │
├──────────────────────────────────────────────────────────┤
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐    │
│  │ 🔴 Logo  │ │    Logo  │ │    Logo  │ │    Logo  │    │
│  │ La 1     │ │ La 2     │ │ Antena 3 │ │ Cuatro   │    │
│  │ Teledia..│ │ Documenta│ │ El Hormi.│ │ Todo es..│    │
│  │ ████░░░  │ │ ██░░░░░  │ │ ██████░  │ │ ███░░░░  │    │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘    │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐    │
│  │    Logo  │ │    Logo  │ │    Logo  │ │    Logo  │    │
│  │ Telecinco│ │ La Sexta │ │ TVE 24h  │ │ Deportes │    │
│  │ GH VIP  │ │ El Inter.│ │ Noticias │ │ Liga F..  │    │
│  │ █████░░  │ │ ███░░░░  │ │ ████░░░  │ │ ██░░░░░  │    │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘    │
└──────────────────────────────────────────────────────────┘
```

### Channel Card Component
Each channel card shows:
- Channel logo (from M3U)
- Channel name
- Red dot `🔴` if currently selected/watching
- Current program title (from EPG)
- Progress bar showing how far into the current program
- Next program title (subtle, secondary text)

### Channel Switch Behavior
1. User clicks a channel card
2. Player switches stream instantly (< 500ms perceived)
3. A brief **overlay info bar** appears on the player:
   ```
   ┌──────────────────────────────────────────┐
   │  [Logo] La 1  •  Canal 1                 │
   │  Telediario 21:00                        │
   │  ████████████░░░░  67%                   │
   │  Siguiente: El Tiempo                    │
   └──────────────────────────────────────────┘
   ```
4. Overlay auto-hides after 4 seconds

### Mini-Player (Browse while watching)
When user navigates to EPG guide or other pages:
- Current stream continues in a **floating mini-player** (bottom-right corner)
- Mini-player is draggable
- Click to return to full player view
- Close button to stop playback

---

## 6. EPG Timeline Grid (`/live-tv/guide`)

Full-screen electronic program guide, the "TV guide" experience.

### Layout

```
┌──────┬────────────┬────────────┬────────────┬────────────┐
│      │   20:00    │   20:30    │   21:00    │   21:30    │
├──────┼────────────┴────────────┼────────────┴────────────┤
│[Logo]│ Telediario 21:00       │ El Tiempo   │ Película..│
│ La 1 │ ███████████████████     │             │           │
├──────┼──────────┬──────────────┼─────────────┴───────────┤
│[Logo]│ Documenta│  Filmoteca   │ Cine de Barrio          │
│ La 2 │          │              │                         │
├──────┼──────────┴──────┬───────┴─────────────────────────┤
│[Logo]│ El Hormiguero   │ Tu Cara Me Suena               │
│  A3  │                 │                                 │
├──────┼─────────────────┴──────┬──────────────────────────┤
│[Logo]│ Todo es Mentira        │ Cuarto Milenio           │
│  C4  │                        │                          │
└──────┴────────────────────────┴──────────────────────────┘
         ↑
    Red "NOW" line (vertical, moves with time)
```

### EPG Features
- **Virtual scrolling** both horizontal (time) and vertical (channels) — essential for 500+ channels
- **Red "NOW" line**: vertical marker at current time, always visible
- **Auto-scroll**: on load, scrolls to current time
- **Program hover/click**: shows popup with description, duration, category, episode info
- **Time navigation**: buttons for "Now", "+1h", "+2h", "Tomorrow", or date picker
- **Channel filtering**: same category tabs as main view (filters rows)
- **Mini-player**: floats in corner while browsing the guide
- **Color coding**: subtle background tint per genre (blue=news, green=sports, purple=film, etc.)
- **Keyboard navigation**: arrow keys to move between cells, Enter to tune

### Implementation
Use **Planby** React library as base for the EPG grid. It provides:
- Virtual scrolling for large channel/program lists
- Timeline with customizable layout
- Channel sidebar
- Program component customization

---

## 7. Movie/Series Library

### Grid View (Default)
```
┌──────────────────────────────────────────────────────────┐
│  Movies  [Grid ☷] [List ≡]  Sort: [Recently Added ▼]    │
│  Genres: [All] [Action] [Sci-Fi] [Drama] [Comedy] [+]   │
├──────────────────────────────────────────────────────────┤
│  ┌───────┐ ┌───────┐ ┌───────┐ ┌───────┐ ┌───────┐     │
│  │       │ │       │ │       │ │       │ │       │     │
│  │ POSTER│ │ POSTER│ │ POSTER│ │ POSTER│ │ POSTER│     │
│  │       │ │       │ │  ✓    │ │       │ │       │     │
│  ├───────┤ ├───────┤ ├───────┤ ├───────┤ ├───────┤     │
│  │Title  │ │Title  │ │Title  │ │Title  │ │Title  │     │
│  │2024   │ │2023   │ │2024   │ │2023   │ │2024   │     │
│  │★ 8.1  │ │★ 7.4  │ │★ 9.0  │ │★ 6.8  │ │★ 8.5  │     │
│  └───────┘ └───────┘ └───────┘ └───────┘ └───────┘     │
│  ┌───────┐ ┌───────┐ ┌───────┐ ┌───────┐ ┌───────┐     │
│  │       │ │       │ │       │ │       │ │       │     │
│  ...                                                     │
└──────────────────────────────────────────────────────────┘
```

- Posters with blurhash while loading
- Watched badge (checkmark) on completed items
- Progress bar overlay at bottom for partially watched
- Hover: shows backdrop + quick info + play button
- Infinite scroll with virtual rendering

### Movie Detail Page

```
┌──────────────────────────────────────────────────────────┐
│  [Backdrop image, full width, gradient to dark at bottom] │
│                                                          │
│  ┌────────┐  Inception (2010)                            │
│  │        │  ★ 8.4  •  PG-13  •  2h 28m  •  Sci-Fi     │
│  │ POSTER │                                              │
│  │        │  [▶ Play] [♡ Favorite] [⬇ Download]          │
│  │        │                                              │
│  └────────┘  A thief who steals corporate secrets through│
│              the use of dream-sharing technology...       │
├──────────────────────────────────────────────────────────┤
│  Cast                                                    │
│  ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐                    │
│  │ Photo│ │ Photo│ │ Photo│ │ Photo│  ← horizontal       │
│  │ Leo  │ │ Ellen│ │ Tom  │ │ Ken  │    scroll           │
│  │ Cobb │ │Ariadne│ │Eames │ │Saito │                    │
│  └──────┘ └──────┘ └──────┘ └──────┘                    │
├──────────────────────────────────────────────────────────┤
│  Technical Info                                          │
│  Video: HEVC 4K HDR10  •  Audio: TrueHD 7.1, AAC 2.0   │
│  Subtitles: Spanish, English, French                     │
├──────────────────────────────────────────────────────────┤
│  Similar Movies                                          │
│  ┌───────┐ ┌───────┐ ┌───────┐ ┌───────┐               │
│  │POSTER │ │POSTER │ │POSTER │ │POSTER │               │
│  └───────┘ └───────┘ └───────┘ └───────┘               │
└──────────────────────────────────────────────────────────┘
```

---

## 8. Video Player

### Controls Overlay (appears on hover/tap)
```
┌──────────────────────────────────────────────────────────┐
│  [← Back]                          [CC] [Audio] [⛶]     │
│                                                          │
│                                                          │
│                     [▶ / ⏸]                              │
│                                                          │
│                                                          │
│  1:23:45 ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━░░░░░░ 2:28:00  │
│           ↑ Trickplay thumbnail on hover                 │
│  [Skip Intro]                              [Next Episode]│
└──────────────────────────────────────────────────────────┘
```

### Player Features
- **Skip Intro / Skip Credits** buttons (from `media_segments` data)
- **Trickplay thumbnails**: hover over progress bar shows timeline preview
- **Subtitle selector**: dropdown with all available tracks
- **Audio track selector**: dropdown with all audio streams
- **Quality selector**: Auto, 4K, 1080p, 720p, 480p
- **Next Episode**: auto-play countdown (10s) when episode ends
- **Keyboard shortcuts**: Space=play/pause, F=fullscreen, M=mute, ←→=seek 10s, ↑↓=volume
- **Remember position**: saves progress every 10 seconds via API
- **Chapters**: if available, shown as markers on the progress bar

---

## 9. Federated Servers

### Sidebar Section
```
┌────────────────────────┐
│  My Libraries          │
│    📁 Movies           │
│    📁 TV Shows         │
│    📺 TV en Directo    │
│                        │
│  Federated Servers     │
│    🟢 Pedro's Server   │
│    🟢 María's Server   │
│    🔴 Carlos (offline) │
│                        │
│  [♡ Favorites]         │
│  [⏱ Continue Watching] │
│  [🔍 Search]           │
└────────────────────────┘
```

- Green/red dot for online/offline status
- Click a federated server → shows their shared libraries
- Federated items show a subtle "remote" badge on cards
- Streaming works identical to local — user doesn't notice the difference

---

## 10. Admin Panel (`/admin`)

```
/admin/libraries    → Library management (add, edit, scan, paths)
/admin/users        → User management (create, edit, permissions)
/admin/plugins      → Plugin management (install, enable/disable)
/admin/federation   → Peer management (link, permissions, sync status)
/admin/webhooks     → Webhook configuration
/admin/activity     → Activity log (filterable by type, user, date)
/admin/system       → Server info, FFmpeg status, storage usage, cache management
```

---

## 11. Responsive Behavior

| Screen | Layout |
|--------|--------|
| Desktop (>1200px) | Full layout, sidebar navigation, multi-column grids |
| Tablet (768-1200px) | Collapsed sidebar, 3-column grids, full EPG |
| Mobile (<768px) | Bottom tab navigation, single column, simplified EPG (list view instead of grid) |

On mobile the EPG timeline grid is replaced with a **channel list + "now/next" view** since the grid is hard to navigate on small screens.

---

## 12. Key UX Rules

1. **Never interrupt playback** — browsing EPG, searching, anything = mini-player keeps playing
2. **Dark theme only** for v1 — media content looks better on dark backgrounds
3. **Blurhash everywhere** — no blank rectangles while images load
4. **Progress bars on everything** — movies, episodes, live programs, scans
5. **< 500ms channel switching** — pre-buffer if possible
6. **Keyboard-first for TV** — arrow keys, Enter, Escape, number keys work everywhere
7. **Loading states** — skeleton screens (shimmer), not spinners
8. **Instant search** — filter as you type, debounced 300ms

---

## 13. Additional API Endpoints Needed

The frontend needs these endpoints that weren't in the original API design:

```
# Home page
GET /api/v1/me/home                → Aggregated home data (continue watching, recently added, next up, live now)

# EPG
GET /api/v1/channels/{id}/epg?from=&to=  → EPG for a single channel (time range)
GET /api/v1/channels/epg?from=&to=&group= → EPG for all channels (batch, for grid view)
GET /api/v1/channels/now              → What's playing now on all channels (lightweight)

# Similar content
GET /api/v1/items/{id}/similar        → Similar movies/series (based on genres + cast)

# Federated content on home
GET /api/v1/federation/highlights     → Curated picks from federated servers for home page
```

---

## 14. Directory Structure (Frontend)

```
web/src/
├── components/
│   ├── layout/
│   │   ├── Sidebar.tsx
│   │   ├── MiniPlayer.tsx
│   │   └── TopBar.tsx
│   ├── player/
│   │   ├── VideoPlayer.tsx       # hls.js wrapper
│   │   ├── PlayerControls.tsx
│   │   ├── SkipButton.tsx        # Skip intro/credits
│   │   ├── TrickplayPreview.tsx
│   │   └── SubtitleSelector.tsx
│   ├── epg/
│   │   ├── EPGGrid.tsx           # Planby wrapper
│   │   ├── ChannelCard.tsx
│   │   ├── ChannelList.tsx
│   │   ├── NowNextBar.tsx
│   │   └── ChannelOverlay.tsx    # Info overlay on channel switch
│   ├── media/
│   │   ├── PosterCard.tsx
│   │   ├── EpisodeCard.tsx
│   │   ├── HeroSection.tsx
│   │   ├── CastRow.tsx
│   │   └── MediaGrid.tsx
│   ├── common/
│   │   ├── BlurhashImage.tsx
│   │   ├── ProgressBar.tsx
│   │   ├── SkeletonLoader.tsx
│   │   └── SearchBar.tsx
│   └── admin/
│       ├── LibraryManager.tsx
│       ├── UserManager.tsx
│       └── ActivityLog.tsx
├── pages/
│   ├── Home.tsx
│   ├── Movies.tsx
│   ├── MovieDetail.tsx
│   ├── Series.tsx
│   ├── SeriesDetail.tsx
│   ├── LiveTV.tsx
│   ├── EPGGuide.tsx
│   ├── Search.tsx
│   ├── Favorites.tsx
│   ├── Settings.tsx
│   ├── Admin.tsx
│   ├── Login.tsx
│   ├── Setup.tsx
│   └── QuickConnect.tsx
├── hooks/
│   ├── usePlayer.ts              # Player state management
│   ├── useEPG.ts                 # EPG data fetching + caching
│   ├── useProgress.ts            # Watch progress sync
│   └── useKeyboard.ts            # Keyboard shortcut handling
├── api/
│   └── client.ts                 # API client (TanStack Query)
├── store/
│   └── player.ts                 # Zustand store for player state
├── styles/
│   └── globals.css               # Tailwind config + custom styles
└── App.tsx
```
