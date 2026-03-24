import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { Sparkles, Star } from "lucide-react";
import { tmdbPosterUrl } from "@plum/shared";
import type { DiscoverItem, DiscoverResponse } from "@/api";
import { Input } from "@/components/ui/input";
import { discoverMediaLabel, discoverYear } from "@/lib/discover";
import { useDiscover, useDiscoverSearch } from "@/queries";

function isDiscoverConfigError(error: Error | null): boolean {
  return error?.message.includes("TMDB_API_KEY") ?? false;
}

export function Discover() {
  const [searchInput, setSearchInput] = useState("");
  const [searchQuery, setSearchQuery] = useState("");
  const {
    data: discover,
    error: discoverError,
    isLoading: discoverLoading,
    refetch: refetchDiscover,
  } = useDiscover();

  useEffect(() => {
    const trimmed = searchInput.trim();
    if (trimmed.length < 2) {
      setSearchQuery("");
      return;
    }
    const timeoutId = window.setTimeout(() => {
      setSearchQuery(trimmed);
    }, 300);
    return () => window.clearTimeout(timeoutId);
  }, [searchInput]);

  const searchActive = searchQuery.length >= 2;
  const {
    data: searchResults,
    error: searchError,
    isLoading: searchLoading,
    refetch: refetchSearch,
  } = useDiscoverSearch(searchQuery, { enabled: searchActive });
  const activeError = searchActive ? searchError : discoverError;
  const isConfigError = isDiscoverConfigError(activeError);

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-8">
      <section className="rounded-[var(--radius-xl)] border border-[var(--plum-border)] bg-[radial-gradient(circle_at_top_left,rgba(244,90,160,0.22),transparent_45%),linear-gradient(135deg,rgba(15,23,42,0.96),rgba(18,24,38,0.88))] p-6 text-white shadow-[0_20px_70px_rgba(9,12,20,0.28)]">
        <div className="flex flex-col gap-6 lg:flex-row lg:items-end lg:justify-between">
          <div className="max-w-2xl space-y-3">
            <div className="inline-flex items-center gap-2 rounded-full border border-white/15 bg-white/10 px-3 py-1 text-xs font-semibold uppercase tracking-[0.24em] text-white/75">
              <Sparkles className="size-3.5" />
              Discover
            </div>
            <div className="space-y-2">
              <h1 className="text-3xl font-semibold tracking-tight">Find something worth adding.</h1>
              <p className="max-w-xl text-sm leading-6 text-white/75">
                Browse live TMDB-powered shelves for movies and TV, then jump into rich title
                details with Plum-aware library status.
              </p>
            </div>
          </div>

          <div className="w-full max-w-xl">
            <Input
              type="search"
              value={searchInput}
              onChange={(event) => setSearchInput(event.target.value)}
              placeholder="Search movies and TV shows"
              className="h-11 border-white/10 bg-black/20 text-white placeholder:text-white/45"
            />
            <p className="mt-2 text-xs text-white/55">
              Search kicks in after 2 characters with a 300ms delay.
            </p>
          </div>
        </div>
      </section>

      {isConfigError ? (
        <DiscoverMessage
          title="Discover needs TMDB configured"
          copy="Set `TMDB_API_KEY` on the server to enable external shelves, search, and title details."
          actionLabel="Retry"
          onAction={() => {
            if (searchActive) {
              void refetchSearch();
              return;
            }
            void refetchDiscover();
          }}
        />
      ) : activeError ? (
        <DiscoverMessage
          title="Discover is unavailable right now"
          copy={activeError.message}
          actionLabel="Retry"
          onAction={() => {
            if (searchActive) {
              void refetchSearch();
              return;
            }
            void refetchDiscover();
          }}
        />
      ) : searchActive ? (
        <DiscoverSearchResults
          query={searchQuery}
          loading={searchLoading}
          results={searchResults}
        />
      ) : discoverLoading ? (
        <p className="text-sm text-[var(--plum-muted)]">Loading discover shelves...</p>
      ) : (
        <DiscoverShelves discover={discover} />
      )}
    </div>
  );
}

function DiscoverShelves({ discover }: { discover: DiscoverResponse | undefined }) {
  if (!discover?.shelves.length) {
    return (
      <DiscoverMessage
        title="Nothing to surface yet"
        copy="Plum could not load any discover shelves from TMDB."
      />
    );
  }

  return (
    <div className="flex flex-col gap-8">
      {discover.shelves.map((shelf) => (
        <section key={shelf.id} className="flex flex-col gap-4">
          <div className="flex items-center justify-between gap-4">
            <h2 className="text-lg font-semibold text-[var(--plum-text)]">{shelf.title}</h2>
            <span className="text-sm text-[var(--plum-muted)]">
              {shelf.items.length} title{shelf.items.length === 1 ? "" : "s"}
            </span>
          </div>
          <DiscoverCardRail items={shelf.items} />
        </section>
      ))}
    </div>
  );
}

function DiscoverSearchResults({
  query,
  loading,
  results,
}: {
  query: string;
  loading: boolean;
  results: { movies: DiscoverItem[]; tv: DiscoverItem[] } | undefined;
}) {
  if (loading && !results) {
    return <p className="text-sm text-[var(--plum-muted)]">Searching TMDB...</p>;
  }

  const movies = results?.movies ?? [];
  const tv = results?.tv ?? [];

  if (movies.length === 0 && tv.length === 0) {
    return (
      <DiscoverMessage
        title={`No results for "${query}"`}
        copy="Try another title, a shorter query, or browse one of the shelves instead."
      />
    );
  }

  return (
    <div className="flex flex-col gap-8">
      <section className="flex flex-col gap-4">
        <div className="flex items-center justify-between gap-4">
          <h2 className="text-lg font-semibold text-[var(--plum-text)]">Movies</h2>
          <span className="text-sm text-[var(--plum-muted)]">{movies.length} matches</span>
        </div>
        <DiscoverGrid items={movies} emptyLabel="No movie matches." />
      </section>

      <section className="flex flex-col gap-4">
        <div className="flex items-center justify-between gap-4">
          <h2 className="text-lg font-semibold text-[var(--plum-text)]">TV Shows</h2>
          <span className="text-sm text-[var(--plum-muted)]">{tv.length} matches</span>
        </div>
        <DiscoverGrid items={tv} emptyLabel="No TV matches." />
      </section>
    </div>
  );
}

function DiscoverGrid({ items, emptyLabel }: { items: DiscoverItem[]; emptyLabel: string }) {
  if (items.length === 0) {
    return (
      <div className="rounded-[var(--radius-xl)] border border-dashed border-[var(--plum-border)] bg-[var(--plum-panel)]/45 p-6 text-sm text-[var(--plum-muted)]">
        {emptyLabel}
      </div>
    );
  }

  return (
    <div className="grid grid-cols-[repeat(auto-fill,minmax(170px,1fr))] gap-4">
      {items.map((item) => (
        <DiscoverCard key={`${item.media_type}-${item.tmdb_id}`} item={item} />
      ))}
    </div>
  );
}

function DiscoverCardRail({ items }: { items: DiscoverItem[] }) {
  return (
    <div className="flex gap-4 overflow-x-auto pb-2">
      {items.map((item) => (
        <DiscoverCard key={`${item.media_type}-${item.tmdb_id}`} item={item} rail />
      ))}
    </div>
  );
}

function DiscoverCard({ item, rail = false }: { item: DiscoverItem; rail?: boolean }) {
  const posterUrl = tmdbPosterUrl(item.poster_path, "w500");
  const year = discoverYear(item);
  const inLibrary = (item.library_matches?.length ?? 0) > 0;

  return (
    <Link
      to={`/discover/${item.media_type}/${item.tmdb_id}`}
      className={`group flex ${rail ? "w-44 shrink-0" : "w-full"} flex-col overflow-hidden rounded-[var(--radius-xl)] border border-[var(--plum-border)] bg-[var(--plum-panel)] shadow-[0_14px_40px_rgba(8,12,24,0.12)] transition-transform duration-200 hover:-translate-y-1 hover:border-[var(--plum-accent-soft)] hover:bg-[var(--plum-panel-alt)]`}
    >
      <div className="relative aspect-[2/3] overflow-hidden bg-[var(--plum-panel-alt)]">
        {posterUrl ? (
          <img
            src={posterUrl}
            alt=""
            className="h-full w-full object-cover transition-transform duration-300 group-hover:scale-[1.04]"
          />
        ) : (
          <img src="/placeholder-poster.svg" alt="" className="h-full w-full object-cover" />
        )}
        <div className="absolute inset-x-0 top-0 flex items-start justify-between gap-2 p-3">
          <span className="rounded-full bg-black/60 px-2.5 py-1 text-[11px] font-medium uppercase tracking-[0.18em] text-white/75 backdrop-blur-sm">
            {discoverMediaLabel(item.media_type)}
          </span>
          {inLibrary ? (
            <span className="rounded-full bg-[var(--plum-accent)] px-2.5 py-1 text-[11px] font-semibold uppercase tracking-[0.15em] text-white shadow-[0_0_18px_rgba(244,90,160,0.35)]">
              In Library
            </span>
          ) : null}
        </div>
      </div>

      <div className="flex flex-1 flex-col gap-2 p-3">
        <div className="line-clamp-2 text-sm font-semibold text-[var(--plum-text)]">
          {item.title}
        </div>
        <div className="flex items-center justify-between gap-3 text-xs text-[var(--plum-muted)]">
          <span>{year || "Upcoming"}</span>
          {item.vote_average ? (
            <span className="inline-flex items-center gap-1">
              <Star className="size-3 fill-current text-[var(--plum-accent)]" />
              {item.vote_average.toFixed(1)}
            </span>
          ) : null}
        </div>
      </div>
    </Link>
  );
}

function DiscoverMessage({
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
        <h2 className="text-lg font-semibold text-[var(--plum-text)]">{title}</h2>
        <p className="text-sm leading-6 text-[var(--plum-muted)]">{copy}</p>
        {actionLabel && onAction ? (
          <button
            type="button"
            className="mt-2 text-sm font-medium text-[var(--plum-accent)] hover:underline"
            onClick={onAction}
          >
            {actionLabel}
          </button>
        ) : null}
      </div>
    </div>
  );
}
