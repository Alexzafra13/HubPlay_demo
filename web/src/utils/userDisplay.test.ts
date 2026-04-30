import { describe, it, expect } from "vitest";
import { getInitials } from "./userDisplay";

describe("getInitials", () => {
  it("uses display_name first letters, capped at 2", () => {
    expect(getInitials({ display_name: "Alex Zafra", username: "alex" })).toBe("AZ");
    expect(getInitials({ display_name: "Alejandro Garcia Soto", username: "alex" })).toBe("AG");
    expect(getInitials({ display_name: "Pedro", username: "pedro" })).toBe("P");
  });

  it("falls back to username when display_name is empty", () => {
    expect(getInitials({ display_name: "", username: "operator" })).toBe("OP");
  });

  it("uppercases the result", () => {
    expect(getInitials({ display_name: "alex zafra", username: "alex" })).toBe("AZ");
    expect(getInitials({ display_name: "", username: "abc" })).toBe("AB");
  });

  it("collapses runs of whitespace", () => {
    expect(getInitials({ display_name: "Alex   Zafra", username: "alex" })).toBe("AZ");
    expect(getInitials({ display_name: "  Alex  ", username: "alex" })).toBe("A");
  });

  it("returns '?' when user is null/undefined", () => {
    expect(getInitials(null)).toBe("?");
    expect(getInitials(undefined)).toBe("?");
  });

  it("returns '?' when both display_name and username are missing", () => {
    expect(getInitials({ display_name: "", username: "" })).toBe("?");
  });
});
