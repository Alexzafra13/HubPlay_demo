// Modal — focuses on the wiring between the component and the
// useModalStack store, since that's the surface that prevented the
// "inner modal leaks past its parent" bug. Visual concerns
// (backdrop, animation) are intentionally not covered here.

import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { Modal } from "./Modal";
import { useModalStack } from "@/store/modalStack";

describe("Modal", () => {
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
      <Modal isOpen={false} onClose={() => {}} title="Hidden">
        <p>body</p>
      </Modal>,
    );
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(useModalStack.getState().stack).toHaveLength(0);
  });

  it("registers in the stack and locks body scroll on open", () => {
    render(
      <Modal isOpen onClose={() => {}} title="Top">
        <p>body</p>
      </Modal>,
    );
    expect(screen.getByRole("dialog")).toBeTruthy();
    expect(useModalStack.getState().stack).toHaveLength(1);
    expect(document.body.style.overflow).toBe("hidden");
  });

  it("clears the body scroll lock only when the LAST modal unmounts", () => {
    const outer = render(
      <Modal isOpen onClose={() => {}} title="Outer">
        <p>outer</p>
      </Modal>,
    );
    const inner = render(
      <Modal isOpen onClose={() => {}} title="Inner">
        <p>inner</p>
      </Modal>,
    );

    expect(useModalStack.getState().stack).toHaveLength(2);
    expect(document.body.style.overflow).toBe("hidden");

    // Closing the inner modal must NOT release the lock — the outer is
    // still up. This is the bug the stack manager exists to fix.
    inner.unmount();
    expect(useModalStack.getState().stack).toHaveLength(1);
    expect(document.body.style.overflow).toBe("hidden");

    outer.unmount();
    expect(useModalStack.getState().stack).toHaveLength(0);
    expect(document.body.style.overflow).toBe("");
  });

  it("removes itself from the stack on unmount even if isOpen was true", () => {
    const { unmount } = render(
      <Modal isOpen onClose={() => {}} title="Vanishing">
        <p>body</p>
      </Modal>,
    );
    expect(useModalStack.getState().stack).toHaveLength(1);
    unmount();
    expect(useModalStack.getState().stack).toHaveLength(0);
    expect(document.body.style.overflow).toBe("");
  });
});
