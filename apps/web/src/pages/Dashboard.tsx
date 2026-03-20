import { Link } from "react-router-dom";
import type { HomeDashboard } from "@/api";
import { LibraryPosterGrid, type PosterGridItem } from "@/components/LibraryPosterGrid";
import { usePlayer } from "@/contexts/PlayerContext";
import { formatRemainingTime } from "@/lib/progress";
import { useHomeDashboard, useLibraries } from "@/queries";

type DashboardEntry =
  | HomeDashboard["continueWatching"][number]
  | NonNullable<HomeDashboard["recentlyAdded"]>[number];
type DashboardShelf = "continueWatching" | "recentlyAdded";

function getDashboardEntryTitle(entry: DashboardEntry): string {
  return entry.kind === "show" ? entry.show_title || entry.media.title : entry.media.title;
}

function getDashboardEntrySubtitle(entry: DashboardEntry, shelf: DashboardShelf): string {
  const remainingSeconds = "remaining_seconds" in entry ? entry.remaining_seconds : undefined;
  if (entry.kind === "show") {
    if (shelf === "continueWatching") {
      return [entry.episode_label, formatRemainingTime(remainingSeconds)]
        .filter(Boolean)
        .join(" • ");
    }
    return entry.episode_label || "New episode";
  }

  const year = entry.media.release_date?.split("-")[0] ?? "Movie";
  if (shelf === "continueWatching") {
    return [year, formatRemainingTime(remainingSeconds)].filter(Boolean).join(" • ");
  }
  return year;
}

function toPosterGridItem(
  entry: DashboardEntry,
  shelf: DashboardShelf,
  playMovie: (item: DashboardEntry["media"]) => void,
  playEpisode: (item: DashboardEntry["media"]) => void,
): PosterGridItem {
  const playItem =
    entry.kind === "movie" ? () => playMovie(entry.media) : () => playEpisode(entry.media);

  return {
    key: `${shelf}-${entry.kind}-${entry.media.id}`,
    title: getDashboardEntryTitle(entry),
    subtitle: getDashboardEntrySubtitle(entry, shelf),
    posterPath: entry.media.poster_path,
    imdbRating: entry.media.imdb_rating,
    progressPercent: shelf === "continueWatching" ? entry.media.progress_percent : undefined,
    href: undefined,
    onClick: playItem,
    onPlay: playItem,
  };
}

export function Dashboard() {
  const { data, error, isLoading, refetch } = useHomeDashboard();
  const { data: libraries = [] } = useLibraries();
  const { playEpisode, playMovie } = usePlayer();
  const firstLibraryId = libraries[0]?.id ?? null;

  const continueWatchingCards: PosterGridItem[] =
    data?.continueWatching.map((entry) =>
      toPosterGridItem(entry, "continueWatching", playMovie, playEpisode),
    ) ?? [];
  const recentlyAddedCards: PosterGridItem[] =
    data?.recentlyAdded?.map((entry) =>
      toPosterGridItem(entry, "recentlyAdded", playMovie, playEpisode),
    ) ?? [];

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

      <section className="flex min-h-0 flex-col gap-4">
        <div className="flex items-center justify-between gap-4">
          <h2 className="text-lg font-semibold text-[var(--plum-text)]">Recently added</h2>
          {data?.recentlyAdded?.length ? (
            <span className="text-sm text-[var(--plum-muted)]">
              {data.recentlyAdded.length} new item{data.recentlyAdded.length === 1 ? "" : "s"}
            </span>
          ) : null}
        </div>

        {isLoading ? (
          <p className="text-sm text-[var(--plum-muted)]">Loading recently added…</p>
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
        ) : recentlyAddedCards.length === 0 ? (
          <div className="rounded-[var(--radius-xl)] border border-dashed border-[var(--plum-border)] bg-[var(--plum-panel)]/45 p-8 text-sm text-[var(--plum-muted)]">
            Scan your libraries and Plum will surface the newest additions here.
          </div>
        ) : (
          <LibraryPosterGrid items={recentlyAddedCards} />
        )}
      </section>
    </div>
  );
}
