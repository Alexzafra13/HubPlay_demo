import { describe, it, expect, beforeEach } from "vitest";
import { usePlayerStore } from "./player";
import type { StreamSession } from "@/api/types";

const testSession: StreamSession = {
  session_id: "s1",
  session_token: "tok-123",
  playback_method: "transcode",
  master_playlist: "/stream/s1/master.m3u8",
  direct_url: null,
};

describe("usePlayerStore", () => {
  beforeEach(() => {
    usePlayerStore.getState().stopPlayback();
  });

  it("starts idle", () => {
    const s = usePlayerStore.getState();
    expect(s.isPlaying).toBe(false);
    expect(s.currentItemId).toBeNull();
  });

  it("startPlayback sets session info", () => {
    usePlayerStore.getState().startPlayback(testSession, "item-1");

    const s = usePlayerStore.getState();
    expect(s.isPlaying).toBe(true);
    expect(s.currentItemId).toBe("item-1");
    expect(s.sessionToken).toBe("tok-123");
    expect(s.playbackMethod).toBe("transcode");
    expect(s.masterPlaylistUrl).toBe("/stream/s1/master.m3u8");
  });

  it("stopPlayback resets all fields", () => {
    usePlayerStore.getState().startPlayback(testSession, "item-1");
    usePlayerStore.getState().stopPlayback();

    const s = usePlayerStore.getState();
    expect(s.isPlaying).toBe(false);
    expect(s.currentItemId).toBeNull();
    expect(s.sessionToken).toBeNull();
  });

  it("setVolume clamps to [0, 1]", () => {
    usePlayerStore.getState().setVolume(1.5);
    expect(usePlayerStore.getState().volume).toBe(1);

    usePlayerStore.getState().setVolume(-0.5);
    expect(usePlayerStore.getState().volume).toBe(0);

    usePlayerStore.getState().setVolume(0.7);
    expect(usePlayerStore.getState().volume).toBe(0.7);
  });

  it("toggleMute toggles", () => {
    expect(usePlayerStore.getState().isMuted).toBe(false);
    usePlayerStore.getState().toggleMute();
    expect(usePlayerStore.getState().isMuted).toBe(true);
    usePlayerStore.getState().toggleMute();
    expect(usePlayerStore.getState().isMuted).toBe(false);
  });

  it("updateTime sets time fields", () => {
    usePlayerStore.getState().updateTime(30, 120, 60);

    const s = usePlayerStore.getState();
    expect(s.currentTime).toBe(30);
    expect(s.duration).toBe(120);
    expect(s.buffered).toBe(60);
  });
});
