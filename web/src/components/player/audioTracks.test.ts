import { describe, it, expect } from "vitest";
import {
  buildPickerTracksFromDB,
  channelLabel,
  codecLabel,
  enrichAudioTracks,
  languageLabel,
} from "./audioTracks";

describe("channelLabel", () => {
  it("maps common channel counts to marketing labels", () => {
    expect(channelLabel(1)).toBe("Mono");
    expect(channelLabel(2)).toBe("Stereo");
    expect(channelLabel(6)).toBe("5.1");
    expect(channelLabel(8)).toBe("7.1");
  });
  it("falls through unknown counts to '<n>ch'", () => {
    expect(channelLabel(4)).toBe("4ch");
    expect(channelLabel(12)).toBe("12ch");
  });
  it("returns empty for nullish / non-positive", () => {
    expect(channelLabel(null)).toBe("");
    expect(channelLabel(0)).toBe("");
  });
});

describe("codecLabel", () => {
  it("pretty-prints terse ffprobe codec ids", () => {
    expect(codecLabel("eac3")).toBe("EAC3");
    expect(codecLabel("truehd")).toBe("TrueHD");
    expect(codecLabel("dts_hd")).toBe("DTS-HD");
  });
  it("uppercases unknown codecs", () => {
    expect(codecLabel("xyz")).toBe("XYZ");
  });
});

describe("languageLabel", () => {
  it("renders es names in Spanish UI", () => {
    expect(languageLabel("spa", "es")).toBe("Español");
    expect(languageLabel("eng", "es")).toBe("Inglés");
    expect(languageLabel("rum", "es")).toBe("Rumano");
  });
  it("renders en names in English UI", () => {
    expect(languageLabel("spa", "en")).toBe("Spanish");
    expect(languageLabel("rum", "en")).toBe("Romanian");
  });
  it("collapses ISO 639-1 and 639-2/T to the right canonical entry", () => {
    expect(languageLabel("es", "es")).toBe("Español");
    expect(languageLabel("fra", "es")).toBe("Francés"); // 639-2/T → 639-2/B
    expect(languageLabel("ron", "en")).toBe("Romanian");
  });
  it("strips region/script suffixes before lookup", () => {
    expect(languageLabel("spa-419", "es")).toBe("Español");
    expect(languageLabel("zh-Hant", "en")).toBe("Chinese");
  });
  it("returns empty for nullish / empty input", () => {
    expect(languageLabel(null, "es")).toBe("");
    expect(languageLabel("", "es")).toBe("");
  });
  it("uppercases unknown codes", () => {
    expect(languageLabel("xyz", "es")).toBe("XYZ");
  });
});

describe("buildPickerTracksFromDB", () => {
  it("emits one entry per audio stream with per-type ids", () => {
    const streams = [
      { stream_type: "video", codec: "h264", language: null, title: null, channels: null },
      { stream_type: "audio", codec: "eac3", language: "eng", title: null, channels: 6, is_default: true },
      { stream_type: "audio", codec: "eac3", language: "spa", title: null, channels: 6 },
      { stream_type: "subtitle", codec: "subrip", language: "spa", title: null, channels: null },
      { stream_type: "audio", codec: "eac3", language: "rum", title: null, channels: 6 },
    ];
    const tracks = buildPickerTracksFromDB(streams, "es", "Predeterminado");
    expect(tracks.map((t) => t.id)).toEqual([0, 1, 2]);
    expect(tracks[0].name).toBe("Inglés · EAC3 5.1 · Predeterminado");
    expect(tracks[1].name).toBe("Español · EAC3 5.1");
    expect(tracks[2].name).toBe("Rumano · EAC3 5.1");
  });

  it("falls through to a generic label when language is missing", () => {
    const tracks = buildPickerTracksFromDB(
      [{ stream_type: "audio", codec: "aac", language: null, title: null, channels: 2 }],
      "es",
      "Predeterminado",
    );
    expect(tracks[0].name).toBe("AAC Stereo");
  });

  it("appends the file's title when it carries information the label doesn't", () => {
    const tracks = buildPickerTracksFromDB(
      [{ stream_type: "audio", codec: "eac3", language: "eng", title: "Director's Commentary", channels: 6 }],
      "en",
      "Default",
    );
    expect(tracks[0].name).toBe("English · EAC3 5.1 · Director's Commentary");
  });

  it("does not duplicate the title when it merely repeats the language", () => {
    const tracks = buildPickerTracksFromDB(
      [{ stream_type: "audio", codec: "eac3", language: "spa", title: "Español", channels: 6 }],
      "es",
      "Predeterminado",
    );
    // "Español · EAC3 5.1" — no second "Español"
    expect(tracks[0].name).toBe("Español · EAC3 5.1");
  });

  it("falls back to a numbered placeholder when no fields produce a label", () => {
    const tracks = buildPickerTracksFromDB(
      [{ stream_type: "audio", codec: "", language: null, title: null, channels: null }],
      "es",
      "Predeterminado",
    );
    // codec="" produces "" via codecLabel; language null; title null
    // → only fallback "Audio 1".
    expect(tracks[0].name).toBe("Audio 1");
  });

  it("accepts the legacy `type` field interchangeably with stream_type", () => {
    const tracks = buildPickerTracksFromDB(
      [{ type: "audio", codec: "aac", language: "eng", title: null, channels: 2 }],
      "en",
      "Default",
    );
    expect(tracks).toHaveLength(1);
    expect(tracks[0].name).toBe("English · AAC Stereo");
  });
});

describe("enrichAudioTracks", () => {
  it("leaves the list untouched when there are no DB streams", () => {
    const hls = [{ id: 0, name: "English", lang: "eng" }];
    expect(enrichAudioTracks(hls, [])).toEqual(hls);
  });

  it("enriches by language with positional pairing inside a language", () => {
    const hls = [
      { id: 0, name: "English", lang: "eng" },
      { id: 1, name: "English", lang: "eng" },
    ];
    const db = [
      { index: 0, codec: "aac", language: "eng", title: null, channels: 2 },
      { index: 1, codec: "truehd", language: "eng", title: null, channels: 8 },
    ];
    const out = enrichAudioTracks(hls, db);
    expect(out[0].name).toBe("English · AAC Stereo");
    expect(out[1].name).toBe("English · TrueHD 7.1");
  });
});
