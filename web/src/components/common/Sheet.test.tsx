// Sheet shares the modal stack with Modal — these tests pin the
// "two overlay primitives play nice in the same stack" property
// that the whole refactor exists to guarantee.

import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { Sheet } from "./Sheet";
import { Modal } from "./Modal";
import { useModalStack } from "@/store/modalStack";

describe("Sheet", () => {
  beforeEach(() => {
    useModalStack.setState({ stack: [] });
    document.body.style.overflow = "";
  });

  afterEach(() => {
    cleanup();
    document.body.style.overflow = "";
  });

  it("does not render when isOpen is false", () => {
    render(
      <Sheet isOpen={false} onClose={() => {}} title="Hidden">
        <p>body</p>
      </Sheet>,
    );
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(useModalStack.getState().stack).toHaveLength(0);
  });

  it("registers in the stack and locks body scroll on open", () => {
    render(
      <Sheet isOpen onClose={() => {}} title="Edit">
        <p>body</p>
      </Sheet>,
    );
    expect(screen.getByRole("dialog")).toBeTruthy();
    expect(useModalStack.getState().stack).toHaveLength(1);
    expect(document.body.style.overflow).toBe("hidden");
  });

  it("shares the stack with Modal — closing one keeps the lock if the other is up", () => {
    const modal = render(
      <Modal isOpen onClose={() => {}} title="Outer">
        <p>outer</p>
      </Modal>,
    );
    const sheet = render(
      <Sheet isOpen onClose={() => {}} title="Inner">
        <p>inner</p>
      </Sheet>,
    );

    expect(useModalStack.getState().stack).toHaveLength(2);
    expect(document.body.style.overflow).toBe("hidden");

    sheet.unmount();
    expect(useModalStack.getState().stack).toHaveLength(1);
    expect(document.body.style.overflow).toBe("hidden");

    modal.unmount();
    expect(useModalStack.getState().stack).toHaveLength(0);
    expect(document.body.style.overflow).toBe("");
  });

  it("renders footer slot when provided", () => {
    render(
      <Sheet
        isOpen
        onClose={() => {}}
        title="With footer"
        footer={<button>Save</button>}
      >
        <p>body</p>
      </Sheet>,
    );
    expect(screen.getByRole("button", { name: "Save" })).toBeTruthy();
  });
});
