import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";

// MediaBrowse ya tiene su propia batería de tests; aquí sólo
// verificamos que el shim Movies le pasa `type="movie"`.
vi.mock("./MediaBrowse", () => ({
  default: ({ type }: { type: string }) => (
    <div data-testid="media-browse-stub">type={type}</div>
  ),
}));

import Movies from "./Movies";

describe("Movies", () => {
  it("renderiza MediaBrowse con type=movie", () => {
    render(<Movies />);
    expect(screen.getByTestId("media-browse-stub")).toHaveTextContent("type=movie");
  });
});
