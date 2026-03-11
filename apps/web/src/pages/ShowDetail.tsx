import { useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import type { MediaItem } from "../api";
import { BASE_URL } from "../api";
import { usePlayer } from "../contexts/PlayerContext";
import { getShowKey, sortEpisodes } from "../lib/showGrouping";
import { tmdbBackdropUrl, tmdbPosterUrl } from "@plum/shared";
import { useLibraryMedia, useSeries } from "../queries";

function formatDuration(seconds: number): string {
  if (seconds <= 0) return "";
  const m = Math.floor(seconds / 60);
  const s = seconds % 60;
  return s > 0 ? `${m}:${s.toString().padStart(2, "0")}` : `${m} min`;
}

function seasonEpisodeLabel(item: MediaItem): string {
  const s = item.season ?? 0;
  const e = item.episode ?? 0;
  if (s > 0 || e > 0) return `S${String(s).padStart(2, "0")}E${String(e).padStart(2, "0")}`;
  return "";
}

/** Parse TMDB ID from showKey when it is "tmdb-123". */
function tmdbIdFromShowKey(showKey: string | null): number | null {
  if (!showKey || !showKey.startsWith("tmdb-")) return null;
  const id = parseInt(showKey.slice(5), 10);
  return Number.isNaN(id) ? null : id;
}

export function ShowDetail() {
  const { libraryId: libraryIdParam, showKey: showKeyEncoded } = useParams();
  const libraryId = libraryIdParam ? parseInt(libraryIdParam, 10) : null;
  const showKey = showKeyEncoded ? decodeURIComponent(showKeyEncoded) : null;
  const tmdbId = useMemo(() => tmdbIdFromShowKey(showKey), [showKey]);

  const { data: items = [], isLoading: loading, error } = useLibraryMedia(libraryId);
  const { data: series, isLoading: seriesLoading } = useSeries(tmdbId);
  const { playEpisode } = usePlayer();
  const [expandedEpisodeId, setExpandedEpisodeId] = useState<number | null>(null);

  const episodes = useMemo(() => {
    if (!showKey) return [];
    const filtered = items.filter((m) => getShowKey(m) === showKey);
    sortEpisodes(filtered);
    return filtered;
  }, [items, showKey]);

  /** Episodes grouped by season number, with seasons sorted ascending. */
  const episodesBySeason = useMemo(() => {
    const map = new Map<number, MediaItem[]>();
    for (const ep of episodes) {
      const s = ep.season ?? 0;
      const list = map.get(s) ?? [];
      list.push(ep);
      map.set(s, list);
    }
    const seasons = Array.from(map.keys()).toSorted((a, b) => a - b);
    return { map, seasons };
  }, [episodes]);

  const showTitle =
    series?.name ??
    (episodes.length > 0
      ? episodes[0].title.replace(/\s*-\s*S\d+.*$/i, "").trim()
      : (showKey ?? "Show"));

  if (libraryId == null || showKey == null) {
    return (
      <p className="auth-muted">
        <Link to="/" className="link-button">
          Back to library
        </Link>
      </p>
    );
  }

  if (loading) {
    return <p className="auth-muted">Loading…</p>;
  }

  if (error) {
    return (
      <p className="auth-muted">
        {error.message} ·{" "}
        <Link to={`/library/${libraryId}`} className="link-button">
          Back
        </Link>
      </p>
    );
  }

  const posterUrl = series?.poster_path
    ? series.poster_path.startsWith("http")
      ? series.poster_path
      : tmdbPosterUrl(series.poster_path, "w500")
    : episodes[0]?.poster_path
      ? tmdbPosterUrl(episodes[0].poster_path)
      : "";
  const backdropUrl = series?.backdrop_path
    ? series.backdrop_path.startsWith("http")
      ? series.backdrop_path
      : tmdbBackdropUrl(series.backdrop_path, "w780")
    : episodes[0]?.backdrop_path
      ? tmdbBackdropUrl(episodes[0].backdrop_path)
      : "";

  return (
    <div className="show-detail">
      <nav className="show-detail-nav">
        <Link to={libraryId ? `/library/${libraryId}` : "/"} className="link-button">
          ← Back to library
        </Link>
      </nav>
      {backdropUrl && (
        <div className="show-detail-backdrop">
          <img src={backdropUrl} alt="" />
        </div>
      )}
      <div className="show-detail-header">
        {posterUrl && (
          <div className="show-detail-poster">
            <img src={posterUrl} alt="" />
          </div>
        )}
        <div className="show-detail-meta">
          <h1 className="show-detail-title">{showTitle}</h1>
          {series?.first_air_date && <p className="show-detail-date">{series.first_air_date}</p>}
          {!seriesLoading && series?.overview && (
            <p className="show-detail-overview">{series.overview}</p>
          )}
        </div>
      </div>
      {episodes.length === 0 ? (
        <p className="auth-muted">No episodes found for this show.</p>
      ) : (
        <div className="show-detail-seasons">
          {episodesBySeason.seasons.map((seasonNum) => {
            const seasonEps = episodesBySeason.map.get(seasonNum) ?? [];
            const label = seasonNum === 0 ? "Specials" : `Season ${seasonNum}`;
            return (
              <section key={seasonNum} className="show-detail-season">
                <h2 className="show-detail-season-title">
                  {label}
                  <span className="show-detail-season-count">
                    {seasonEps.length} episode{seasonEps.length !== 1 ? "s" : ""}
                  </span>
                </h2>
                <ul className="episodes-list show-detail-episodes">
                  {seasonEps.map((ep) => (
                    <li key={ep.id} className="episode-row episode-row-detail">
                      <div className="episode-thumbnail-wrap">
                        <img
                          src={`${BASE_URL}/api/media/${ep.id}/thumbnail`}
                          alt=""
                          className="episode-thumbnail"
                        />
                      </div>
                      <span className="episode-season-ep" title={ep.title}>
                        {seasonEpisodeLabel(ep)}
                      </span>
                      <div className="episode-info">
                        <span className="episode-title" title={ep.title}>
                          {ep.title}
                        </span>
                        {ep.match_status && ep.match_status !== "identified" && (
                          <span className="episode-release-date">{ep.match_status}</span>
                        )}
                        {ep.release_date && (
                          <span className="episode-release-date">{ep.release_date}</span>
                        )}
                        {ep.overview && (
                          <button
                            type="button"
                            className="episode-overview-toggle"
                            onClick={() =>
                              setExpandedEpisodeId((id) => (id === ep.id ? null : ep.id))
                            }
                          >
                            {expandedEpisodeId === ep.id ? "Hide" : "Show"} summary
                          </button>
                        )}
                        {expandedEpisodeId === ep.id && ep.overview && (
                          <p className="episode-overview">{ep.overview}</p>
                        )}
                      </div>
                      {ep.duration > 0 && (
                        <span className="episode-duration">{formatDuration(ep.duration)}</span>
                      )}
                      <button
                        type="button"
                        className="play-button small"
                        onClick={() => playEpisode(ep)}
                      >
                        Play
                      </button>
                    </li>
                  ))}
                </ul>
              </section>
            );
          })}
        </div>
      )}
    </div>
  );
}
