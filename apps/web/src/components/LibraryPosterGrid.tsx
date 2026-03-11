import type { MouseEvent } from 'react'
import { Link } from 'react-router-dom'
import { tmdbPosterUrl } from '@plum/shared'

export type PosterGridItem = {
  key: string
  title: string
  subtitle: string
  posterPath?: string
  isIdentifying?: boolean
  statusLabel?: string
  href?: string
  onClick?: () => void
  onContextMenu?: (event: MouseEvent<HTMLDivElement>) => void
}

interface Props {
  items: PosterGridItem[]
  compact?: boolean
}

export function LibraryPosterGrid({ items, compact = false }: Props) {
  return (
    <div className={`show-cards-grid${compact ? ' show-cards-grid--compact' : ''}`}>
      {items.map((item) => (
        <div
          key={item.key}
          className="relative"
          onContextMenu={item.onContextMenu}
        >
          {item.href ? (
            <Link to={item.href} className="show-card">
              <PosterCardBody item={item} />
            </Link>
          ) : (
            <button type="button" className="show-card show-card-button" onClick={item.onClick}>
              <PosterCardBody item={item} />
            </button>
          )}
        </div>
      ))}
    </div>
  )
}

function PosterCardBody({ item }: { item: PosterGridItem }) {
  const posterUrl = tmdbPosterUrl(item.posterPath)

  return (
    <>
      <div className={`show-card-poster${item.isIdentifying ? ' show-card-poster--identifying' : ''}`}>
        {posterUrl ? (
          <img src={posterUrl} alt="" />
        ) : item.isIdentifying ? (
          <div className="show-card-poster-shell show-card-poster-shell--identifying" aria-hidden="true" />
        ) : (
          <img src="/placeholder-poster.png" alt="" />
        )}
        {item.isIdentifying && (
          <div className="show-card-identifying">
            <span className="show-card-identifying-label">{item.statusLabel || 'Identifying…'}</span>
          </div>
        )}
      </div>
      <div className="show-card-info">
        <div className="show-card-title">{item.title}</div>
        <div className="show-card-count">{item.subtitle}</div>
      </div>
    </>
  )
}
