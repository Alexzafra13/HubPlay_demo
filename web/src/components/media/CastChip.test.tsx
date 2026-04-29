import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { CastChip } from "./CastChip";
import type { Person } from "@/api/types";

// CastChip became a clickable <Link to="/people/{id}"> in the same
// commit that added the person detail page. The chip lives at the
// boundary between the cast strip on the detail surface and that
// page; verify the contract from both sides — the link target lands
// on the right id, the avatar still falls back to the initial when
// no photo is supplied, and the character/role line still surfaces
// the right line for actor vs crew.

function makePerson(overrides: Partial<Person> = {}): Person {
  return {
    id: "p-1",
    name: "Tom Hanks",
    role: "actor",
    character: "Forrest Gump",
    sort_order: 0,
    ...overrides,
  };
}

function renderChip(person: Person) {
  return render(
    <MemoryRouter>
      <CastChip person={person} />
    </MemoryRouter>,
  );
}

describe("CastChip", () => {
  it("links to /people/{id} when clicked", () => {
    renderChip(makePerson());
    const link = screen.getByRole("link");
    expect(link).toHaveAttribute("href", "/people/p-1");
  });

  it("renders the actor's character on the second line", () => {
    renderChip(makePerson());
    expect(screen.getByText("Forrest Gump")).toBeInTheDocument();
  });

  it("falls back to the role label when there's no character (crew)", () => {
    renderChip(
      makePerson({ role: "director", character: "", name: "Robert Zemeckis" }),
    );
    // Crew entries don't have a character — the chip should surface
    // "director" so the user knows what they did on the title.
    expect(screen.getByText("director")).toBeInTheDocument();
  });

  it("renders the initial-letter placeholder when image_url is absent", () => {
    renderChip(makePerson({ image_url: undefined }));
    // The avatar slot falls through to a single uppercase letter.
    // We don't assert on a specific element type because the chip
    // renders the letter directly in the avatar div.
    expect(screen.getByText("T")).toBeInTheDocument();
    // And no <img> mounted — the chip skipped the photo branch
    // entirely.
    expect(screen.queryByRole("img")).toBeNull();
  });

  it("renders the photo when image_url is present", () => {
    renderChip(makePerson({ image_url: "/api/v1/people/p-1/thumb" }));
    const img = screen.getByRole("img");
    expect(img).toHaveAttribute("src", "/api/v1/people/p-1/thumb");
  });
});
