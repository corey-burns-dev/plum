import type { MediaItem } from '../api'
import { BASE_URL } from '../api'

interface Props {
  selected?: MediaItem
  wsConnected: boolean
  lastEvent?: string
}

export function PlayerPanel({ selected, wsConnected, lastEvent }: Props) {
  return (
    <section className="player-panel">
      <header className="player-header">
        <div className="status-dot" data-connected={wsConnected} />
        <span className="status-label">
          WebSocket {wsConnected ? 'connected' : 'disconnected'}
        </span>
      </header>
      {selected ? (
        <div className="player-body">
          <div className="player-title">{selected.title}</div>
          <div className="player-sub">
            {selected.type} · {Math.round(selected.duration)}s
          </div>
          <video
            className="player-video"
            controls
            src={`${BASE_URL || ''}/api/stream/${selected.id}`}
          />
          <div className="player-event">
            {lastEvent || 'Idle · trigger a transcode to see updates.'}
          </div>
        </div>
      ) : (
        <div className="player-empty">
          Select a media item on the left to inspect and transcode.
        </div>
      )}
    </section>
  )
}

