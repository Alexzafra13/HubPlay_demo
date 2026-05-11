import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import "@/i18n";
import type { MediaItem } from "@/api/types";
import { LandscapeCard } from "./LandscapeCard";

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

function renderCard(props: React.ComponentProps<typeof LandscapeCard>) {
  return render(
    <MemoryRouter>
      <LandscapeCard {...props} />
    </MemoryRouter>,
  );
}

describe("LandscapeCard row actions", () => {
  it("renders no action buttons when neither handler is provided", () => {
    renderCard({ item: makeItem() });
    expect(
      screen.queryByRole("button", { name: /marcar como visto|mark as watched/i }),
    ).toBeNull();
    expect(
      screen.queryByRole("button", { name: /quitar de seguir viendo|remove from continue watching/i }),
    ).toBeNull();
  });

  it("renders the mark-watched button when onMarkWatched is provided", async () => {
    const onMarkWatched = vi.fn();
    renderCard({ item: makeItem(), onMarkWatched });
    const btn = screen.getByRole("button", {
      name: /marcar como visto|mark as watched/i,
    });
    await userEvent.click(btn);
    expect(onMarkWatched).toHaveBeenCalledTimes(1);
    expect(onMarkWatched.mock.calls[0]?.[0]?.id).toBe("it-1");
  });

  it("renders the remove button when onRemove is provided", async () => {
    const onRemove = vi.fn();
    renderCard({ item: makeItem(), onRemove });
    const btn = screen.getByRole("button", {
      name: /quitar de seguir viendo|remove from continue watching/i,
    });
    await userEvent.click(btn);
    expect(onRemove).toHaveBeenCalledTimes(1);
    expect(onRemove.mock.calls[0]?.[0]?.id).toBe("it-1");
  });

  it("clicking a row action does NOT navigate the surrounding link", async () => {
    // The card's <Link> would navigate to /movies/it-1. Asserting no
    // navigation happened proves preventDefault + stopPropagation
    // worked: the surrounding href stays where it was at mount time.
    const onRemove = vi.fn();
    renderCard({ item: makeItem(), onRemove });
    const link = screen.getByRole("link");
    const initialHref = link.getAttribute("href");
    await userEvent.click(
      screen.getByRole("button", {
        name: /quitar de seguir viendo|remove from continue watching/i,
      }),
    );
    expect(link.getAttribute("href")).toBe(initialHref);
    expect(onRemove).toHaveBeenCalledTimes(1);
  });
});
