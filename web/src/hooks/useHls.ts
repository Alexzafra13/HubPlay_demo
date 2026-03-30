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
  currentAudioTrack: number;
  currentSubtitleTrack: number;
  setAudioTrack: (id: number) => void;
  setSubtitleTrack: (id: number) => void;
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
  const [currentAudioTrack, setCurrentAudioTrack] = useState(0);
  const [currentSubtitleTrack, setCurrentSubtitleTrack] = useState(-1);

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
            xhr.withCredentials = false;
            const accessToken = localStorage.getItem("hubplay_access_token");
            if (accessToken) {
              xhr.setRequestHeader("Authorization", `Bearer ${accessToken}`);
            }
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
          video.play().catch(() => {});
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
      const accessToken = localStorage.getItem("hubplay_access_token");
      const authUrl = accessToken
        ? `${directUrl}${directUrl.includes("?") ? "&" : "?"}token=${accessToken}`
        : directUrl;
      video.src = authUrl;
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
    currentAudioTrack,
    currentSubtitleTrack,
    setAudioTrack: setAudioTrackCb,
    setSubtitleTrack: setSubtitleTrackCb,
  };
}
