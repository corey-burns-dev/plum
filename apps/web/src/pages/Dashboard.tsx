import { Link } from "react-router-dom";
import { LibraryPosterGrid, type PosterGridItem } from "@/components/LibraryPosterGrid";
import { usePlayer } from "@/contexts/PlayerContext";
import { formatRemainingTime } from "@/lib/progress";
import { useHomeDashboard, useLibraries } from "@/queries";

export function Dashboard() {
  const { data, error, isLoading, refetch } = useHomeDashboard();
  const { data: libraries = [] } = useLibraries();
  const { playEpisode, playMovie } = usePlayer();
  const firstLibraryId = libraries[0]?.id ?? null;

  const continueWatchingCards: PosterGridItem[] =
    data?.continueWatching.map((entry) => {
      const subtitle =
        entry.kind === "show"
          ? [entry.episode_label, formatRemainingTime(entry.remaining_seconds)]
              .filter(Boolean)
              .join(" • ")
          : [
              entry.media.release_date?.split("-")[0] ?? "Movie",
              formatRemainingTime(entry.remaining_seconds),
            ]
              .filter(Boolean)
              .join(" • ");

      return {
        key: `${entry.kind}-${entry.media.id}`,
        title: entry.kind === "show" ? entry.show_title || entry.media.title : entry.media.title,
        subtitle,
        posterPath: entry.media.poster_path,
        imdbRating: entry.media.imdb_rating,
        progressPercent: entry.media.progress_percent,
        href: undefined,
        onClick:
          entry.kind === "movie" ? () => playMovie(entry.media) : () => playEpisode(entry.media),
        onPlay:
          entry.kind === "movie"
            ? () => playMovie(entry.media)
            : () => playEpisode(entry.media),
      };
    }) ?? [];

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-8">
      <section className="rounded-[var(--radius-xl)] border border-[var(--plum-border)] bg-[var(--plum-panel)]/70 p-6 shadow-[0_30px_80px_rgba(0,0,0,0.24)]">
        <div className="flex items-end justify-between gap-4">
          <div>
            <p className="text-xs font-semibold uppercase tracking-[0.24em] text-[var(--plum-muted)]">
              Home
            </p>
            <h1
              className="mt-2 text-3xl font-semibold tracking-tight text-[var(--plum-text)]"
              style={{ fontFamily: "var(--font-display)" }}
            >
              Continue watching
            </h1>
            <p className="mt-2 max-w-2xl text-sm text-[var(--plum-muted)]">
              Pick up where you left off across movies, TV, and anime.
            </p>
          </div>
          {firstLibraryId != null ? (
            <Link
              to={`/library/${firstLibraryId}`}
              className="text-sm font-medium text-[var(--plum-accent)] hover:underline"
            >
              Browse libraries
            </Link>
          ) : null}
        </div>
      </section>

      <section className="flex min-h-0 flex-1 flex-col gap-4">
        <div className="flex items-center justify-between gap-4">
          <h2 className="text-lg font-semibold text-[var(--plum-text)]">Recent progress</h2>
          {data?.continueWatching.length ? (
            <span className="text-sm text-[var(--plum-muted)]">
              {data.continueWatching.length} active item
              {data.continueWatching.length === 1 ? "" : "s"}
            </span>
          ) : null}
        </div>

        {isLoading ? (
          <p className="text-sm text-[var(--plum-muted)]">Loading continue watching…</p>
        ) : error ? (
          <p className="text-sm text-[var(--plum-muted)]">
            Failed to load home: {error.message}{" "}
            <button
              type="button"
              className="text-[var(--plum-accent)] hover:underline"
              onClick={() => void refetch()}
            >
              Retry
            </button>
          </p>
        ) : continueWatchingCards.length === 0 ? (
          <div className="rounded-[var(--radius-xl)] border border-dashed border-[var(--plum-border)] bg-[var(--plum-panel)]/45 p-8 text-sm text-[var(--plum-muted)]">
            Start a movie or episode and Plum will keep your spot here.
          </div>
        ) : (
          <LibraryPosterGrid items={continueWatchingCards} />
        )}
      </section>
    </div>
  );
}
