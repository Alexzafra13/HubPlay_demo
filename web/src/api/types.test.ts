import { describe, it, expect } from "vitest";
import { ApiError } from "./types";

describe("ApiError", () => {
  it("creates error with status, code, and message", () => {
    const err = new ApiError(404, {
      error: { code: "not_found", message: "Item not found" },
    });

    expect(err).toBeInstanceOf(Error);
    expect(err.name).toBe("ApiError");
    expect(err.status).toBe(404);
    expect(err.code).toBe("not_found");
    expect(err.message).toBe("Item not found");
  });

  it("includes details when provided", () => {
    const err = new ApiError(422, {
      error: {
        code: "validation",
        message: "Invalid",
        details: { field: "name" },
      },
    });

    expect(err.details).toEqual({ field: "name" });
  });
});
