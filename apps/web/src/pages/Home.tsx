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
import { LibraryPosterGrid, type PosterGridItem } from "../components/LibraryPosterGrid";
import { MusicLibraryView } from "../components/MusicLibraryView";
import { useIdentifyQueue, type IdentifyLibraryPhase } from "../contexts/IdentifyQueueContext";
import { usePlayer } from "../contexts/PlayerContext";
import { useScanQueue } from "../contexts/ScanQueueContext";
import { getLibraryActivity } from "../lib/libraryActivity";
import { formatEpisodeLabel, formatRemainingTime, shouldShowProgress } from "../lib/progress";
import type { ShowGroup } from "../lib/showGrouping";
import { groupMediaByShow } from "../lib/showGrouping";
import { useConfirmShow, useLibraryMedia, useLibraries, useRefreshShow } from "../queries";

const isTVOrAnime = (lib: Library) => lib.type === "tv" || lib.type === "anime";
const IDENTIFY_POLL_INTERVAL_MS = 5_000;
const SCAN_POLL_INTERVAL_MS = 2_000;
type ItemIdentifyState = "queued" | "identifying" | "failed" | undefined;

const hasProviderMatch = (tmdbId?: number, tvdbId?: string) =>
  Boolean(tmdbId && tmdbId > 0) || Boolean(tvdbId);
const isExplicitlyUnmatched = (matchStatus?: string) =>
  matchStatus === "local" || matchStatus === "unmatched";
const isActiveIdentifyState = (identifyState?: ItemIdentifyState) =>
  identifyState === "queued" || identifyState === "identifying";

function canShowFailureState(
  identifyPhase: IdentifyLibraryPhase | undefined,
  isProcessing: boolean,
  isFetching: boolean,
  hasActiveIdentifyItems: boolean,
  identifyFailedCount: number,
) {
  return (
    !isProcessing &&
    !isFetching &&
    !hasActiveIdentifyItems &&
    (identifyPhase === "identify-failed" ||
      (identifyPhase === "complete" && identifyFailedCount > 0))
  );
}

function shouldDeferIncompleteCard(
  identifyState: ItemIdentifyState,
  identifyPhase: IdentifyLibraryPhase | undefined,
) {
  return identifyState == null && identifyPhase === "queued";
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

function resolveLibraryIdentifyPhase(
  localPhase: IdentifyLibraryPhase | undefined,
  backendPhase: IdentifyLibraryPhase | undefined,
) {
  if (localPhase === "queued" || localPhase === "identifying" || localPhase === "soft-reveal") {
    return localPhase;
  }
  if (backendPhase === "queued" || backendPhase === "identifying") {
    return backendPhase;
  }
  return localPhase ?? backendPhase;
}

function isMovieIncomplete(item: {
  match_status?: string;
  poster_path?: string;
  tmdb_id?: number;
  tvdb_id?: string;
}) {
  return isExplicitlyUnmatched(item.match_status);
}

function isMovieTerminalFailure(
  item: {
    identify_state?: ItemIdentifyState;
    match_status?: string;
    poster_path?: string;
    tmdb_id?: number;
    tvdb_id?: string;
  },
  libraryCanShowFailure: boolean,
) {
  return (
    isMovieIncomplete(item) &&
    !isActiveIdentifyState(item.identify_state) &&
    (item.identify_state === "failed" || libraryCanShowFailure)
  );
}

function getGroupIdentifyState(group: ShowGroup): ItemIdentifyState {
  if (group.episodes.some((episode) => episode.identify_state === "identifying"))
    return "identifying";
  if (group.episodes.some((episode) => episode.identify_state === "queued")) return "queued";
  if (group.episodes.some((episode) => episode.identify_state === "failed")) return "failed";
  return undefined;
}

type LibraryContextMenuState =
  | {
      kind: "show";
      x: number;
      y: number;
      group: ShowGroup;
    }
  | {
      kind: "movie";
      x: number;
      y: number;
      movieId: number;
      canRetryIdentify: boolean;
    };

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
  const selectedLibraryBackendIdentifyPhase = mapBackendIdentifyPhase(
    selectedLibraryScanStatus?.identifyPhase,
  );
  const selectedLibraryIdentifyPhase = resolveLibraryIdentifyPhase(
    getLibraryPhase(selectedLibraryId),
    selectedLibraryBackendIdentifyPhase,
  );
  const selectedLibraryActivity = getLibraryActivity({
    scanPhase: selectedLibraryScanStatus?.phase,
    enriching: selectedLibraryScanStatus?.enriching === true,
    identifyPhase: selectedLibraryScanStatus?.identifyPhase,
    localIdentifyPhase: selectedLibraryIdentifyPhase,
  });
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
  } = useLibraryMedia(selectedLibraryId, {
    refetchInterval: selectedLibraryPollInterval,
  });
  const selectedLibraryScanWarning =
    selectedLibraryScanStatus?.phase === "completed" && selectedItems.length === 0
      ? selectedLibraryScanStatus.error
      : undefined;
  const refreshShowMutation = useRefreshShow();
  const confirmShowMutation = useConfirmShow();
  const selectedLib = libraries.find((library) => library.id === selectedLibraryId);

  const [contextMenu, setContextMenu] = useState<LibraryContextMenuState | null>(null);
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

  const hasActiveIdentifyItems = selectedItems.some((item) =>
    isActiveIdentifyState(item.identify_state),
  );
  const selectedLibraryCanShowFailure = canShowFailureState(
    selectedLibraryIdentifyPhase,
    isSelectedLibraryScanning,
    selectedFetching,
    hasActiveIdentifyItems,
    selectedLibraryScanStatus?.identifyFailed ?? 0,
  );
  const hasIdentifyProgress = selectedItems.some((item) => {
    if (isActiveIdentifyState(item.identify_state)) return true;
    return item.match_status === "identified" || hasProviderMatch(item.tmdb_id, item.tvdb_id);
  });
  const shouldRevealSearchingCards =
    selectedLibraryIdentifyPhase === "soft-reveal" ||
    selectedLibraryIdentifyPhase === "identifying" ||
    hasIdentifyProgress;

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
      const imdbRating = group.episodes.find(
        (episode) => (episode.imdb_rating ?? 0) > 0,
      )?.imdb_rating;
      const needsMetadataReview = group.episodes.some(
        (episode) => episode.metadata_review_needed === true,
      );
      const isConfirmingReview =
        confirmShowMutation.isPending &&
        confirmShowMutation.variables?.libraryId === selectedLibraryId &&
        confirmShowMutation.variables?.showKey === group.showKey;
      const identifyState = getGroupIdentifyState(group);
      const isIncomplete = group.unmatchedCount > 0 || group.localCount > 0;
      if (isIncomplete && shouldDeferIncompleteCard(identifyState, selectedLibraryIdentifyPhase)) {
        deferredGroups.push(group);
        return [];
      }
      const showSearching =
        isIncomplete &&
        (isActiveIdentifyState(identifyState) ||
          (identifyState == null && shouldRevealSearchingCards && !selectedLibraryCanShowFailure));
      const showFailure =
        isIncomplete &&
        !showSearching &&
        !needsMetadataReview &&
        !isActiveIdentifyState(identifyState) &&
        (identifyState === "failed" || selectedLibraryCanShowFailure);

      return [
        {
          key: group.showKey,
          title: group.showTitle,
          subtitle: `${group.episodes.length} episode${group.episodes.length === 1 ? "" : "s"}${group.unmatchedCount > 0 ? ` • ${group.unmatchedCount} unmatched` : group.localCount > 0 ? ` • ${group.localCount} local` : ""}`,
          metaLine: progressEpisode
            ? [
                formatEpisodeLabel(progressEpisode),
                formatRemainingTime(progressEpisode.remaining_seconds),
              ]
                .filter(Boolean)
                .join(" • ")
            : undefined,
          posterPath: group.posterPath,
          posterUrl: group.posterUrl,
          imdbRating,
          progressPercent: progressEpisode?.progress_percent,
          cardState: needsMetadataReview
            ? "review-needed"
            : showSearching
              ? "identifying"
              : showFailure
                ? "identify-failed"
                : "default",
          statusLabel: needsMetadataReview
            ? "Is this correct?"
            : showSearching
              ? "Searching…"
              : showFailure
                ? "Couldn't match automatically"
                : undefined,
          statusActionLabel:
            needsMetadataReview && selectedLibraryId != null
              ? "Confirm"
              : showFailure && selectedLibraryId != null
                ? "Identify manually"
                : undefined,
          statusActionDisabled: isConfirmingReview,
          onStatusAction:
            needsMetadataReview && selectedLibraryId != null
              ? () =>
                  confirmShowMutation.mutate({
                    libraryId: selectedLibraryId,
                    showKey: group.showKey,
                  })
              : showFailure && selectedLibraryId != null
                ? () => setIdentifyGroup(group)
                : undefined,
          href: `/library/${selectedLibraryId}/show/${encodeURIComponent(group.showKey)}`,
          onPlay: () => playShowGroup(group.episodes, progressEpisode),
          onContextMenu: (event: ReactMouseEvent<HTMLDivElement>) => {
            event.preventDefault();
            setContextMenu({ kind: "show", x: event.clientX, y: event.clientY, group });
          },
        },
      ] satisfies PosterGridItem[];
    });
    return { deferredCount: deferredGroups.length, visibleCards };
  }, [
    confirmShowMutation,
    playShowGroup,
    shouldRevealSearchingCards,
    selectedLibraryIdentifyPhase,
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
      const isIncomplete = isMovieIncomplete(item);
      if (
        isIncomplete &&
        shouldDeferIncompleteCard(item.identify_state, selectedLibraryIdentifyPhase)
      ) {
        deferredCount += 1;
        return [];
      }
      const showSearching =
        isIncomplete &&
        (isActiveIdentifyState(item.identify_state) ||
          (item.identify_state == null &&
            shouldRevealSearchingCards &&
            !selectedLibraryCanShowFailure));
      const showFailure = isMovieTerminalFailure(item, selectedLibraryCanShowFailure);

      return [
        {
          key: String(item.id),
          title: item.title,
          subtitle: `${year}${status}`,
          metaLine: formatRemainingTime(item.remaining_seconds),
          posterPath: item.poster_path,
          posterUrl: item.poster_url,
          imdbRating: item.imdb_rating,
          progressPercent: shouldShowProgress(item) ? item.progress_percent : undefined,
          cardState: showSearching ? "identifying" : showFailure ? "identify-failed" : "default",
          statusLabel: showSearching
            ? "Searching…"
            : showFailure
              ? "Couldn't match automatically"
              : undefined,
          statusActionLabel:
            showFailure && selectedLibraryId != null ? "Retry identify" : undefined,
          onStatusAction:
            showFailure && selectedLibraryId != null
              ? () =>
                  queueLibraryIdentify(selectedLibraryId, {
                    prioritize: true,
                    resetState: true,
                  })
              : undefined,
          href: selectedLibraryId != null ? `/library/${selectedLibraryId}/movie/${item.id}` : undefined,
          onPlay: () => playMovie(item),
          onContextMenu: (event: ReactMouseEvent<HTMLDivElement>) => {
            event.preventDefault();
            setContextMenu({
              kind: "movie",
              x: event.clientX,
              y: event.clientY,
              movieId: item.id,
              canRetryIdentify: showFailure,
            });
          },
        },
      ] satisfies PosterGridItem[];
    });

    return { deferredCount, visibleCards };
  }, [
    playMovie,
    queueLibraryIdentify,
    selectedItems,
    selectedLibraryIdentifyPhase,
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
              ) : selectedLibraryActivity != null && selectedItems.length === 0 ? (
                <p className="text-sm text-[var(--plum-muted)]">
                  {selectedLibraryActivity === "importing"
                    ? "Importing library…"
                    : selectedLibraryActivity === "finishing"
                      ? "Finishing library…"
                      : "Identifying library…"}
                  {selectedLibraryActivity === "importing" && selectedLibraryScanStatus && (
                    <>
                      {" "}
                      {selectedLibraryScanStatus.processed} processed •{" "}
                      {selectedLibraryScanStatus.added} added
                    </>
                  )}
                </p>
              ) : selectedLibraryScanWarning ? (
                <p className="text-sm text-[var(--plum-muted)]">{selectedLibraryScanWarning}</p>
              ) : selectedItems.length === 0 ? (
                <p className="text-sm text-[var(--plum-muted)]">No media in this library yet.</p>
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
                  role="menu"
                  aria-label={contextMenu.kind === "show" ? "Show actions" : "Movie actions"}
                  className="fixed z-50 min-w-[10rem] rounded-[var(--radius-md)] border border-[var(--plum-border)] bg-[var(--plum-panel)] py-1 text-[var(--plum-text)] shadow-lg"
                  style={{ left: contextMenu.x, top: contextMenu.y }}
                >
                  {contextMenu.kind === "show" ? (
                    <>
                      <button
                        type="button"
                        className="w-full px-3 py-2 text-left text-sm hover:bg-[var(--plum-bg)]"
                        onClick={() => {
                          refreshShowMutation.mutate(
                            {
                              libraryId: selectedLibraryId,
                              showKey: contextMenu.group.showKey,
                            },
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
                        Open details
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
                    </>
                  ) : (
                    <>
                      {contextMenu.canRetryIdentify ? (
                        <button
                          type="button"
                          className="w-full px-3 py-2 text-left text-sm hover:bg-[var(--plum-bg)]"
                          onClick={() => {
                            queueLibraryIdentify(selectedLibraryId, {
                              prioritize: true,
                              resetState: true,
                            });
                            closeContextMenu();
                          }}
                        >
                          Retry identify
                        </button>
                      ) : null}
                      <button
                        type="button"
                        className="w-full px-3 py-2 text-left text-sm hover:bg-[var(--plum-bg)]"
                        onClick={() => {
                          navigate(`/library/${selectedLibraryId}/movie/${contextMenu.movieId}`);
                          closeContextMenu();
                        }}
                      >
                        Open details
                      </button>
                    </>
                  )}
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
