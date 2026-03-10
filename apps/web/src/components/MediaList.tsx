import type { MediaItem } from '../api'
import { tmdbPosterUrl } from '@plum/shared'

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
    <div className="media-grid">
      {items.map((m) => (
        <div key={m.id} className="media-card" onClick={() => onSelect(m)}>
          <div className="media-poster">
            <img src={tmdbPosterUrl(m.poster_path) || '/placeholder-poster.png'} alt={m.title} />
            <div className="media-type-overlay">{m.type}</div>
          </div>
          <div className="media-info">
            <div className="media-title" title={m.title}>{m.title}</div>
            <div className="media-subtitle">
              {m.release_date && <span>{m.release_date.split('-')[0]}</span>}
              {m.vote_average ? <span>⭐ {m.vote_average.toFixed(1)}</span> : null}
            </div>
          </div>
          <button
            className="play-button"
            onClick={(e) => {
              e.stopPropagation()
              onTranscode(m)
            }}
          >
            Play
          </button>
        </div>
      ))}
    </div>
  )
}

