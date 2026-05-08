import { describe, it, expect } from "vitest";
import { AVATAR_PALETTE, avatarColorFor } from "./avatarColor";

describe("avatarColorFor", () => {
  it("returns a palette entry for any non-empty input", () => {
    const result = avatarColorFor("alex");
    expect(AVATAR_PALETTE).toContainEqual(result);
  });

  it("is deterministic — same seed → same colour", () => {
    expect(avatarColorFor("alex")).toBe(avatarColorFor("alex"));
    expect(avatarColorFor("pedro")).toBe(avatarColorFor("pedro"));
  });

  it("falls back to the first palette entry when seed is missing", () => {
    expect(avatarColorFor(null)).toBe(AVATAR_PALETTE[0]);
    expect(avatarColorFor(undefined)).toBe(AVATAR_PALETTE[0]);
    expect(avatarColorFor("")).toBe(AVATAR_PALETTE[0]);
  });

  it("spreads usernames across the palette (no clustering on first slot)", () => {
    // Smoke test: 30 distinct usernames should hit at least 6 distinct
    // colours. Tighter than that and we'd need cryptographic guarantees;
    // this just guards against regressions where the hash collapses.
    const usernames = Array.from({ length: 30 }, (_, i) => `user-${i}`);
    const colours = new Set(usernames.map((u) => avatarColorFor(u).background));
    expect(colours.size).toBeGreaterThan(5);
  });
});
