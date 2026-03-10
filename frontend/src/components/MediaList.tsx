import type { MediaItem } from '../api'

interface Props {
  items: MediaItem[]
  onSelect: (item: MediaItem) => void
  onTranscode: (item: MediaItem) => void
}

export function MediaList({ items, onSelect, onTranscode }: Props) {
  if (items.length === 0) {
    return <div className="empty-state">No media found in Plum.</div>
  }

  return (
    <div className="media-list">
      {items.map((m) => (
        <button
          key={m.id}
          className="media-row"
          onClick={() => onSelect(m)}
        >
          <div className="media-meta">
            <div className="media-title">{m.title}</div>
            <div className="media-sub">
              <span>{m.type}</span>
              <span>·</span>
              <span>{Math.round(m.duration)}s</span>
            </div>
          </div>
          <div className="media-actions">
            <span
              className="pill"
              onClick={(e) => {
                e.stopPropagation()
                onTranscode(m)
              }}
            >
              Play / Transcode
            </span>
          </div>
        </button>
      ))}
    </div>
  )
}

