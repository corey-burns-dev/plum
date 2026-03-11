import { useEffect, useState, type MouseEvent as ReactMouseEvent } from "react";
import { Link } from "react-router-dom";
import { Play } from "lucide-react";
import { tmdbPosterUrl } from "@plum/shared";

export type PosterCardState = "default" | "identifying" | "identify-failed";

export type PosterGridItem = {
  key: string;
  title: string;
  subtitle: string;
  metaLine?: string;
  posterPath?: string;
  imdbRating?: number;
  progressPercent?: number;
  cardState?: PosterCardState;
  statusLabel?: string;
  statusActionLabel?: string;
  href?: string;
  onClick?: () => void;
  onPlay?: () => void;
  onStatusAction?: () => void;
  onContextMenu?: (event: ReactMouseEvent<HTMLDivElement>) => void;
};

interface Props {
  items: PosterGridItem[];
  compact?: boolean;
}

export function LibraryPosterGrid({ items, compact = false }: Props) {
  return (
    <div className={`show-cards-grid${compact ? " show-cards-grid--compact" : ""}`}>
      {items.map((item) => (
        <div key={item.key} className="show-card" onContextMenu={item.onContextMenu}>
          <CardHitArea item={item} />
          {item.onPlay && (item.cardState ?? "default") === "default" && (
            <button
              type="button"
              className="show-card-play-button"
              aria-label={`Play ${item.title}`}
              onClick={(event) => {
                event.preventDefault();
                event.stopPropagation();
                item.onPlay?.();
              }}
            >
              <Play className="size-5 fill-current" />
            </button>
          )}
          <PosterCardBody item={item} />
        </div>
      ))}
    </div>
  );
}

function CardHitArea({ item }: { item: PosterGridItem }) {
  if (item.href) {
    return <Link to={item.href} className="show-card-hit-area" aria-label={item.title} />;
  }

  if (item.onClick) {
    return (
      <button
        type="button"
        className="show-card-hit-area show-card-hit-area-button"
        aria-label={item.title}
        onClick={item.onClick}
      />
    );
  }

  return <div className="show-card-hit-area" aria-hidden="true" />;
}

function PosterCardBody({ item }: { item: PosterGridItem }) {
  const posterUrl = tmdbPosterUrl(item.posterPath);
  const [posterErrored, setPosterErrored] = useState(false);
  const cardState = item.cardState ?? "default";
  const progressPercent =
    item.progressPercent != null ? Math.max(0, Math.min(100, item.progressPercent)) : 0;
  const showIdentifyingShell = cardState === "identifying" && (!posterUrl || posterErrored);
  const showFailedShell = cardState === "identify-failed" && (!posterUrl || posterErrored);
  const showPlaceholderPoster = cardState === "default" && (!posterUrl || posterErrored);

  useEffect(() => {
    setPosterErrored(false);
  }, [posterUrl]);

  return (
    <div className="show-card-content">
      <div
        className={`show-card-poster${cardState === "identifying" ? " show-card-poster--identifying" : ""}`}
      >
        {showIdentifyingShell ? (
          <div
            className="show-card-poster-shell show-card-poster-shell--identifying"
            aria-hidden="true"
          />
        ) : showFailedShell ? (
          <div
            className="show-card-poster-shell show-card-poster-shell--failed"
            aria-hidden="true"
          />
        ) : showPlaceholderPoster ? (
          <img src="/placeholder-poster.png" alt="" />
        ) : (
          <img src={posterUrl} alt="" onError={() => setPosterErrored(true)} />
        )}
        {cardState !== "default" && (
          <div className={`show-card-status show-card-status--${cardState}`}>
            {item.statusLabel && <span className="show-card-status-label">{item.statusLabel}</span>}
            {cardState === "identify-failed" && item.statusActionLabel && item.onStatusAction && (
              <button
                type="button"
                className="show-card-status-action"
                onClick={(event) => {
                  event.preventDefault();
                  event.stopPropagation();
                  item.onStatusAction?.();
                }}
              >
                {item.statusActionLabel}
              </button>
            )}
          </div>
        )}
        {progressPercent > 0 && progressPercent < 95 && (
          <div className="show-card-progress" aria-hidden="true">
            <div
              className="show-card-progress__value"
              style={{ width: `${progressPercent}%` }}
            />
          </div>
        )}
      </div>
      <div className="show-card-info">
        <div className="show-card-title">{item.title}</div>
        <div className="show-card-count">{item.subtitle}</div>
        {(item.imdbRating || item.metaLine) && (
          <div className="show-card-meta">
            {item.imdbRating ? <ImdbBadge rating={item.imdbRating} /> : null}
            {item.metaLine ? <span className="show-card-meta__copy">{item.metaLine}</span> : null}
          </div>
        )}
      </div>
    </div>
  );
}

function ImdbBadge({ rating }: { rating: number }) {
  return (
    <span className="show-card-imdb">
      <span className="show-card-imdb__mark">IMDb</span>
      <span>{rating.toFixed(1)}</span>
    </span>
  );
}
