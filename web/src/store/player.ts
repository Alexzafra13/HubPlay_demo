import { create } from 'zustand'
import type { StreamSession, PlaybackMethod } from '@/api/types'

interface PlayerState {
  isPlaying: boolean
  currentItemId: string | null
  currentSessionId: string | null
  sessionToken: string | null
  playbackMethod: PlaybackMethod | null
  masterPlaylistUrl: string | null
  directUrl: string | null
  volume: number
  isMuted: boolean
  isFullscreen: boolean
  currentTime: number
  duration: number
  buffered: number

  startPlayback: (session: StreamSession, itemId: string) => void
  stopPlayback: () => void
  setVolume: (v: number) => void
  toggleMute: () => void
  setFullscreen: (v: boolean) => void
  updateTime: (current: number, duration: number, buffered: number) => void
}

export const usePlayerStore = create<PlayerState>()((set) => ({
  isPlaying: false,
  currentItemId: null,
  currentSessionId: null,
  sessionToken: null,
  playbackMethod: null,
  masterPlaylistUrl: null,
  directUrl: null,
  volume: 1,
  isMuted: false,
  isFullscreen: false,
  currentTime: 0,
  duration: 0,
  buffered: 0,

  startPlayback(session: StreamSession, itemId: string) {
    set({
      isPlaying: true,
      currentItemId: itemId,
      currentSessionId: session.session_id,
      sessionToken: session.session_token,
      playbackMethod: session.playback_method,
      masterPlaylistUrl: session.master_playlist,
      directUrl: session.direct_url,
      currentTime: 0,
      duration: 0,
      buffered: 0,
    })
  },

  stopPlayback() {
    set({
      isPlaying: false,
      currentItemId: null,
      currentSessionId: null,
      sessionToken: null,
      playbackMethod: null,
      masterPlaylistUrl: null,
      directUrl: null,
      currentTime: 0,
      duration: 0,
      buffered: 0,
    })
  },

  setVolume(v: number) {
    set({ volume: Math.max(0, Math.min(1, v)) })
  },

  toggleMute() {
    set((state) => ({ isMuted: !state.isMuted }))
  },

  setFullscreen(v: boolean) {
    set({ isFullscreen: v })
  },

  updateTime(current: number, duration: number, buffered: number) {
    set({ currentTime: current, duration, buffered })
  },
}))
