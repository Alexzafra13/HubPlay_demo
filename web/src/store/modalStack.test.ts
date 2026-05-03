import { beforeEach, describe, expect, it } from "vitest";
import { useModalStack, modalStackSelectors } from "./modalStack";

describe("useModalStack", () => {
  beforeEach(() => {
    // Reset between tests — Zustand stores persist across the file
    // otherwise.
    useModalStack.setState({ stack: [] });
  });

  it("starts empty", () => {
    expect(useModalStack.getState().stack).toEqual([]);
    expect(modalStackSelectors.count(useModalStack.getState())).toBe(0);
    expect(modalStackSelectors.topId(useModalStack.getState())).toBeNull();
  });

  it("push appends in LIFO order", () => {
    useModalStack.getState().push("a");
    useModalStack.getState().push("b");
    expect(useModalStack.getState().stack).toEqual(["a", "b"]);
    expect(modalStackSelectors.topId(useModalStack.getState())).toBe("b");
  });

  it("push is idempotent for the same id", () => {
    useModalStack.getState().push("a");
    useModalStack.getState().push("a");
    expect(useModalStack.getState().stack).toEqual(["a"]);
  });

  it("remove drops the matching id without disturbing the rest", () => {
    useModalStack.getState().push("a");
    useModalStack.getState().push("b");
    useModalStack.getState().push("c");
    useModalStack.getState().remove("b");
    expect(useModalStack.getState().stack).toEqual(["a", "c"]);
    expect(modalStackSelectors.topId(useModalStack.getState())).toBe("c");
  });

  it("remove of an unknown id is a no-op (and returns the same reference)", () => {
    useModalStack.getState().push("a");
    const before = useModalStack.getState().stack;
    useModalStack.getState().remove("missing");
    // Same reference signals to React's shallow compares that nothing
    // changed — important so unrelated subscribers don't re-render.
    expect(useModalStack.getState().stack).toBe(before);
  });

  it("count and topId track the stack", () => {
    expect(modalStackSelectors.count(useModalStack.getState())).toBe(0);
    useModalStack.getState().push("a");
    useModalStack.getState().push("b");
    expect(modalStackSelectors.count(useModalStack.getState())).toBe(2);
    useModalStack.getState().remove("b");
    expect(modalStackSelectors.topId(useModalStack.getState())).toBe("a");
    useModalStack.getState().remove("a");
    expect(modalStackSelectors.count(useModalStack.getState())).toBe(0);
    expect(modalStackSelectors.topId(useModalStack.getState())).toBeNull();
  });
});
