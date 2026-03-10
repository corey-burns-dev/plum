import { useEffect, useMemo, useState } from 'react'
import './App.css'
import { fetchMediaList, scanLibrary, startTranscode, type MediaItem } from './api'
import { MediaList } from './components/MediaList'
import { PlayerPanel } from './components/PlayerPanel'

type WsEvent =
  | { type: 'welcome'; message: string }
  | { type: 'pong' }
  | { type: 'transcode_started'; id: number }
  | { type: 'transcode_complete'; id: number; output?: string; elapsed?: number }
  | { type: string; [k: string]: unknown }

function App() {
  const [media, setMedia] = useState<MediaItem[]>([])
  const [selected, setSelected] = useState<MediaItem>()
  const [wsConnected, setWsConnected] = useState(false)
  const [lastEvent, setLastEvent] = useState<string>()
  const [debugLines, setDebugLines] = useState<string[]>([])

  const pushDebug = (line: string) => {
    const stamped = `${new Date().toLocaleTimeString()} · ${line}`
    setDebugLines((prev) => [stamped, ...prev].slice(0, 50))
  }

  useEffect(() => {
    pushDebug('Fetching initial media list…')
    ;(async () => {
      try {
        const items = await fetchMediaList()
        setMedia(items)
        pushDebug(`Loaded ${items.length} media items`)
      } catch (err) {
        console.error(err)
        pushDebug(`Error loading media: ${String(err)}`)
      }
    })()
  }, [])

  useEffect(() => {
    const wsUrl =
      (import.meta.env.VITE_WS_URL as string | undefined) ||
      (location.protocol === 'https:' ? 'wss://' : 'ws://') +
        location.host +
        '/ws'

    pushDebug(`Opening WebSocket to ${wsUrl}`)

    const ws = new WebSocket(wsUrl)

    ws.onopen = () => {
      setWsConnected(true)
      pushDebug('WebSocket connected')
    }
    ws.onclose = () => {
      setWsConnected(false)
      pushDebug('WebSocket closed')
    }
    ws.onerror = () => {
      setWsConnected(false)
      pushDebug('WebSocket error')
    }
    ws.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data) as WsEvent
        if (data.type === 'welcome') {
          setLastEvent('Connected to Plum backend.')
          pushDebug('WS event: welcome')
        } else if (data.type === 'transcode_started') {
          setLastEvent(`Transcode started for id=${data.id}`)
          pushDebug(`WS event: transcode_started for id=${data.id}`)
        } else if (data.type === 'transcode_complete') {
          const elapsed = 'elapsed' in data && typeof data.elapsed === 'number' ? data.elapsed.toFixed(1) : 'unknown'
          setLastEvent(`Transcode complete for id=${data.id} in ${elapsed}s`)
          pushDebug(`WS event: transcode_complete for id=${data.id} in ${elapsed}s`)
        }
      } catch (e) {
        console.warn('Bad WS message', e)
        pushDebug('Failed to parse WS message')
      }
    }

    return () => {
      ws.close()
    }
  }, [])

  const title = useMemo(() => 'Plum · Media Server Sketch', [])

  return (
    <div className="app-root">
      <header className="app-header">
        <div className="brand-mark">
          <div className="brand-glyph" />
          <div className="brand-text">
            <div className="brand-title">{title}</div>
            <div className="brand-sub">Barebones Plex-style experiment</div>
          </div>
        </div>
      </header>
      <main className="app-main">
        <section className="left-pane">
          <div className="pane-header">
            <h2 className="pane-title">Library</h2>
            <button
              type="button"
              className="scan-button"
              onClick={async () => {
                pushDebug('Triggering manual library scan…')
                try {
                  const res = await scanLibrary('/tv', 'tv')
                  pushDebug(`Scan complete: added ${res.added} items`)
                  const items = await fetchMediaList()
                  setMedia(items)
                } catch (err) {
                  console.error(err)
                  pushDebug(`Scan failed: ${String(err)}`)
                }
              }}
            >
              Scan
            </button>
          </div>
          <MediaList
            items={media}
            onSelect={setSelected}
            onTranscode={async (item) => {
              setSelected(item)
              try {
                await startTranscode(item.id)
                setLastEvent(`Requested transcode for “${item.title}”`)
              } catch (err) {
                console.error(err)
                setLastEvent('Failed to start transcode – see console')
              }
            }}
          />
        </section>
        <PlayerPanel
          selected={selected}
          wsConnected={wsConnected}
          lastEvent={lastEvent}
        />
      </main>
      <section className="debug-panel">
        <div className="debug-header">
          <span>Debug log</span>
          <button
            type="button"
            className="debug-clear"
            onClick={() => setDebugLines([])}
          >
            Clear
          </button>
        </div>
        <div className="debug-body">
          {debugLines.length === 0 ? (
            <div className="debug-empty">No debug messages yet. Interact with the UI to see events.</div>
          ) : (
            <ul>
              {debugLines.map((line) => (
                <li key={line}>{line}</li>
              ))}
            </ul>
          )}
        </div>
      </section>
    </div>
  )
}

export default App
