import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import "@/i18n";
import type { MediaItem, UserData } from "@/api/types";
import { PosterCard } from "./PosterCard";

function makeItem(overrides: Partial<MediaItem> = {}): MediaItem {
  return {
    id: "it-1",
    type: "movie",
    title: "Foo",
    original_title: null,
    year: 2020,
    sort_title: "foo",
    overview: null,
    tagline: null,
    genres: [],
    community_rating: null,
    content_rating: null,
    duration_ticks: null,
    premiere_date: null,
    poster_url: null,
    backdrop_url: null,
    logo_url: null,
    parent_id: null,
    series_id: null,
    season_number: null,
    episode_number: null,
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

function renderCard(item: MediaItem, progress?: number) {
  return render(
    <MemoryRouter>
      <PosterCard item={item} progress={progress} />
    </MemoryRouter>,
  );
}

describe("PosterCard", () => {
  it("renders the title and links to the right detail route by type", () => {
    renderCard(makeItem({ type: "series", id: "s-1" }));
    const link = screen.getByRole("link");
    expect(link).toHaveAttribute("href", "/series/s-1");
    expect(screen.getByText("Foo")).toBeInTheDocument();
  });

  it("renders the watched check when user_data.played is true", () => {
    renderCard(
      makeItem({ user_data: makeUserData({ played: true, play_count: 1 }) }),
    );
    expect(screen.getByLabelText(/watched|visto/i)).toBeInTheDocument();
    // Progress bar should not coexist with the watched badge.
    expect(screen.queryByRole("progressbar")).toBeNull();
  });

  it("renders the progress bar when user_data has partial progress", () => {
    renderCard(
      makeItem({
        user_data: makeUserData({
          played: false,
          progress: {
            position_ticks: 250,
            percentage: 25,
            audio_stream_index: null,
            subtitle_stream_index: null,
          },
        }),
      }),
    );
    const bar = screen.getByRole("progressbar");
    expect(bar).toHaveAttribute("aria-valuenow", "25");
  });

  it("omits both badges when user_data is absent (anonymous list)", () => {
    renderCard(makeItem());
    expect(screen.queryByRole("progressbar")).toBeNull();
    expect(screen.queryByLabelText(/watched|visto/i)).toBeNull();
  });

  it("explicit progress prop overrides item.user_data progress", () => {
    renderCard(
      makeItem({
        user_data: makeUserData({
          progress: {
            position_ticks: 100,
            percentage: 10,
            audio_stream_index: null,
            subtitle_stream_index: null,
          },
        }),
      }),
      75,
    );
    const bar = screen.getByRole("progressbar");
    expect(bar).toHaveAttribute("aria-valuenow", "75");
  });

  it("renders a BlurHash canvas placeholder when poster_blurhash is present", () => {
    const { container } = renderCard(
      makeItem({
        poster_url: "https://example/poster.jpg",
        // Real-shape BlurHash from the blurhash repo's example fixtures.
        // The component validates before decoding so a malformed value
        // would silently no-op; we want the canvas to actually mount.
        poster_blurhash: "LEHV6nWB2yk8pyo0adR*.7kCMdnj",
      }),
    );
    // The placeholder layer renders as a <canvas aria-hidden="true">
    // sitting absolutely behind the real <img>. Until the image loads,
    // both coexist; once <img> fires onLoad the canvas conditionally
    // unmounts. In jsdom the image never actually decodes, so the
    // canvas should still be present at the time of assertion.
    const canvas = container.querySelector('canvas[aria-hidden="true"]');
    expect(canvas).not.toBeNull();
  });

  it("does not render the BlurHash canvas when poster_blurhash is absent", () => {
    const { container } = renderCard(
      makeItem({ poster_url: "https://example/poster.jpg" }),
    );
    const canvas = container.querySelector('canvas[aria-hidden="true"]');
    expect(canvas).toBeNull();
  });
});
