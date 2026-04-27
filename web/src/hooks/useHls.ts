import { useEffect, useRef, useState, useCallback } from "react";
import type { RefObject } from "react";
import Hls from "hls.js";

export interface AudioTrack {
  id: number;
  name: string;
  lang: string;
}

export interface SubtitleTrack {
  id: number;
  name: string;
  lang: string;
}

export interface QualityLevel {
  id: number;        // index into hls.levels
  height: number;    // 1080, 720, ...
  bitrate: number;   // bits/sec
  label: string;     // "1080p", "720p", "Source"
}

interface UseHlsOptions {
  videoRef: RefObject<HTMLVideoElement | null>;
  masterPlaylistUrl: string | null;
  directUrl: string | null;
  playbackMethod: string;
  sessionToken: string;
  startPosition?: number;
}

interface UseHlsReturn {
  error: string | null;
  audioTracks: AudioTrack[];
  subtitleTracks: SubtitleTrack[];
  qualityLevels: QualityLevel[];
  currentAudioTrack: number;
  currentSubtitleTrack: number;
  /** -1 = auto (ABR). Otherwise an index into qualityLevels. */
  currentQuality: number;
  setAudioTrack: (id: number) => void;
  setSubtitleTrack: (id: number) => void;
  setQuality: (id: number) => void;
}

export function useHls({
  videoRef,
  masterPlaylistUrl,
  directUrl,
  playbackMethod,
  sessionToken,
  startPosition,
}: UseHlsOptions): UseHlsReturn {
  const hlsRef = useRef<Hls | null>(null);

  const [error, setError] = useState<string | null>(null);
  const [audioTracks, setAudioTracks] = useState<AudioTrack[]>([]);
  const [subtitleTracks, setSubtitleTracks] = useState<SubtitleTrack[]>([]);
  const [qualityLevels, setQualityLevels] = useState<QualityLevel[]>([]);
  const [currentAudioTrack, setCurrentAudioTrack] = useState(0);
  const [currentSubtitleTrack, setCurrentSubtitleTrack] = useState(-1);
  // hls.js convention: -1 means ABR / auto. We expose the same value
  // so the UI doesn't have to translate between "Auto" and a magic
  // index — selecting "Auto" sets it back to -1.
  const [currentQuality, setCurrentQuality] = useState(-1);

  const setAudioTrackCb = useCallback((id: number) => {
    const hls = hlsRef.current;
    if (hls) {
      hls.audioTrack = id;
      setCurrentAudioTrack(id);
    }
  }, []);

  const setSubtitleTrackCb = useCallback((id: number) => {
    const hls = hlsRef.current;
    if (hls) {
      hls.subtitleTrack = id;
      setCurrentSubtitleTrack(id);
    }
  }, []);

  const setQualityCb = useCallback((id: number) => {
    const hls = hlsRef.current;
    if (!hls) return;
    // hls.js: writing -1 to currentLevel re-enables ABR; any
    // non-negative value pins the player to that ladder rung. The
    // local mirror is updated optimistically; LEVEL_SWITCHED will
    // refine to the actual level the engine settled on.
    hls.currentLevel = id;
    setCurrentQuality(id);
  }, []);

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;

    const useHlsPlayback =
      playbackMethod === "transcode" || playbackMethod === "direct_stream";

    if (useHlsPlayback && masterPlaylistUrl) {
      const url = sessionToken
        ? `${masterPlaylistUrl}${masterPlaylistUrl.includes("?") ? "&" : "?"}token=${sessionToken}`
        : masterPlaylistUrl;

      if (Hls.isSupported()) {
        const hls = new Hls({
          enableWorker: true,
          lowLatencyMode: false,
          startPosition: startPosition ?? -1,
          xhrSetup: (xhr) => {
            // Auth is handled via HTTP-only cookies.
            xhr.withCredentials = true;
          },
        });

        hlsRef.current = hls;
        hls.loadSource(url);
        hls.attachMedia(video);

        hls.on(Hls.Events.MANIFEST_PARSED, () => {
          const aTracks: AudioTrack[] = hls.audioTracks.map((t) => ({
            id: t.id,
            name: t.name,
            lang: t.lang || "",
          }));
          setAudioTracks(aTracks);
          setCurrentAudioTrack(hls.audioTrack);

          // Quality ladder. We expose the levels exactly once (the
          // master playlist is parsed before the first segment plays)
          // and rely on LEVEL_SWITCHED to keep currentQuality in sync
          // when ABR or the user moves between rungs.
          const levels: QualityLevel[] = hls.levels.map((l, idx) => ({
            id: idx,
            height: l.height,
            bitrate: l.bitrate,
            label: l.height > 0 ? `${l.height}p` : `${Math.round(l.bitrate / 1000)} kbps`,
          }));
          setQualityLevels(levels);
          setCurrentQuality(hls.currentLevel); // -1 unless the engine pre-pinned

          video.play().catch(() => {});
        });

        hls.on(Hls.Events.LEVEL_SWITCHED, (_event, data) => {
          // Mirror hls.js's "we are now on level N" event. Note that
          // even in ABR mode (currentLevel = -1) the engine still
          // emits this with the concrete level it picked. We keep
          // the user's selection (-1 = auto) by reading it back from
          // hls.autoLevelEnabled rather than trusting data.level.
          if (hls.autoLevelEnabled) {
            setCurrentQuality(-1);
          } else {
            setCurrentQuality(data.level);
          }
        });

        hls.on(Hls.Events.SUBTITLE_TRACKS_UPDATED, () => {
          const sTracks: SubtitleTrack[] = hls.subtitleTracks.map((t) => ({
            id: t.id,
            name: t.name,
            lang: t.lang || "",
          }));
          setSubtitleTracks(sTracks);
          setCurrentSubtitleTrack(hls.subtitleTrack);
        });

        hls.on(Hls.Events.AUDIO_TRACK_SWITCHED, (_event, data) => {
          setCurrentAudioTrack(data.id);
        });

        hls.on(Hls.Events.SUBTITLE_TRACK_SWITCH, (_event, data) => {
          setCurrentSubtitleTrack(data.id);
        });

        hls.on(Hls.Events.ERROR, (_event, data) => {
          if (data.fatal) {
            switch (data.type) {
              case Hls.ErrorTypes.NETWORK_ERROR:
                setError("A network error occurred. Attempting to recover...");
                hls.startLoad();
                break;
              case Hls.ErrorTypes.MEDIA_ERROR:
                setError("A media error occurred. Attempting to recover...");
                hls.recoverMediaError();
                break;
              default:
                setError(`Playback failed: ${data.details}`);
                hls.destroy();
                break;
            }
          }
        });
      } else if (video.canPlayType("application/vnd.apple.mpegurl")) {
        video.src = url;
        video.addEventListener(
          "loadedmetadata",
          () => {
            video.play().catch(() => {});
          },
          { once: true },
        );
      } else {
        setError("HLS playback is not supported in this browser.");
      }
    } else if (playbackMethod === "direct_play" && directUrl) {
      // Auth is handled via HTTP-only cookies for same-origin requests.
      video.src = directUrl;
      video.addEventListener(
        "loadedmetadata",
        () => {
          video.play().catch(() => {});
        },
        { once: true },
      );
    } else {
      setError("No playback source available.");
    }

    return () => {
      if (hlsRef.current) {
        hlsRef.current.destroy();
        hlsRef.current = null;
      }
    };
    // Only run on mount
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return {
    error,
    audioTracks,
    subtitleTracks,
    qualityLevels,
    currentAudioTrack,
    currentSubtitleTrack,
    currentQuality,
    setAudioTrack: setAudioTrackCb,
    setSubtitleTrack: setSubtitleTrackCb,
    setQuality: setQualityCb,
  };
}
