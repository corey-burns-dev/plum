import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import type { MediaItem } from '../api'
import { BASE_URL, startTranscode } from '../api'
import { sortMusicTracks } from '../lib/musicGrouping'

export type PlayerViewMode = 'inline' | 'theatre' | 'fullscreen'
export type MusicRepeatMode = 'off' | 'all' | 'one'

type PlayerContextValue = {
  selectedMedia: MediaItem | null
  setSelectedMedia: (item: MediaItem | null) => void
  playMedia: (item: MediaItem) => void
  playMusicCollection: (items: MediaItem[], startItem?: MediaItem) => void
  musicQueue: MediaItem[]
  musicQueueIndex: number
  musicShuffle: boolean
  musicRepeatMode: MusicRepeatMode
  playNextTrack: () => void
  playPreviousTrack: () => void
  toggleMusicShuffle: () => void
  cycleMusicRepeatMode: () => void
  wsConnected: boolean
  lastEvent: string
  viewMode: PlayerViewMode
  setViewMode: (mode: PlayerViewMode | ((current: PlayerViewMode) => PlayerViewMode)) => void
}

const PlayerContext = createContext<PlayerContextValue | null>(null)

function getWsUrl(): string {
  const base = BASE_URL || ''
  const url = base.startsWith('http') ? new URL(base) : new URL(base, window.location.origin)
  const protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${protocol}//${url.host}${url.pathname.replace(/\/$/, '')}/ws`
}

function shuffleQueue(items: MediaItem[], currentId: number): MediaItem[] {
  const current = items.find((item) => item.id === currentId) ?? items[0]
  const rest = items.filter((item) => item.id !== current?.id)
  for (let index = rest.length - 1; index > 0; index -= 1) {
    const swapIndex = Math.floor(Math.random() * (index + 1))
    ;[rest[index], rest[swapIndex]] = [rest[swapIndex], rest[index]]
  }
  return current ? [current, ...rest] : rest
}

function indexOfTrack(items: MediaItem[], trackId: number): number {
  return items.findIndex((item) => item.id === trackId)
}

export function PlayerProvider({ children }: { children: ReactNode }) {
  const [selectedMedia, setSelectedMedia] = useState<MediaItem | null>(null)
  const [wsConnected, setWsConnected] = useState(false)
  const [lastEvent, setLastEvent] = useState('')
  const [viewMode, setViewMode] = useState<PlayerViewMode>('inline')
  const [musicBaseQueue, setMusicBaseQueue] = useState<MediaItem[]>([])
  const [musicQueue, setMusicQueue] = useState<MediaItem[]>([])
  const [musicQueueIndex, setMusicQueueIndex] = useState(0)
  const [musicShuffle, setMusicShuffle] = useState(false)
  const [musicRepeatMode, setMusicRepeatMode] = useState<MusicRepeatMode>('off')
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectTimeoutRef = useRef<ReturnType<typeof setTimeout>>(0)
  const mountedRef = useRef(true)

  const playVideo = useCallback((item: MediaItem) => {
    setSelectedMedia(item)
    setViewMode('theatre')
    startTranscode(item.id).catch((err) => {
      console.error('[Player] startTranscode failed', err)
      setLastEvent(`Error: ${err instanceof Error ? err.message : 'Failed to start transcode'}`)
    })
  }, [])

  const playMusicCollection = useCallback(
    (items: MediaItem[], startItem?: MediaItem) => {
      const baseQueue = sortMusicTracks(items.filter((item) => item.type === 'music'))
      if (baseQueue.length === 0) return
      const target = startItem ?? baseQueue[0]
      const orderedQueue = musicShuffle ? shuffleQueue(baseQueue, target.id) : baseQueue
      const nextIndex = Math.max(0, indexOfTrack(orderedQueue, target.id))
      setMusicBaseQueue(baseQueue)
      setMusicQueue(orderedQueue)
      setMusicQueueIndex(nextIndex)
      setSelectedMedia(orderedQueue[nextIndex] ?? target)
      setViewMode('inline')
    },
    [musicShuffle]
  )

  const playMedia = useCallback(
    (item: MediaItem) => {
      if (item.type === 'music') {
        playMusicCollection([item], item)
        return
      }
      playVideo(item)
    },
    [playMusicCollection, playVideo]
  )

  const selectMusicIndex = useCallback(
    (nextIndex: number) => {
      if (musicQueue.length === 0) return
      const clampedIndex = Math.max(0, Math.min(nextIndex, musicQueue.length - 1))
      setMusicQueueIndex(clampedIndex)
      setSelectedMedia(musicQueue[clampedIndex] ?? null)
    },
    [musicQueue]
  )

  const playNextTrack = useCallback(() => {
    if (musicQueue.length === 0) return
    const atLastTrack = musicQueueIndex >= musicQueue.length - 1
    if (!atLastTrack) {
      selectMusicIndex(musicQueueIndex + 1)
      return
    }
    if (musicRepeatMode === 'all') {
      selectMusicIndex(0)
    }
  }, [musicQueue.length, musicQueueIndex, musicRepeatMode, selectMusicIndex])

  const playPreviousTrack = useCallback(() => {
    if (musicQueue.length === 0) return
    if (musicQueueIndex > 0) {
      selectMusicIndex(musicQueueIndex - 1)
      return
    }
    if (musicRepeatMode === 'all') {
      selectMusicIndex(musicQueue.length - 1)
    }
  }, [musicQueue.length, musicQueueIndex, musicRepeatMode, selectMusicIndex])

  const toggleMusicShuffle = useCallback(() => {
    setMusicShuffle((current) => {
      const next = !current
      if (musicBaseQueue.length === 0 || !selectedMedia || selectedMedia.type !== 'music') {
        return next
      }
      const reorderedQueue = next
        ? shuffleQueue(musicBaseQueue, selectedMedia.id)
        : musicBaseQueue
      setMusicQueue(reorderedQueue)
      setMusicQueueIndex(Math.max(0, indexOfTrack(reorderedQueue, selectedMedia.id)))
      return next
    })
  }, [musicBaseQueue, selectedMedia])

  const cycleMusicRepeatMode = useCallback(() => {
    setMusicRepeatMode((current) => {
      if (current === 'off') return 'all'
      if (current === 'all') return 'one'
      return 'off'
    })
  }, [])

  useEffect(() => {
    if (!BASE_URL) return
    mountedRef.current = true
    const connect = () => {
      if (!mountedRef.current) return
      const ws = new WebSocket(getWsUrl())
      wsRef.current = ws
      ws.addEventListener('open', () => {
        if (mountedRef.current) setWsConnected(true)
      })
      ws.addEventListener('close', () => {
        if (!mountedRef.current) return
        setWsConnected(false)
        reconnectTimeoutRef.current = setTimeout(connect, 3000)
      })
      ws.addEventListener('message', (event) => {
        if (!mountedRef.current) return
        try {
          const data = JSON.parse(event.data as string)
          if (data.type === 'transcode_started') {
            setLastEvent('Transcoding…')
          } else if (data.type === 'transcode_complete') {
            setLastEvent('Ready')
          } else if (data.type) {
            setLastEvent(data.type)
          }
        } catch {
          setLastEvent(event.data as string)
        }
      })
      ws.addEventListener('error', () => {})
    }
    connect()
    return () => {
      mountedRef.current = false
      clearTimeout(reconnectTimeoutRef.current)
      reconnectTimeoutRef.current = 0
      if (wsRef.current) {
        wsRef.current.close()
        wsRef.current = null
      }
    }
  }, [])

  const value = useMemo<PlayerContextValue>(
    () => ({
      selectedMedia,
      setSelectedMedia,
      playMedia,
      playMusicCollection,
      musicQueue,
      musicQueueIndex,
      musicShuffle,
      musicRepeatMode,
      playNextTrack,
      playPreviousTrack,
      toggleMusicShuffle,
      cycleMusicRepeatMode,
      wsConnected,
      lastEvent,
      viewMode,
      setViewMode,
    }),
    [
      selectedMedia,
      playMedia,
      playMusicCollection,
      musicQueue,
      musicQueueIndex,
      musicShuffle,
      musicRepeatMode,
      playNextTrack,
      playPreviousTrack,
      toggleMusicShuffle,
      cycleMusicRepeatMode,
      wsConnected,
      lastEvent,
      viewMode,
    ]
  )

  return <PlayerContext.Provider value={value}>{children}</PlayerContext.Provider>
}

export function usePlayer() {
  const ctx = useContext(PlayerContext)
  if (!ctx) throw new Error('usePlayer must be used within PlayerProvider')
  return ctx
}
