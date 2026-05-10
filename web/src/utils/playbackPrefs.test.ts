import { describe, it, expect } from "vitest";
import { normaliseLanguage, pickAudioStreamIndex } from "./playbackPrefs";

describe("normaliseLanguage", () => {
  it("returns empty for nullish / empty input", () => {
    expect(normaliseLanguage(null)).toBe("");
    expect(normaliseLanguage(undefined)).toBe("");
    expect(normaliseLanguage("")).toBe("");
  });

  it("collapses ISO 639-1 to ISO 639-2/B", () => {
    expect(normaliseLanguage("es")).toBe("spa");
    expect(normaliseLanguage("EN")).toBe("eng");
    expect(normaliseLanguage("ja")).toBe("jpn");
    expect(normaliseLanguage("ro")).toBe("rum");
  });

  it("collapses 639-2/T to 639-2/B (the picker's canonical form)", () => {
    expect(normaliseLanguage("fra")).toBe("fre");
    expect(normaliseLanguage("deu")).toBe("ger");
    expect(normaliseLanguage("zho")).toBe("chi");
    expect(normaliseLanguage("ron")).toBe("rum");
  });

  it("strips region/script suffixes", () => {
    expect(normaliseLanguage("spa-419")).toBe("spa");
    expect(normaliseLanguage("en-GB")).toBe("eng");
    expect(normaliseLanguage("zh-Hant")).toBe("chi");
    expect(normaliseLanguage("pt_BR")).toBe("por");
  });

  it("passes through unknown codes lowercased", () => {
    // Unknown 3-letter code stays as-is so the comparison is at
    // least deterministic — never mistake it for a known one.
    expect(normaliseLanguage("xyz")).toBe("xyz");
    expect(normaliseLanguage("ASM")).toBe("asm");
  });
});

describe("pickAudioStreamIndex", () => {
  it("returns -1 when there is no preference", () => {
    expect(
      pickAudioStreamIndex(
        [{ stream_type: "audio", language: "spa" }],
        "",
      ),
    ).toBe(-1);
  });

  it("returns -1 when streams is nullish", () => {
    expect(pickAudioStreamIndex(undefined, "spa")).toBe(-1);
    expect(pickAudioStreamIndex(null, "spa")).toBe(-1);
  });

  it("counts only audio streams when computing the per-type index", () => {
    const streams = [
      { stream_type: "video", language: null },
      { stream_type: "audio", language: "eng" }, // index 0
      { stream_type: "audio", language: "spa" }, // index 1
      { stream_type: "subtitle", language: "spa" },
      { stream_type: "audio", language: "rum" }, // index 2
    ];
    expect(pickAudioStreamIndex(streams, "spa")).toBe(1);
    expect(pickAudioStreamIndex(streams, "rum")).toBe(2);
    expect(pickAudioStreamIndex(streams, "eng")).toBe(0);
  });

  it("matches an ISO 639-1 file tag against an ISO 639-2 preference", () => {
    // Real-world rip: encoder wrote "es" instead of MKV-canonical
    // "spa". Without the alias map the user's "spa" preference
    // never matched and the file's default audio (English) played.
    expect(
      pickAudioStreamIndex(
        [
          { stream_type: "audio", language: "en" },
          { stream_type: "audio", language: "es" },
        ],
        "spa",
      ),
    ).toBe(1);
  });

  it("matches a region-tagged file tag (spa-419) against bare spa", () => {
    expect(
      pickAudioStreamIndex(
        [
          { stream_type: "audio", language: "eng" },
          { stream_type: "audio", language: "spa-419" },
        ],
        "spa",
      ),
    ).toBe(1);
  });

  it("matches the 639-2/T variant (fra) against the picker's fre", () => {
    expect(
      pickAudioStreamIndex(
        [
          { stream_type: "audio", language: "eng" },
          { stream_type: "audio", language: "fra" },
        ],
        "fre",
      ),
    ).toBe(1);
  });

  it("returns -1 when the preferred language has no track in the file", () => {
    expect(
      pickAudioStreamIndex(
        [
          { stream_type: "audio", language: "eng" },
          { stream_type: "audio", language: "spa" },
        ],
        "jpn",
      ),
    ).toBe(-1);
  });

  it("falls through to the legacy `type` field when stream_type is absent", () => {
    expect(
      pickAudioStreamIndex(
        [
          { type: "audio", language: "eng" },
          { type: "audio", language: "spa" },
        ],
        "spa",
      ),
    ).toBe(1);
  });
});
