import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { MemoryRouter, Routes, Route } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import "@/i18n";
import type { ItemDetail as ItemDetailT, MediaItem, UserData } from "@/api/types";

// ─── Mocks ───────────────────────────────────────────────────────────────────
//
// VideoPlayer transitively imports hls.js; mocking it with a no-op
// keeps this test from spinning up a real video element. ImageManager
// pulls in the admin dialog tree (heavy + irrelevant here).
//
// We use `importActual` so the rest of the @/components/player barrel
// (PlayerControls, TimeDisplay, types) is still real — otherwise any
// future import via the same barrel returns `undefined` and the test
// crashes far from the cause.

vi.mock("@/components/player", async () => {
  const actual = await vi.importActual<typeof import("@/components/player")>(
    "@/components/player",
  );
  return {
    ...actual,
    VideoPlayer: ({ title, onClose, onEnded }: { title?: string; onClose: () => void; onEnded?: () => void }) => (
      <div data-testid="video-player">
        <span>{title}</span>
        <button onClick={onClose}>close-player</button>
        <button onClick={() => onEnded?.()}>fire-ended</button>
      </div>
    ),
  };
});

vi.mock("@/components/ImageManager", () => ({
  ImageManager: () => null,
}));

const apiMock = vi.hoisted(() => ({
  getItem: vi.fn(),
  getItemChildren: vi.fn(),
  toggleFavorite: vi.fn(),
  markPlayed: vi.fn(),
  getStreamInfo: vi.fn(),
}));

vi.mock("@/api/client", () => ({
  api: apiMock,
}));

const authStoreState = {
  user: { id: "u-1", username: "tester", role: "user" } as { id: string; username: string; role: string } | null,
};

vi.mock("@/store/auth", () => ({
  useAuthStore: <T,>(selector: (s: typeof authStoreState) => T) => selector(authStoreState),
}));

import ItemDetail from "./ItemDetail";

// ─── Fixtures ────────────────────────────────────────────────────────────────

function makeMediaItem(overrides: Partial<MediaItem> = {}): MediaItem {
  return {
    id: "ep-1",
    type: "episode",
    title: "Episode 1",
    original_title: null,
    year: 2020,
    sort_title: "episode 1",
    overview: null,
    tagline: null,
    genres: [],
    community_rating: null,
    content_rating: null,
    runtime_ticks: null,
    premiere_date: null,
    poster_url: null,
    backdrop_url: null,
    logo_url: null,
    parent_id: "season-1",
    series_id: "series-1",
    season_number: 1,
    episode_number: 1,
    path: null,
    ...overrides,
  };
}

function makeUserData(overrides: Partial<UserData> = {}): UserData {
  return {
    progress: {
      position_ticks: 0,
      percentage: 0,
      audio_stream_index: null,
      subtitle_stream_index: null,
    },
    is_favorite: false,
    played: false,
    play_count: 0,
    last_played_at: null,
    ...overrides,
  };
}

function makeItemDetail(overrides: Partial<ItemDetailT> = {}): ItemDetailT {
  // No `user_data: null` key here — the field is now optional
  // (matches what the backend serializes: key omitted, never null).
  // Tests that need a populated user_data pass it via overrides.
  return {
    ...makeMediaItem(),
    duration_ticks: 18_000_000_000,
    media_streams: [],
    people: [],
    ...overrides,
  };
}

// ─── Render helper ───────────────────────────────────────────────────────────

function renderItemDetail(itemId: string) {
  // Each test gets a fresh client so cached results don't leak across.
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0, staleTime: 0 },
      mutations: { retry: false },
    },
  });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[`/items/${itemId}`]}>
        <Routes>
          <Route path="/items/:id" element={<ItemDetail />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  apiMock.getItem.mockReset();
  apiMock.getItemChildren.mockReset();
  apiMock.toggleFavorite.mockReset();
  apiMock.markPlayed.mockReset();
  apiMock.getStreamInfo.mockReset();
  authStoreState.user = { id: "u-1", username: "tester", role: "user" };
});

// ─── Tests ───────────────────────────────────────────────────────────────────

describe("ItemDetail", () => {
  it("renders a spinner while the item loads", () => {
    apiMock.getItem.mockImplementation(() => new Promise(() => {})); // never resolves
    renderItemDetail("ep-1");
    // Spinner exposes role="status"; query by role rather than the
    // tailwind class so a future swap of the spinner implementation
    // doesn't break the test.
    expect(screen.getByRole("status")).toBeInTheDocument();
  });

  it("renders the not-found state when the item query errors", async () => {
    apiMock.getItem.mockRejectedValue(new Error("404"));
    renderItemDetail("ep-missing");
    expect(await screen.findByText(/not found|no encontrado/i)).toBeInTheDocument();
  });

  it("renders the item title in the hero on happy path", async () => {
    apiMock.getItem.mockResolvedValue(makeItemDetail({ id: "ep-1", title: "Pilot" }));
    apiMock.getItemChildren.mockResolvedValue([]);
    renderItemDetail("ep-1");
    expect(await screen.findByRole("heading", { name: "Pilot" })).toBeInTheDocument();
  });

  it("reflects user_data.is_favorite in the hero favorite button", async () => {
    apiMock.getItem.mockResolvedValue(
      makeItemDetail({ user_data: makeUserData({ is_favorite: true }) }),
    );
    apiMock.getItemChildren.mockResolvedValue([]);
    renderItemDetail("ep-1");
    // The hero button gets the localized aria-label from i18n keys
    // we added in the dedupe commit. Watching that key go through
    // also doubles as a regression check on the i18n wiring.
    expect(
      await screen.findByLabelText(/remove from favorites|quitar de favoritos/i),
    ).toBeInTheDocument();
    expect(screen.queryByLabelText(/^add to favorites|^anadir a favoritos/i)).toBeNull();
  });

  it("calls toggleFavorite when the user clicks the favorite button", async () => {
    apiMock.getItem.mockResolvedValue(
      makeItemDetail({ user_data: makeUserData({ is_favorite: false }) }),
    );
    apiMock.getItemChildren.mockResolvedValue([]);
    apiMock.toggleFavorite.mockResolvedValue(makeUserData({ is_favorite: true }));
    renderItemDetail("ep-1");

    const btn = await screen.findByLabelText(/add to favorites|anadir a favoritos/i);
    fireEvent.click(btn);

    await waitFor(() => expect(apiMock.toggleFavorite).toHaveBeenCalledWith("ep-1"));
  });

  it("does not show admin-only menu items for a non-admin user", async () => {
    apiMock.getItem.mockResolvedValue(
      makeItemDetail({
        media_streams: [{
          index: 0, type: "video", codec: "h264", language: null, title: null,
          channels: null, width: 1920, height: 1080, bitrate: null,
          is_default: true, is_forced: false, hdr_type: null,
        }],
      }),
    );
    apiMock.getItemChildren.mockResolvedValue([]);
    renderItemDetail("ep-1");

    // Open the kebab menu so its items mount.
    const kebab = await screen.findByLabelText(/more options|mas opciones/i);
    fireEvent.click(kebab);
    // "Refresh metadata" / "Image manager" are admin-only — must be absent.
    expect(screen.queryByText(/refresh metadata|actualizar metadatos/i)).toBeNull();
    // The non-admin-visible "Media info" entry must mount as a menu
    // button (the same label also appears as a section heading lower
    // in the page, so we narrow to the button role).
    expect(
      screen.getByRole("button", { name: /media info|info del medio/i }),
    ).toBeInTheDocument();
  });

  it("shows admin-only menu items when the user is an admin", async () => {
    authStoreState.user = { id: "u-admin", username: "admin", role: "admin" };
    apiMock.getItem.mockResolvedValue(makeItemDetail());
    apiMock.getItemChildren.mockResolvedValue([]);
    renderItemDetail("ep-1");

    const kebab = await screen.findByLabelText(/more options|mas opciones/i);
    fireEvent.click(kebab);
    expect(screen.getByText(/refresh metadata|actualizar metadatos/i)).toBeInTheDocument();
  });
});
