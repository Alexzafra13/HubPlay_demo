import { useCallback, useEffect, useRef, useState } from 'react'
import type { RefObject } from 'react'
import Hls from 'hls.js'
import { usePlayerStore } from '@/store/player'

const PROGRESS_INTERVAL_MS = 10_000

interface UsePlayerOptions {
  videoRef: RefObject<HTMLVideoElement | null>
}

interface UsePlayerReturn {
  play: () => void
  pause: () => void
  togglePlay: () => void
  seek: (time: number) => void
  setVolume: (v: number) => void
  toggleMute: () => void
  toggleFullscreen: () => void
  isReady: boolean
  error: string | null
}

export function usePlayer({ videoRef }: UsePlayerOptions): UsePlayerReturn {
  const hlsRef = useRef<Hls | null>(null)
  const progressTimerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const [isReady, setIsReady] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const {
    masterPlaylistUrl,
    directUrl,
    playbackMethod,
    sessionToken,
    currentSessionId,
    currentItemId,
    volume,
    isMuted,
  } = usePlayerStore()

  const storeSetVolume = usePlayerStore((s) => s.setVolume)
  const storeToggleMute = usePlayerStore((s) => s.toggleMute)
  const storeSetFullscreen = usePlayerStore((s) => s.setFullscreen)
  const storeUpdateTime = usePlayerStore((s) => s.updateTime)

  // ── HLS lifecycle ──────────────────────────────────────────────────────────

  useEffect(() => {
    const video = videoRef.current
    if (!video) return

    setIsReady(false)
    setError(null)

    // Direct play — set src directly
    if (playbackMethod === 'direct_play' && directUrl) {
      video.src = directUrl
      video.load()
      setIsReady(true)
      return
    }

    // HLS playback
    if (!masterPlaylistUrl) return

    if (Hls.isSupported()) {
      const hls = new Hls({
        xhrSetup(xhr) {
          if (sessionToken) {
            xhr.setRequestHeader('X-Stream-Token', sessionToken)
          }
        },
      })

      hls.loadSource(masterPlaylistUrl)
      hls.attachMedia(video)

      hls.on(Hls.Events.MANIFEST_PARSED, () => {
        setIsReady(true)
      })

      hls.on(Hls.Events.ERROR, (_event, data) => {
        if (data.fatal) {
          setError(`Playback error: ${data.details}`)
          if (data.type === Hls.ErrorTypes.NETWORK_ERROR) {
            hls.startLoad()
          } else if (data.type === Hls.ErrorTypes.MEDIA_ERROR) {
            hls.recoverMediaError()
          } else {
            hls.destroy()
          }
        }
      })

      hlsRef.current = hls
    } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
      // Native HLS support (Safari)
      video.src = masterPlaylistUrl
      video.load()
      setIsReady(true)
    } else {
      setError('HLS is not supported in this browser')
    }

    return () => {
      if (hlsRef.current) {
        hlsRef.current.destroy()
        hlsRef.current = null
      }
    }
  }, [videoRef, masterPlaylistUrl, directUrl, playbackMethod, sessionToken])

  // ── Sync volume / mute to video element ────────────────────────────────────

  useEffect(() => {
    const video = videoRef.current
    if (!video) return
    video.volume = volume
    video.muted = isMuted
  }, [videoRef, volume, isMuted])

  // ── Time tracking ─────────────────────────────────────────────────────────

  useEffect(() => {
    const video = videoRef.current
    if (!video) return

    function handleTimeUpdate() {
      if (!video) return
      const buf = video.buffered
      const bufferedEnd = buf.length > 0 ? buf.end(buf.length - 1) : 0
      storeUpdateTime(video.currentTime, video.duration || 0, bufferedEnd)
    }

    video.addEventListener('timeupdate', handleTimeUpdate)
    return () => {
      video.removeEventListener('timeupdate', handleTimeUpdate)
    }
  }, [videoRef, storeUpdateTime])

  // ── Progress reporting (every 10s) ─────────────────────────────────────────

  useEffect(() => {
    if (!currentSessionId || !currentItemId) return

    progressTimerRef.current = setInterval(() => {
      const video = videoRef.current
      if (!video || video.paused) return

      const positionTicks = Math.round(video.currentTime * 10_000_000)

      void reportProgress(currentSessionId, currentItemId, positionTicks)
    }, PROGRESS_INTERVAL_MS)

    return () => {
      if (progressTimerRef.current) {
        clearInterval(progressTimerRef.current)
        progressTimerRef.current = null
      }
    }
  }, [videoRef, currentSessionId, currentItemId])

  // ── Controls ──────────────────────────────────────────────────────────────

  const play = useCallback(() => {
    void videoRef.current?.play()
  }, [videoRef])

  const pause = useCallback(() => {
    videoRef.current?.pause()
  }, [videoRef])

  const togglePlay = useCallback(() => {
    const video = videoRef.current
    if (!video) return
    if (video.paused) {
      void video.play()
    } else {
      video.pause()
    }
  }, [videoRef])

  const seek = useCallback(
    (time: number) => {
      const video = videoRef.current
      if (!video) return
      video.currentTime = Math.max(0, Math.min(time, video.duration || 0))
    },
    [videoRef],
  )

  const setVolume = useCallback(
    (v: number) => {
      storeSetVolume(v)
    },
    [storeSetVolume],
  )

  const toggleMute = useCallback(() => {
    storeToggleMute()
  }, [storeToggleMute])

  const toggleFullscreen = useCallback(() => {
    const video = videoRef.current
    if (!video) return

    const container = video.parentElement
    if (!container) return

    if (document.fullscreenElement) {
      void document.exitFullscreen()
      storeSetFullscreen(false)
    } else {
      void container.requestFullscreen()
      storeSetFullscreen(true)
    }
  }, [videoRef, storeSetFullscreen])

  return {
    play,
    pause,
    togglePlay,
    seek,
    setVolume,
    toggleMute,
    toggleFullscreen,
    isReady,
    error,
  }
}

// ── Helpers ────────────────────────────────────────────────────────────────────

async function reportProgress(
  _sessionId: string,
  itemId: string,
  positionTicks: number,
): Promise<void> {
  try {
    // Dynamic import to avoid circular dependencies; the api module will be
    // created separately and is expected to expose an `updateProgress` function.
    const { api } = await import('@/api/client')
    await api.updateProgress(itemId, { position_ticks: positionTicks })
  } catch {
    // Silently ignore progress reporting failures — they are non-critical.
  }
}
