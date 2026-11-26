import { describe, it, expect } from "vitest";
import { safeReturnTo } from "./returnTo";

describe("safeReturnTo", () => {
  it.each([
    [{ from: "/link?code=ABCD-EFGH" }, "/link?code=ABCD-EFGH"],
    [{ from: "/movies/123" }, "/movies/123"],
    [undefined, "/"],
    [null, "/"],
    [{}, "/"],
    [{ from: 42 }, "/"],
    [{ from: "https://evil.example" }, "/"],
    [{ from: "//evil.example" }, "/"],
    [{ from: "/\\evil.example" }, "/"],
    [{ from: "/login" }, "/"],
    [{ from: "/login?x=1" }, "/"],
    [{ from: "/select-profile" }, "/"],
    [{ from: "/setup/step" }, "/"],
    [{ from: "/loginx" }, "/loginx"],
  ])("safeReturnTo(%j) = %s", (state, want) => {
    expect(safeReturnTo(state)).toBe(want);
  });
});
