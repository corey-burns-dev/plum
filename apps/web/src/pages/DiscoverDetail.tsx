import { Link, useParams } from "react-router-dom";
import { ExternalLink, Film, Sparkles, Star, Tv } from "lucide-react";
import { tmdbBackdropUrl, tmdbPosterUrl } from "@plum/shared";
import { Button } from "@/components/ui/button";
import type { DiscoverMediaType } from "@/api";
import {
  discoverDetailMeta,
  discoverLibraryHref,
  discoverMediaLabel,
  discoverVideoUrl,
  firstDiscoverMatch,
} from "@/lib/discover";
import { useDiscoverTitleDetails } from "@/queries";

function isDiscoverMediaType(value: string | undefined): value is DiscoverMediaType {
  return value === "movie" || value === "tv";
}

function isDiscoverConfigError(error: Error | null): boolean {
  return error?.message.includes("TMDB_API_KEY") ?? false;
}

export function DiscoverDetail() {
  const { mediaType: mediaTypeParam, tmdbId: tmdbIdParam } = useParams();
  const mediaType = isDiscoverMediaType(mediaTypeParam) ? mediaTypeParam : null;
  const tmdbId = tmdbIdParam ? Number.parseInt(tmdbIdParam, 10) : null;
  const {
    data: details,
    error,
    isLoading,
    refetch,
  } = useDiscoverTitleDetails(mediaType, tmdbId);

  if (mediaType == null || tmdbId == null || Number.isNaN(tmdbId) || tmdbId <= 0) {
    return (
      <DiscoverDetailMessage
        title="Invalid title"
        copy="The discover title you opened is missing a valid TMDB media type or id."
      />
    );
  }

  if (isLoading) {
    return <p className="text-sm text-[var(--plum-muted)]">Loading discover title...</p>;
  }

  if (isDiscoverConfigError(error)) {
    return (
      <DiscoverDetailMessage
        title="Discover needs TMDB configured"
        copy="Set `TMDB_API_KEY` on the server to enable external title details."
        actionLabel="Retry"
        onAction={() => void refetch()}
      />
    );
  }

  if (error) {
    return (
      <DiscoverDetailMessage
        title="Could not load this title"
        copy={error.message}
        actionLabel="Retry"
        onAction={() => void refetch()}
      />
    );
  }

  if (!details) {
    return (
      <DiscoverDetailMessage
        title="Title not found"
        copy="This discover title is no longer available from TMDB."
      />
    );
  }

  const primaryMatch = firstDiscoverMatch(details.library_matches);
  const backdropUrl = tmdbBackdropUrl(details.backdrop_path, "w780");
  const posterUrl = tmdbPosterUrl(details.poster_path, "w500");
  const meta = discoverDetailMeta(details);
  const videos = details.videos
    .map((video) => ({ ...video, href: discoverVideoUrl(video) }))
    .filter((video) => video.href !== "")
    .slice(0, 6);

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-8">
      <section className="relative overflow-hidden rounded-[var(--radius-xl)] border border-[var(--plum-border)] bg-[var(--plum-panel)] shadow-[0_24px_80px_rgba(8,12,24,0.16)]">
        <div className="absolute inset-0">
          {backdropUrl ? (
            <img src={backdropUrl} alt="" className="h-full w-full object-cover opacity-35" />
          ) : null}
          <div className="absolute inset-0 bg-[linear-gradient(120deg,rgba(12,17,30,0.96),rgba(12,17,30,0.72),rgba(12,17,30,0.86))]" />
        </div>

        <div className="relative grid gap-6 p-6 lg:grid-cols-[220px_minmax(0,1fr)] lg:p-8">
          <div className="overflow-hidden rounded-[var(--radius-lg)] border border-white/10 bg-black/20 shadow-[0_20px_55px_rgba(0,0,0,0.28)]">
            {posterUrl ? (
              <img src={posterUrl} alt="" className="h-full w-full object-cover" />
            ) : (
              <img src="/placeholder-poster.svg" alt="" className="h-full w-full object-cover" />
            )}
          </div>

          <div className="flex flex-col gap-5 text-white">
            <div className="flex flex-wrap items-center gap-3 text-xs font-semibold uppercase tracking-[0.2em] text-white/65">
              <Link to="/discover" className="transition-colors hover:text-white">
                Discover
              </Link>
              <span>/</span>
              <span>{discoverMediaLabel(details.media_type)}</span>
            </div>

            <div className="space-y-3">
              <div className="flex flex-wrap items-center gap-3">
                <span className="inline-flex items-center gap-2 rounded-full border border-white/10 bg-white/10 px-3 py-1 text-xs font-semibold uppercase tracking-[0.18em] text-white/75">
                  {details.media_type === "movie" ? (
                    <Film className="size-3.5" />
                  ) : (
                    <Tv className="size-3.5" />
                  )}
                  {discoverMediaLabel(details.media_type)}
                </span>
                {(details.library_matches?.length ?? 0) > 0 ? (
                  <span className="rounded-full bg-[var(--plum-accent)] px-3 py-1 text-xs font-semibold uppercase tracking-[0.18em] text-white shadow-[0_0_18px_rgba(244,90,160,0.35)]">
                    In Library
                  </span>
                ) : (
                  <span className="rounded-full border border-white/10 bg-white/8 px-3 py-1 text-xs font-semibold uppercase tracking-[0.18em] text-white/70">
                    Not In Server Yet
                  </span>
                )}
              </div>

              <div className="space-y-2">
                <h1 className="text-3xl font-semibold tracking-tight">{details.title}</h1>
                <div className="flex flex-wrap items-center gap-3 text-sm text-white/70">
                  {meta.map((entry) => (
                    <span key={entry}>{entry}</span>
                  ))}
                </div>
              </div>
            </div>

            <div className="flex flex-wrap items-center gap-4 text-sm text-white/80">
              {details.vote_average ? (
                <span className="inline-flex items-center gap-2 rounded-full bg-white/10 px-3 py-1">
                  <Star className="size-4 fill-current text-[var(--plum-accent)]" />
                  TMDB {details.vote_average.toFixed(1)}
                </span>
              ) : null}
              {details.imdb_rating ? (
                <span className="inline-flex items-center gap-2 rounded-full bg-white/10 px-3 py-1">
                  <Sparkles className="size-4 text-[var(--plum-accent)]" />
                  IMDb {details.imdb_rating.toFixed(1)}
                </span>
              ) : null}
            </div>

            <p className="max-w-3xl text-sm leading-7 text-white/78">{details.overview}</p>

            <div className="flex flex-wrap gap-3">
              {primaryMatch ? (
                <Button asChild>
                  <Link to={discoverLibraryHref(primaryMatch)}>Open in Library</Link>
                </Button>
              ) : (
                <div className="rounded-[var(--radius-lg)] border border-dashed border-white/15 bg-white/6 px-4 py-3 text-sm text-white/75">
                  Not in your server yet.
                </div>
              )}
              <Button asChild variant="outline">
                <Link to="/discover">Back to Discover</Link>
              </Button>
            </div>

            {(details.library_matches?.length ?? 0) > 0 ? (
              <div className="rounded-[var(--radius-lg)] border border-white/10 bg-black/20 p-4">
                <div className="text-sm font-medium text-white">Available in Plum</div>
                <div className="mt-3 flex flex-wrap gap-2">
                  {details.library_matches?.map((match) => (
                    <Link
                      key={`${match.library_id}-${match.kind}-${match.show_key ?? "root"}`}
                      to={discoverLibraryHref(match)}
                      className="inline-flex items-center gap-2 rounded-full border border-white/10 bg-white/8 px-3 py-1.5 text-xs font-medium text-white/80 transition-colors hover:bg-white/12"
                    >
                      <span>{match.library_name}</span>
                      <span className="text-white/45">•</span>
                      <span className="uppercase tracking-[0.14em] text-white/55">
                        {match.kind}
                      </span>
                    </Link>
                  ))}
                </div>
              </div>
            ) : null}
          </div>
        </div>
      </section>

      <section className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_320px]">
        <div className="rounded-[var(--radius-xl)] border border-[var(--plum-border)] bg-[var(--plum-panel)] p-6">
          <h2 className="text-lg font-semibold text-[var(--plum-text)]">Videos</h2>
          {videos.length === 0 ? (
            <p className="mt-3 text-sm text-[var(--plum-muted)]">
              TMDB did not return any trailer or featurette links for this title.
            </p>
          ) : (
            <div className="mt-4 grid gap-3">
              {videos.map((video) => (
                <a
                  key={`${video.site}-${video.key}-${video.type}`}
                  href={video.href}
                  target="_blank"
                  rel="noreferrer"
                  className="flex items-center justify-between gap-4 rounded-[var(--radius-lg)] border border-[var(--plum-border)] bg-[var(--plum-panel-alt)] px-4 py-3 text-sm text-[var(--plum-text)] transition-colors hover:border-[var(--plum-accent-soft)]"
                >
                  <div className="min-w-0">
                    <div className="truncate font-medium">{video.name}</div>
                    <div className="text-xs uppercase tracking-[0.14em] text-[var(--plum-muted)]">
                      {video.site} • {video.type}
                    </div>
                  </div>
                  <ExternalLink className="size-4 shrink-0 text-[var(--plum-muted)]" />
                </a>
              ))}
            </div>
          )}
        </div>

        <aside className="rounded-[var(--radius-xl)] border border-[var(--plum-border)] bg-[var(--plum-panel)] p-6">
          <h2 className="text-lg font-semibold text-[var(--plum-text)]">At a glance</h2>
          <div className="mt-4 flex flex-wrap gap-2">
            {details.genres.map((genre) => (
              <span
                key={genre}
                className="rounded-full border border-[var(--plum-border)] bg-[var(--plum-panel-alt)] px-3 py-1.5 text-xs font-medium text-[var(--plum-text)]"
              >
                {genre}
              </span>
            ))}
          </div>
          <dl className="mt-6 space-y-4 text-sm">
            {details.release_date ? <DetailRow label="Release date" value={details.release_date} /> : null}
            {details.first_air_date ? (
              <DetailRow label="First air date" value={details.first_air_date} />
            ) : null}
            {details.status ? <DetailRow label="Status" value={details.status} /> : null}
            {details.number_of_seasons ? (
              <DetailRow label="Seasons" value={String(details.number_of_seasons)} />
            ) : null}
            {details.number_of_episodes ? (
              <DetailRow label="Episodes" value={String(details.number_of_episodes)} />
            ) : null}
            {details.runtime ? (
              <DetailRow
                label={details.media_type === "movie" ? "Runtime" : "Episode runtime"}
                value={`${details.runtime} min`}
              />
            ) : null}
          </dl>
        </aside>
      </section>
    </div>
  );
}

function DetailRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-start justify-between gap-4">
      <dt className="text-[var(--plum-muted)]">{label}</dt>
      <dd className="text-right font-medium text-[var(--plum-text)]">{value}</dd>
    </div>
  );
}

function DiscoverDetailMessage({
  title,
  copy,
  actionLabel,
  onAction,
}: {
  title: string;
  copy: string;
  actionLabel?: string;
  onAction?: () => void;
}) {
  return (
    <div className="rounded-[var(--radius-xl)] border border-dashed border-[var(--plum-border)] bg-[var(--plum-panel)]/45 p-8">
      <div className="max-w-xl space-y-2">
        <h1 className="text-lg font-semibold text-[var(--plum-text)]">{title}</h1>
        <p className="text-sm leading-6 text-[var(--plum-muted)]">{copy}</p>
        <div className="pt-2">
          {actionLabel && onAction ? (
            <button
              type="button"
              className="text-sm font-medium text-[var(--plum-accent)] hover:underline"
              onClick={onAction}
            >
              {actionLabel}
            </button>
          ) : (
            <Button asChild variant="outline">
              <Link to="/discover">Back to Discover</Link>
            </Button>
          )}
        </div>
      </div>
    </div>
  );
}
