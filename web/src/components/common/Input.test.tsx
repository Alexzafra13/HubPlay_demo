import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Input } from "./Input";

describe("Input", () => {
  it("renders with label", () => {
    render(<Input label="Email" />);
    expect(screen.getByLabelText("Email")).toBeInTheDocument();
  });

  it("shows error message", () => {
    render(<Input label="Name" error="Required" />);
    expect(screen.getByText("Required")).toBeInTheDocument();
  });

  it("shows hint when no error", () => {
    render(<Input hint="Optional" />);
    expect(screen.getByText("Optional")).toBeInTheDocument();
  });

  it("hides hint when error is present", () => {
    render(<Input hint="Optional" error="Oops" />);
    expect(screen.queryByText("Optional")).not.toBeInTheDocument();
    expect(screen.getByText("Oops")).toBeInTheDocument();
  });

  it("accepts user input", async () => {
    render(<Input label="Username" />);
    const input = screen.getByLabelText("Username");
    await userEvent.type(input, "admin");
    expect(input).toHaveValue("admin");
  });

  it("derives id from label", () => {
    render(<Input label="Display Name" />);
    expect(screen.getByLabelText("Display Name")).toHaveAttribute(
      "id",
      "display-name",
    );
  });
});
