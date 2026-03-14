import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { EmptyState } from "./EmptyState";

describe("EmptyState", () => {
  it("renders title", () => {
    render(<EmptyState title="No items" />);
    expect(screen.getByText("No items")).toBeInTheDocument();
  });

  it("renders description when provided", () => {
    render(<EmptyState title="Empty" description="Add something" />);
    expect(screen.getByText("Add something")).toBeInTheDocument();
  });

  it("renders action when provided", () => {
    render(
      <EmptyState title="Empty" action={<button>Add</button>} />,
    );
    expect(screen.getByRole("button", { name: "Add" })).toBeInTheDocument();
  });

  it("does not render description or action when not provided", () => {
    const { container } = render(<EmptyState title="Nothing" />);
    expect(container.querySelectorAll("p")).toHaveLength(0);
  });
});
