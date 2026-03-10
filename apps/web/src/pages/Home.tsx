import { useCallback, useEffect, useRef, useState } from 'react'
import {
  fetchLibraryMedia,
  listLibraries,
  startTranscode,
  type Library,
  type MediaItem,
} from '../api'
import { useAuthActions, useAuthState } from '../contexts/AuthContext'
import { PlayerPanel } from '../components/PlayerPanel'
import { Login } from './Login'

const LIBRARY_MEDIA_TIMEOUT_MS = 15_000

function fetchLibraryMediaWithTimeout(id: number): Promise<MediaItem[]> {
  return Promise.race([
    fetchLibraryMedia(id),
    new Promise<never>((_, reject) =>
      setTimeout(() => reject(new Error('Request timed out')), LIBRARY_MEDIA_TIMEOUT_MS)
    ),
  ])
}

type WsEvent =
  | { type: 'welcome'; message: string }
  | { type: 'pong' }
  | { type: 'transcode_started'; id: number }
  | { type: 'transcode_complete'; id: number; output?: string; elapsed?: number }
  | { type: string; [k: string]: unknown }

function getShowName(title: string): string {
  const match = title.match(/^(.+?)\s*-\s*S\d+/i)
  return match ? match[1].trim() : title
}

/** Display label for library tab: Movies, TV, Anime, or Music based on type and name. */
function getLibraryTabLabel(lib: Library): string {
  if (lib.type === 'movie') return 'Movies'
  if (lib.type === 'music') return 'Music'
  if (lib.type === 'anime' || (lib.type === 'tv' && /anime/i.test(lib.name))) return 'Anime'
  if (lib.type === 'tv') return 'TV'
  return lib.name
}

function groupMediaByShow(items: MediaItem[]): Map<string, MediaItem[]> {
  const map = new Map<string, MediaItem[]>()
  for (const m of items) {
    const show = getShowName(m.title)
    const list = map.get(show) ?? []
    list.push(m)
    map.set(show, list)
  }
  for (const list of map.values()) {
    list.sort((a, b) => a.title.localeCompare(b.title))
  }
  return map
}

export function Home() {
  const { user } = useAuthState()
  const { logout } = useAuthActions()
  const [libraries, setLibraries] = useState<Library[]>([])
  const [selectedLibraryId, setSelectedLibraryId] = useState<number | null>(null)
  const [mediaByLibrary, setMediaByLibrary] = useState<Record<number, MediaItem[]>>({})
  const [loadingLibs, setLoadingLibs] = useState(true)
  const [loadLibsError, setLoadLibsError] = useState<string | null>(null)
  const [loadingMedia, setLoadingMedia] = useState<number | null>(null)
  const [errorByLibrary, setErrorByLibrary] = useState<Record<number, string>>({})
  const [selected, setSelected] = useState<MediaItem | undefined>()
  const [wsConnected, setWsConnected] = useState(false)
  const [lastEvent, setLastEvent] = useState<string | undefined>()
  const selectedLibraryIdRef = useRef<number | null>(null)
  selectedLibraryIdRef.current = selectedLibraryId

  const loadLibraryMedia = useCallback((id: number) => {
    setLoadingMedia(id)
    setErrorByLibrary((prev) => {
      const next = { ...prev }
      delete next[id]
      return next
    })
    console.log('[Home] Loading library media…', { libraryId: id })
    fetchLibraryMediaWithTimeout(id)
      .then((items) => {
        console.log('[Home] Library media loaded', { libraryId: id, count: items.length })
        if (selectedLibraryIdRef.current === id) {
          setMediaByLibrary((prev) => ({ ...prev, [id]: items }))
        }
      })
      .catch((err) => {
        console.error('[Home] Failed to load library media', { libraryId: id, err })
        if (err instanceof Error) {
          console.error('[Home] Error name:', err.name, 'message:', err.message, 'stack:', err.stack)
        }
        const message = err instanceof Error ? err.message : 'Failed to load'
        setErrorByLibrary((prev) => ({ ...prev, [id]: message }))
      })
      .finally(() => {
        setLoadingMedia((current) => (current === id ? null : current))
      })
  }, [])

  useEffect(() => {
    let cancelled = false
    setLoadLibsError(null)
    console.log('[Home] Loading libraries…', { baseUrl: import.meta.env.VITE_BACKEND_URL })
    listLibraries()
      .then((list) => {
        if (!cancelled) {
          console.log('[Home] Libraries loaded', { count: list.length, libraries: list })
          setLibraries(list)
          setSelectedLibraryId((prev) => (prev === null && list.length > 0 ? list[0].id : prev))
        }
      })
      .catch((err) => {
        console.error('[Home] Failed to load libraries', err)
        if (err instanceof Error) {
          console.error('[Home] Error name:', err.name, 'message:', err.message, 'stack:', err.stack)
        }
        if (!cancelled) {
          const message = err instanceof Error ? err.message : 'Failed to load libraries.'
          setLoadLibsError(message)
        }
      })
      .finally(() => {
        if (!cancelled) setLoadingLibs(false)
      })
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    if (selectedLibraryId !== null && mediaByLibrary[selectedLibraryId] === undefined) {
      loadLibraryMedia(selectedLibraryId)
    }
  }, [selectedLibraryId, mediaByLibrary, loadLibraryMedia])

  useEffect(() => {
    const wsUrl =
      (import.meta.env.VITE_WS_URL as string | undefined) ||
      (location.protocol === 'https:' ? 'wss://' : 'ws://') + location.host + '/ws'
    const ws = new WebSocket(wsUrl)
    ws.addEventListener('open', () => setWsConnected(true))
    ws.addEventListener('close', () => setWsConnected(false))
    ws.addEventListener('error', () => setWsConnected(false))
    ws.addEventListener('message', (event) => {
      try {
        const data = JSON.parse(event.data) as WsEvent
        if (data.type === 'welcome') setLastEvent('Connected.')
        else if (data.type === 'transcode_started') setLastEvent(`Transcode started for id=${data.id}`)
        else if (data.type === 'transcode_complete')
          setLastEvent(`Transcode complete for id=${data.id}`)
      } catch {
        // ignore
      }
    })
    return () => ws.close()
  }, [])

  const handlePlay = useCallback(async (item: MediaItem) => {
    setSelected(item)
    try {
      await startTranscode(item.id)
      setLastEvent(`Requested transcode for "${item.title}"`)
    } catch (err) {
      console.error(err)
      setLastEvent('Failed to start transcode')
    }
  }, [])

  const selectLibrary = useCallback((id: number) => {
    setSelectedLibraryId(id)
  }, [])

  const selectedLib = libraries.find((l) => l.id === selectedLibraryId)
  const selectedItems = selectedLibraryId !== null ? (mediaByLibrary[selectedLibraryId] ?? []) : []
  const selectedLoading = selectedLibraryId !== null && loadingMedia === selectedLibraryId
  const selectedError = selectedLibraryId !== null ? errorByLibrary[selectedLibraryId] : undefined
  const selectedGrouped = groupMediaByShow(selectedItems)

  return (
    <div className="app-root">
      <header className="app-header">
        <div className="brand-mark">
          <div className="brand-glyph" />
          <div className="brand-text">
            <div className="brand-title">Plum</div>
            <div className="brand-sub">
              {user?.email ? (
                <>
                  {user.email} ·{' '}
                  <button
                    type="button"
                    className="link-button"
                    onClick={() => logout()}
                  >
                    Sign out
                  </button>
                </>
              ) : (
                'Guest mode'
              )}
            </div>
          </div>
        </div>
      </header>
      <main className="app-main">
        <section className="app-content">
          {loadingLibs ? (
            <p className="auth-muted">Loading libraries…</p>
          ) : loadLibsError ? (
            <p className="auth-muted">
              Failed to load libraries: {loadLibsError}{' '}
              <button
                type="button"
                className="link-button"
                onClick={() => {
                  // Re-trigger initial load flow
                  setLoadingLibs(true)
                  setLoadLibsError(null)
                  listLibraries()
                    .then((list) => {
                      setLibraries(list)
                      setSelectedLibraryId((prev) =>
                        prev === null && list.length > 0 ? list[0].id : prev,
                      )
                    })
                    .catch((err) => {
                      console.error(err)
                      const message =
                        err instanceof Error ? err.message : 'Failed to load libraries.'
                      setLoadLibsError(message)
                    })
                    .finally(() => {
                      setLoadingLibs(false)
                    })
                }}
              >
                Retry
              </button>
            </p>
          ) : libraries.length === 0 ? (
            <p className="auth-muted">
              No libraries yet. Once your server can talk to the library API, they’ll appear here.
            </p>
          ) : (
            <>
              <div className="library-tabs" role="tablist">
                {libraries.map((lib) => (
                  <button
                    key={lib.id}
                    type="button"
                    role="tab"
                    aria-selected={selectedLibraryId === lib.id}
                    className={`library-tab ${selectedLibraryId === lib.id ? 'active' : ''}`}
                    onClick={() => selectLibrary(lib.id)}
                  >
                    {getLibraryTabLabel(lib)}
                  </button>
                ))}
              </div>
              {selectedLib && (
                <div className="library-content">
                  {selectedLoading ? (
                    <p className="auth-muted">Loading…</p>
                  ) : selectedError ? (
                    <p className="auth-muted">
                      {selectedError}{' '}
                      <button
                        type="button"
                        className="link-button"
                        onClick={() => loadLibraryMedia(selectedLibraryId!)}
                      >
                        Retry
                      </button>
                    </p>
                  ) : selectedItems.length === 0 ? (
                    <p className="auth-muted">No media in this library yet.</p>
                  ) : (
                    <div className="shows-list">
                      {Array.from(selectedGrouped.entries()).map(([showName, episodes]) => (
                        <div key={showName} className="show-group">
                          <div className="show-name">{showName}</div>
                          <ul className="episodes-list">
                            {episodes.map((ep) => (
                              <li key={ep.id} className="episode-row">
                                <span className="episode-title" title={ep.title}>
                                  {ep.title}
                                </span>
                                <button
                                  type="button"
                                  className="play-button small"
                                  onClick={() => handlePlay(ep)}
                                >
                                  Play
                                </button>
                              </li>
                            ))}
                          </ul>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              )}
            </>
          )}
        </section>
        <PlayerPanel
          selected={selected}
          wsConnected={wsConnected}
          lastEvent={lastEvent}
        />
      </main>
    </div>
  )
}
