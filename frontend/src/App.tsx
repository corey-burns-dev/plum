import { useEffect, useMemo, useState } from 'react'
import './App.css'
import { fetchMediaList, startTranscode, type MediaItem } from './api'
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

  useEffect(() => {
    ;(async () => {
      try {
        const items = await fetchMediaList()
        setMedia(items)
      } catch (err) {
        console.error(err)
      }
    })()
  }, [])

  useEffect(() => {
    const wsUrl =
      (import.meta.env.VITE_WS_URL as string | undefined) ||
      (location.protocol === 'https:' ? 'wss://' : 'ws://') +
        location.host +
        '/ws'

    const ws = new WebSocket(wsUrl)

    ws.onopen = () => {
      setWsConnected(true)
    }
    ws.onclose = () => {
      setWsConnected(false)
    }
    ws.onerror = () => {
      setWsConnected(false)
    }
    ws.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data) as WsEvent
        if (data.type === 'welcome') {
          setLastEvent('Connected to Plum backend.')
        } else if (data.type === 'transcode_started') {
          setLastEvent(`Transcode started for id=${data.id}`)
        } else if (data.type === 'transcode_complete') {
          const elapsed = 'elapsed' in data && typeof data.elapsed === 'number' ? data.elapsed.toFixed(1) : 'unknown'
          setLastEvent(`Transcode complete for id=${data.id} in ${elapsed}s`)
        }
      } catch (e) {
        console.warn('Bad WS message', e)
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
          <h2 className="pane-title">Library</h2>
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
    </div>
  )
}

export default App
