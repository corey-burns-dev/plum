import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type MouseEvent as ReactMouseEvent,
} from "react";
import { useNavigate, useParams } from "react-router-dom";
import type { Library } from "../api";
import { IdentifyShowDialog } from "../components/IdentifyShowDialog";
import { LibraryPosterGrid } from "../components/LibraryPosterGrid";
import { MusicLibraryView } from "../components/MusicLibraryView";
import { useIdentifyQueue, type IdentifyLibraryPhase } from "../contexts/IdentifyQueueContext";
import { usePlayer } from "../contexts/PlayerContext";
import { useScanQueue } from "../contexts/ScanQueueContext";
import { formatEpisodeLabel, formatRemainingTime, shouldShowProgress } from "../lib/progress";
import type { ShowGroup } from "../lib/showGrouping";
import { groupMediaByShow } from "../lib/showGrouping";
import { useLibraryMedia, useLibraries, useRefreshShow } from "../queries";

const isTVOrAnime = (lib: Library) => lib.type === "tv" || lib.type === "anime";
const IDENTIFY_POLL_INTERVAL_MS = 5_000;
const SCAN_POLL_INTERVAL_MS = 2_000;

const hasProviderMatch = (tmdbId?: number, tvdbId?: string) =>
  Boolean(tmdbId && tmdbId > 0) || Boolean(tvdbId);
const isExplicitlyUnmatched = (matchStatus?: string) =>
  matchStatus === "local" || matchStatus === "unmatched";

function canShowFailureState(identifyPhase: IdentifyLibraryPhase | undefined, isFetching: boolean) {
  return identifyPhase === "identify-failed" || (identifyPhase === "complete" && !isFetching);
}

function shouldDeferIncompleteCard(
  identifyPhase: IdentifyLibraryPhase | undefined,
  shouldRevealSearchingCards: boolean,
) {
  return (
    identifyPhase === "queued" || (identifyPhase === "identifying" && !shouldRevealSearchingCards)
  );
}

function mapBackendIdentifyPhase(phase?: string): IdentifyLibraryPhase | undefined {
  switch (phase) {
    case "queued":
      return "queued";
    case "identifying":
      return "identifying";
    case "completed":
      return "complete";
    case "failed":
      return "identify-failed";
    default:
      return undefined;
  }
}

export function Home() {
  const { libraryId: libraryIdParam } = useParams();
  const navigate = useNavigate();
  const { playMovie, playMusicCollection, playShowGroup } = usePlayer();
  const { getLibraryPhase, queueLibraryIdentify } = useIdentifyQueue();
  const { getLibraryScanStatus } = useScanQueue();
  const {
    data: libraries = [],
    isLoading: loadingLibs,
    error: loadLibsError,
    refetch: refetchLibraries,
  } = useLibraries();
  const selectedLibraryId = useMemo(() => {
    const id = libraryIdParam ? parseInt(libraryIdParam, 10) : null;
    if (id != null && libraries.some((library) => library.id === id)) return id;
    return libraries[0]?.id ?? null;
  }, [libraryIdParam, libraries]);
  const selectedLibraryScanStatus = getLibraryScanStatus(selectedLibraryId);
  const selectedLibraryIdentifyPhase =
    getLibraryPhase(selectedLibraryId) ??
    mapBackendIdentifyPhase(selectedLibraryScanStatus?.identifyPhase);
  const isSelectedLibraryScanning =
    selectedLibraryScanStatus?.phase === "queued" ||
    selectedLibraryScanStatus?.phase === "scanning" ||
    selectedLibraryScanStatus?.enriching === true ||
    selectedLibraryScanStatus?.identifyPhase === "queued" ||
    selectedLibraryScanStatus?.identifyPhase === "identifying";
  const selectedLibraryPollInterval =
    selectedLibraryId == null
      ? false
      : isSelectedLibraryScanning
        ? SCAN_POLL_INTERVAL_MS
        : selectedLibraryIdentifyPhase === "identifying" ||
            selectedLibraryIdentifyPhase === "soft-reveal"
          ? IDENTIFY_POLL_INTERVAL_MS
          : false;
  const {
    data: selectedItems = [],
    isFetching: selectedFetching,
    isLoading: selectedLoading,
    error: selectedError,
    refetch: refetchLibraryMedia,
  } = useLibraryMedia(selectedLibraryId, { refetchInterval: selectedLibraryPollInterval });
  const refreshShowMutation = useRefreshShow();
  const selectedLib = libraries.find((library) => library.id === selectedLibraryId);

  const [contextMenu, setContextMenu] = useState<{ x: number; y: number; group: ShowGroup } | null>(
    null,
  );
  const [identifyGroup, setIdentifyGroup] = useState<ShowGroup | null>(null);
  const contextMenuRef = useRef<HTMLDivElement>(null);
  const closeContextMenu = useCallback(() => setContextMenu(null), []);

  useEffect(() => {
    if (!contextMenu) return;
    const onMouseDown = (event: MouseEvent) => {
      if (contextMenuRef.current?.contains(event.target as Node)) return;
      closeContextMenu();
    };
    document.addEventListener("mousedown", onMouseDown);
    return () => document.removeEventListener("mousedown", onMouseDown);
  }, [contextMenu, closeContextMenu]);

  useEffect(() => {
    if (libraryIdParam != null || libraries.length === 0) return;
    navigate(`/library/${libraries[0].id}`, { replace: true });
  }, [libraryIdParam, libraries, navigate]);

  const selectedLibraryCanShowFailure = canShowFailureState(
    selectedLibraryIdentifyPhase,
    selectedFetching,
  );
  const hasIdentifyProgress = selectedItems.some(
    (item) => item.match_status === "identified" || hasProviderMatch(item.tmdb_id, item.tvdb_id),
  );
  const shouldRevealSearchingCards =
    selectedLibraryIdentifyPhase === "soft-reveal" ||
    (selectedLibraryIdentifyPhase === "identifying" && hasIdentifyProgress);
  const deferIncompleteCards = shouldDeferIncompleteCard(
    selectedLibraryIdentifyPhase,
    shouldRevealSearchingCards,
  );

  const showGroups = useMemo(
    () => (selectedLib && isTVOrAnime(selectedLib) ? groupMediaByShow(selectedItems) : []),
    [selectedItems, selectedLib],
  );

  const showCardState = useMemo(() => {
    const deferredGroups: ShowGroup[] = [];
    const visibleCards = showGroups.flatMap((group) => {
      const progressEpisode = [...group.episodes]
        .filter((episode) => shouldShowProgress(episode))
        .toSorted((a, b) => (b.last_watched_at ?? "").localeCompare(a.last_watched_at ?? ""))[0];
      const imdbRating = group.episodes.find((episode) => (episode.imdb_rating ?? 0) > 0)?.imdb_rating;
      const hasMatchedEpisode = group.episodes.some(
        (episode) =>
          episode.match_status === "identified" ||
          hasProviderMatch(episode.tmdb_id, episode.tvdb_id),
      );
      const isIncomplete =
        group.unmatchedCount > 0 ||
        group.localCount > 0 ||
        (!group.posterPath && hasMatchedEpisode);
      if (isIncomplete && deferIncompleteCards) {
        deferredGroups.push(group);
        return [];
      }

      return [
        {
          key: group.showKey,
          title: group.showTitle,
          subtitle: `${group.episodes.length} episode${group.episodes.length === 1 ? "" : "s"}${group.unmatchedCount > 0 ? ` • ${group.unmatchedCount} unmatched` : group.localCount > 0 ? ` • ${group.localCount} local` : ""}`,
          metaLine: progressEpisode
            ? [formatEpisodeLabel(progressEpisode), formatRemainingTime(progressEpisode.remaining_seconds)]
                .filter(Boolean)
                .join(" • ")
            : undefined,
          posterPath: group.posterPath,
          imdbRating,
          progressPercent: progressEpisode?.progress_percent,
          cardState:
            isIncomplete && shouldRevealSearchingCards
              ? "identifying"
              : isIncomplete && selectedLibraryCanShowFailure
                ? "identify-failed"
                : "default",
          statusLabel:
            isIncomplete && shouldRevealSearchingCards
              ? "Searching…"
              : isIncomplete && selectedLibraryCanShowFailure
                ? "Couldn't match automatically"
                : undefined,
          statusActionLabel:
            isIncomplete && selectedLibraryCanShowFailure && selectedLibraryId != null
              ? "Identify manually"
              : undefined,
          onStatusAction:
            isIncomplete && selectedLibraryCanShowFailure && selectedLibraryId != null
              ? () => setIdentifyGroup(group)
              : undefined,
          href: `/library/${selectedLibraryId}/show/${encodeURIComponent(group.showKey)}`,
          onPlay: () => playShowGroup(group.episodes, progressEpisode),
          onContextMenu: (event: ReactMouseEvent<HTMLDivElement>) => {
            event.preventDefault();
            setContextMenu({ x: event.clientX, y: event.clientY, group });
          },
        },
      ];
    });
    return { deferredCount: deferredGroups.length, visibleCards };
  }, [
    deferIncompleteCards,
    playShowGroup,
    shouldRevealSearchingCards,
    selectedLibraryCanShowFailure,
    selectedLibraryId,
    showGroups,
  ]);

  const movieCardState = useMemo(() => {
    let deferredCount = 0;
    const visibleCards = selectedItems.flatMap((item) => {
      const year =
        item.release_date?.split("-")[0] || item.title.match(/\((\d{4})\)$/)?.[1] || "Unknown year";
      const status =
        item.match_status && item.match_status !== "identified" ? ` • ${item.match_status}` : "";
      const isIncomplete =
        isExplicitlyUnmatched(item.match_status) ||
        (!item.poster_path &&
          (item.match_status === "identified" || hasProviderMatch(item.tmdb_id, item.tvdb_id)));
      if (isIncomplete && deferIncompleteCards) {
        deferredCount += 1;
        return [];
      }

      return [
        {
          key: String(item.id),
          title: item.title,
          subtitle: `${year}${status}`,
          metaLine: formatRemainingTime(item.remaining_seconds),
          posterPath: item.poster_path,
          imdbRating: item.imdb_rating,
          progressPercent: shouldShowProgress(item) ? item.progress_percent : undefined,
          cardState:
            isIncomplete && shouldRevealSearchingCards
              ? "identifying"
              : isIncomplete && selectedLibraryCanShowFailure
                ? "identify-failed"
                : "default",
          statusLabel:
            isIncomplete && shouldRevealSearchingCards
              ? "Searching…"
              : isIncomplete && selectedLibraryCanShowFailure
                ? "Couldn't match automatically"
                : undefined,
          statusActionLabel:
            isIncomplete && selectedLibraryCanShowFailure && selectedLibraryId != null
              ? "Retry identify"
              : undefined,
          onStatusAction:
            isIncomplete && selectedLibraryCanShowFailure && selectedLibraryId != null
              ? () =>
                  queueLibraryIdentify(selectedLibraryId, {
                    prioritize: true,
                    resetState: true,
                  })
              : undefined,
          onClick: () => playMovie(item),
          onPlay: () => playMovie(item),
        },
      ];
    });

    return { deferredCount, visibleCards };
  }, [
    deferIncompleteCards,
    playMovie,
    queueLibraryIdentify,
    selectedItems,
    shouldRevealSearchingCards,
    selectedLibraryCanShowFailure,
    selectedLibraryId,
  ]);

  const deferredCardCount =
    selectedLib == null || selectedLib.type === "music"
      ? 0
      : isTVOrAnime(selectedLib)
        ? showCardState.deferredCount
        : selectedLib.type === "movie"
          ? movieCardState.deferredCount
          : 0;
  const visibleCardCount =
    selectedLib == null || selectedLib.type === "music"
      ? 0
      : isTVOrAnime(selectedLib)
        ? showCardState.visibleCards.length
        : selectedLib.type === "movie"
          ? movieCardState.visibleCards.length
          : 0;
  const showIdentifyPlaceholder =
    selectedLib != null &&
    selectedLib.type !== "music" &&
    deferredCardCount > 0 &&
    visibleCardCount === 0 &&
    (selectedLibraryIdentifyPhase === "queued" || selectedLibraryIdentifyPhase === "identifying");

  return (
    <>
      {loadingLibs ? (
        <p className="text-sm text-[var(--plum-muted)]">Loading libraries…</p>
      ) : loadLibsError ? (
        <p className="text-sm text-[var(--plum-muted)]">
          Failed to load libraries: {loadLibsError.message}{" "}
          <button
            type="button"
            className="text-[var(--plum-accent)] hover:underline"
            onClick={() => void refetchLibraries()}
          >
            Retry
          </button>
        </p>
      ) : libraries.length === 0 ? (
        <p className="text-sm text-[var(--plum-muted)]">
          No libraries yet. Add one in Settings or onboarding.
        </p>
      ) : (
        <>
          {selectedLib && (
            <div className="flex min-h-0 flex-1 flex-col">
              {selectedLoading ? (
                <p className="text-sm text-[var(--plum-muted)]">Loading…</p>
              ) : selectedError ? (
                <p className="text-sm text-[var(--plum-muted)]">
                  {selectedError.message}{" "}
                  <button
                    type="button"
                    className="text-[var(--plum-accent)] hover:underline"
                    onClick={() => void refetchLibraryMedia()}
                  >
                    Retry
                  </button>
                </p>
              ) : isSelectedLibraryScanning && selectedItems.length === 0 ? (
                <p className="text-sm text-[var(--plum-muted)]">
                  Importing library…
                  {selectedLibraryScanStatus && (
                    <>
                      {" "}
                      {selectedLibraryScanStatus.processed} processed •{" "}
                      {selectedLibraryScanStatus.added} added
                    </>
                  )}
                </p>
              ) : selectedItems.length === 0 ? (
                <p className="text-sm text-[var(--plum-muted)]">
                  {isSelectedLibraryScanning ? "Importing library…" : "No media in this library yet."}
                </p>
              ) : showIdentifyPlaceholder ? (
                <p className="text-sm text-[var(--plum-muted)]">Identifying library…</p>
              ) : isTVOrAnime(selectedLib) ? (
                <LibraryPosterGrid items={showCardState.visibleCards} />
              ) : selectedLib.type === "movie" ? (
                <LibraryPosterGrid items={movieCardState.visibleCards} />
              ) : (
                <MusicLibraryView items={selectedItems} onPlayCollection={playMusicCollection} />
              )}
              {contextMenu && selectedLibraryId != null && (
                <div
                  ref={contextMenuRef}
                  className="fixed z-50 min-w-[10rem] rounded-[var(--radius-md)] border border-[var(--plum-border)] bg-[var(--plum-panel)] py-1 text-[var(--plum-text)] shadow-lg"
                  style={{ left: contextMenu.x, top: contextMenu.y }}
                >
                  <button
                    type="button"
                    className="w-full px-3 py-2 text-left text-sm hover:bg-[var(--plum-bg)]"
                    onClick={() => {
                      refreshShowMutation.mutate(
                        { libraryId: selectedLibraryId, showKey: contextMenu.group.showKey },
                        { onSettled: closeContextMenu },
                      );
                    }}
                    disabled={refreshShowMutation.isPending}
                  >
                    Refresh metadata
                  </button>
                  <button
                    type="button"
                    className="w-full px-3 py-2 text-left text-sm hover:bg-[var(--plum-bg)]"
                    onClick={() => {
                      navigate(
                        `/library/${selectedLibraryId}/show/${encodeURIComponent(contextMenu.group.showKey)}`,
                      );
                      closeContextMenu();
                    }}
                  >
                    Edit show
                  </button>
                  <button
                    type="button"
                    className="w-full px-3 py-2 text-left text-sm hover:bg-[var(--plum-bg)]"
                    onClick={() => {
                      setIdentifyGroup(contextMenu.group);
                      closeContextMenu();
                    }}
                  >
                    Identify…
                  </button>
                </div>
              )}
              {identifyGroup && selectedLibraryId != null && (
                <IdentifyShowDialog
                  open={!!identifyGroup}
                  onOpenChange={(open) => !open && setIdentifyGroup(null)}
                  libraryId={selectedLibraryId}
                  showKey={identifyGroup.showKey}
                  showTitle={identifyGroup.showTitle}
                  onSuccess={() => void refetchLibraryMedia()}
                />
              )}
            </div>
          )}
        </>
      )}
    </>
  );
}
