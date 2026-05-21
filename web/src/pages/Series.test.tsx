import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";

vi.mock("./MediaBrowse", () => ({
  default: ({ type }: { type: string }) => (
    <div data-testid="media-browse-stub">type={type}</div>
  ),
}));

import Series from "./Series";

describe("Series", () => {
  it("renderiza MediaBrowse con type=series", () => {
    render(<Series />);
    expect(screen.getByTestId("media-browse-stub")).toHaveTextContent("type=series");
  });
});
