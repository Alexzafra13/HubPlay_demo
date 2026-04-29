import { describe, it, expect } from "vitest";
import { thumb } from "./imageUrl";

describe("thumb", () => {
  it("returns null for null/undefined/empty", () => {
    expect(thumb(null, 300)).toBeNull();
    expect(thumb(undefined, 300)).toBeNull();
    expect(thumb("", 300)).toBeNull();
  });

  it("leaves external URLs alone", () => {
    expect(thumb("https://image.tmdb.org/t/p/w500/foo.jpg", 300)).toBe(
      "https://image.tmdb.org/t/p/w500/foo.jpg",
    );
    expect(thumb("data:image/png;base64,abc", 300)).toBe(
      "data:image/png;base64,abc",
    );
  });

  it("appends ?w= to a bare HubPlay path", () => {
    expect(thumb("/api/v1/images/file/abc", 300)).toBe(
      "/api/v1/images/file/abc?w=300",
    );
  });

  it("appends &w= when other query params are already present", () => {
    expect(thumb("/api/v1/images/file/abc?other=1", 480)).toBe(
      "/api/v1/images/file/abc?other=1&w=480",
    );
  });

  it("replaces an existing trailing w= rather than duplicating it", () => {
    expect(thumb("/api/v1/images/file/abc?w=200", 480)).toBe(
      "/api/v1/images/file/abc?w=480",
    );
  });

  it("replaces an existing middle w= rather than duplicating it", () => {
    expect(thumb("/api/v1/images/file/abc?w=200&other=1", 480)).toBe(
      "/api/v1/images/file/abc?other=1&w=480",
    );
  });

  it("works when the HubPlay path has a host prefix (absolute URL)", () => {
    expect(
      thumb("https://hubplay.example.com/api/v1/images/file/abc", 480),
    ).toBe("https://hubplay.example.com/api/v1/images/file/abc?w=480");
  });
});
