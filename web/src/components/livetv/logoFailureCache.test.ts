import { describe, it, expect, beforeEach } from "vitest";
import {
  hasLogoFailed,
  markLogoFailed,
  __resetLogoFailureCache,
} from "./logoFailureCache";

describe("logoFailureCache", () => {
  beforeEach(() => {
    __resetLogoFailureCache();
  });

  it("reports a URL as failed only after it is marked", () => {
    const url = "/api/v1/channels/abc/logo";
    expect(hasLogoFailed(url)).toBe(false);
    markLogoFailed(url);
    expect(hasLogoFailed(url)).toBe(true);
  });

  it("keys on the exact URL so a different channel is unaffected", () => {
    markLogoFailed("/api/v1/channels/abc/logo");
    expect(hasLogoFailed("/api/v1/channels/xyz/logo")).toBe(false);
  });

  it("treats null / undefined / empty as never-failed and ignores marking them", () => {
    expect(hasLogoFailed(null)).toBe(false);
    expect(hasLogoFailed(undefined)).toBe(false);
    expect(hasLogoFailed("")).toBe(false);
    // Marking a falsy URL is a no-op (no throw, nothing remembered).
    markLogoFailed(null);
    markLogoFailed("");
    expect(hasLogoFailed("")).toBe(false);
  });

  it("reset clears remembered failures", () => {
    const url = "/api/v1/channels/abc/logo";
    markLogoFailed(url);
    expect(hasLogoFailed(url)).toBe(true);
    __resetLogoFailureCache();
    expect(hasLogoFailed(url)).toBe(false);
  });
});
