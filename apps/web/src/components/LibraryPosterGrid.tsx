import {
  useEffect,
  useRef,
  useState,
  type CSSProperties,
  type MouseEvent as ReactMouseEvent,
} from "react";
import { Link } from "react-router-dom";
import { useVirtualizer } from "@tanstack/react-virtual";
import { Play } from "lucide-react";
import { tmdbPosterUrl } from "@plum/shared";
import { useVirtualContainerMetrics } from "@/lib/virtualization";

export type PosterCardState = "default" | "identifying" | "identify-failed" | "review-needed";

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
  statusActionDisabled?: boolean;
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

const DEFAULT_CARD_WIDTH = 160;
const DEFAULT_CARD_GAP = 20;
const DEFAULT_ROW_HEIGHT = 320;
const COMPACT_CARD_WIDTH = 140;
const COMPACT_CARD_GAP = 14;
const COMPACT_ROW_HEIGHT = 276;
const GRID_VIRTUALIZATION_THRESHOLD = 120;

export function LibraryPosterGrid({ items, compact = false }: Props) {
  const rootRef = useRef<HTMLDivElement>(null);
  const { scrollElement, width, scrollMargin } = useVirtualContainerMetrics(rootRef);
  const cardWidth = compact ? COMPACT_CARD_WIDTH : DEFAULT_CARD_WIDTH;
  const gap = compact ? COMPACT_CARD_GAP : DEFAULT_CARD_GAP;
  const columns = Math.max(1, Math.floor((Math.max(width, cardWidth) + gap) / (cardWidth + gap)));
  const shouldVirtualize =
    items.length >= GRID_VIRTUALIZATION_THRESHOLD &&
    scrollElement != null &&
    width > 0 &&
    typeof ResizeObserver !== "undefined";

  const rowCount = Math.ceil(items.length / columns);
  const rowVirtualizer = useVirtualizer({
    count: shouldVirtualize ? rowCount : 0,
    getScrollElement: () => scrollElement,
    estimateSize: () => (compact ? COMPACT_ROW_HEIGHT : DEFAULT_ROW_HEIGHT),
    overscan: compact ? 3 : 2,
    scrollMargin,
  });

  if (!shouldVirtualize) {
    return (
      <div ref={rootRef} className={`show-cards-grid${compact ? " show-cards-grid--compact" : ""}`}>
        {items.map((item) => (
          <PosterCard key={item.key} item={item} />
        ))}
      </div>
    );
  }

  return (
    <div
      ref={rootRef}
      className={`show-cards-grid show-cards-grid--virtual${compact ? " show-cards-grid--compact" : ""}`}
      style={{ "--poster-columns": String(columns) } as CSSProperties}
    >
      <div
        className="show-cards-grid__spacer"
        style={{ height: `${rowVirtualizer.getTotalSize()}px` }}
      >
        {rowVirtualizer.getVirtualItems().map((virtualRow) => {
          const start = virtualRow.index * columns;
          const rowItems = items.slice(start, start + columns);
          return (
            <div
              key={virtualRow.key}
              className="show-cards-grid__row"
              style={{
                transform: `translateY(${virtualRow.start - scrollMargin}px)`,
                gap: `${gap}px`,
              }}
            >
              {rowItems.map((item) => (
                <PosterCard key={item.key} item={item} />
              ))}
            </div>
          );
        })}
      </div>
    </div>
  );
}

function PosterCard({ item }: { item: PosterGridItem }) {
  return (
    <div className="show-card" onContextMenu={item.onContextMenu}>
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
          <img src="/placeholder-poster.svg" alt="" />
        ) : (
          <img src={posterUrl} alt="" onError={() => setPosterErrored(true)} />
        )}
        {cardState !== "default" && (
          <div className={`show-card-status show-card-status--${cardState}`}>
            {item.statusLabel && <span className="show-card-status-label">{item.statusLabel}</span>}
            {item.statusActionLabel && item.onStatusAction && (
              <button
                type="button"
                className="show-card-status-action"
                disabled={item.statusActionDisabled}
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
