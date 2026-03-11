import { useCallback, useEffect, useMemo, useRef, useState, type MouseEvent as ReactMouseEvent } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import type { Library } from '../api'
import { usePlayer } from '../contexts/PlayerContext'
import type { ShowGroup } from '../lib/showGrouping'
import { groupMediaByShow } from '../lib/showGrouping'
import { useIdentifyLibrary, useLibraryMedia, useLibraries, useRefreshShow } from '../queries'
import { IdentifyShowDialog } from '../components/IdentifyShowDialog'
import { LibraryPosterGrid } from '../components/LibraryPosterGrid'
import { MusicLibraryView } from '../components/MusicLibraryView'

const isTVOrAnime = (lib: Library) => lib.type === 'tv' || lib.type === 'anime'

export function Home() {
  const { libraryId: libraryIdParam } = useParams()
  const navigate = useNavigate()
  const { playMedia, playMusicCollection } = usePlayer()
  const {
    data: libraries = [],
    isLoading: loadingLibs,
    error: loadLibsError,
    refetch: refetchLibraries,
  } = useLibraries()
  const selectedLibraryId = useMemo(() => {
    const id = libraryIdParam ? parseInt(libraryIdParam, 10) : null
    if (id != null && libraries.some((library) => library.id === id)) return id
    return libraries[0]?.id ?? null
  }, [libraryIdParam, libraries])
  const {
    data: selectedItems = [],
    isLoading: selectedLoading,
    error: selectedError,
    refetch: refetchLibraryMedia,
  } = useLibraryMedia(selectedLibraryId)
  const identifyMutation = useIdentifyLibrary()
  const identifyLibraryInBackground = identifyMutation.mutateAsync
  const refreshShowMutation = useRefreshShow()
  const queuedLibsRef = useRef<Set<number>>(new Set())
  const identifiedLibsRef = useRef<Set<number>>(new Set())
  const identifyingLibsRef = useRef<Set<number>>(new Set())
  const identifyRetryCountsRef = useRef<Map<number, number>>(new Map())
  const identifyOrderRef = useRef<number[]>([])
  const identifyPumpRunningRef = useRef(false)
  const [identifyingLibraryIds, setIdentifyingLibraryIds] = useState<number[]>([])
  const selectedLib = libraries.find((library) => library.id === selectedLibraryId)

  const [contextMenu, setContextMenu] = useState<{ x: number; y: number; group: ShowGroup } | null>(null)
  const [identifyGroup, setIdentifyGroup] = useState<ShowGroup | null>(null)
  const contextMenuRef = useRef<HTMLDivElement>(null)
  const closeContextMenu = useCallback(() => setContextMenu(null), [])

  useEffect(() => {
    if (!contextMenu) return
    const onMouseDown = (event: MouseEvent) => {
      if (contextMenuRef.current?.contains(event.target as Node)) return
      closeContextMenu()
    }
    document.addEventListener('mousedown', onMouseDown)
    return () => document.removeEventListener('mousedown', onMouseDown)
  }, [contextMenu, closeContextMenu])

  useEffect(() => {
    if (libraryIdParam != null || libraries.length === 0) return
    navigate(`/library/${libraries[0].id}`, { replace: true })
  }, [libraryIdParam, libraries, navigate])

  const setLibraryIdentifying = useCallback((libraryId: number, identifying: boolean) => {
    setIdentifyingLibraryIds((current) => {
      if (identifying) {
        return current.includes(libraryId) ? current : [...current, libraryId]
      }
      return current.filter((id) => id !== libraryId)
    })
  }, [])

  const pumpIdentifyQueue = useCallback(async () => {
    if (identifyPumpRunningRef.current) return
    identifyPumpRunningRef.current = true
    try {
      while (true) {
        const nextLibraryId = identifyOrderRef.current.find((libraryId) =>
          queuedLibsRef.current.has(libraryId)
        )
        if (nextLibraryId == null) return

        queuedLibsRef.current.delete(nextLibraryId)
        identifyingLibsRef.current.add(nextLibraryId)
        setLibraryIdentifying(nextLibraryId, true)

        try {
          await identifyLibraryInBackground(nextLibraryId)
          identifiedLibsRef.current.add(nextLibraryId)
          identifyRetryCountsRef.current.delete(nextLibraryId)
        } catch {
          const retries = identifyRetryCountsRef.current.get(nextLibraryId) ?? 0
          if (retries < 1) {
            identifyRetryCountsRef.current.set(nextLibraryId, retries + 1)
            queuedLibsRef.current.add(nextLibraryId)
          }
        } finally {
          identifyingLibsRef.current.delete(nextLibraryId)
          setLibraryIdentifying(nextLibraryId, false)
        }
      }
    } finally {
      identifyPumpRunningRef.current = false
    }
  }, [identifyLibraryInBackground, setLibraryIdentifying])

  useEffect(() => {
    const identifyableLibraries = libraries
      .filter((library) => library.type !== 'music')
      .map((library) => library.id)
    const activeIds = new Set(identifyableLibraries)
    identifyOrderRef.current =
      selectedLibraryId != null && activeIds.has(selectedLibraryId)
        ? [selectedLibraryId, ...identifyableLibraries.filter((libraryId) => libraryId !== selectedLibraryId)]
        : identifyableLibraries

    for (const libraryId of [...queuedLibsRef.current]) {
      if (!activeIds.has(libraryId)) queuedLibsRef.current.delete(libraryId)
    }
    for (const libraryId of [...identifyingLibsRef.current]) {
      if (!activeIds.has(libraryId)) {
        identifyingLibsRef.current.delete(libraryId)
        setLibraryIdentifying(libraryId, false)
      }
    }
    for (const libraryId of [...identifiedLibsRef.current]) {
      if (!activeIds.has(libraryId)) identifiedLibsRef.current.delete(libraryId)
    }
    for (const libraryId of [...identifyRetryCountsRef.current.keys()]) {
      if (!activeIds.has(libraryId)) identifyRetryCountsRef.current.delete(libraryId)
    }

    for (const libraryId of identifyOrderRef.current) {
      if (identifiedLibsRef.current.has(libraryId)) continue
      if (identifyingLibsRef.current.has(libraryId)) continue
      if (queuedLibsRef.current.has(libraryId)) continue
      queuedLibsRef.current.add(libraryId)
    }

    void pumpIdentifyQueue()
    setIdentifyingLibraryIds((current) => current.filter((libraryId) => activeIds.has(libraryId)))
  }, [libraries, pumpIdentifyQueue, selectedLibraryId, setLibraryIdentifying])

  const selectedLibraryIdentifying =
    selectedLibraryId != null && identifyingLibraryIds.includes(selectedLibraryId)

  const showGroups = useMemo(
    () => (selectedLib && isTVOrAnime(selectedLib) ? groupMediaByShow(selectedItems) : []),
    [selectedItems, selectedLib]
  )

  const showCards = useMemo(
    () =>
      showGroups.map((group) => ({
        key: group.showKey,
        title: group.showTitle,
        subtitle: `${group.episodes.length} episode${group.episodes.length === 1 ? '' : 's'}${group.unmatchedCount > 0 ? ` • ${group.unmatchedCount} unmatched` : group.localCount > 0 ? ` • ${group.localCount} local` : ''}`,
        posterPath: group.posterPath,
        isIdentifying:
          selectedLibraryIdentifying &&
          (group.unmatchedCount > 0 || group.localCount > 0 || !group.posterPath),
        statusLabel: 'Identifying…',
        href: `/library/${selectedLibraryId}/show/${encodeURIComponent(group.showKey)}`,
        onContextMenu: (event: ReactMouseEvent<HTMLDivElement>) => {
          event.preventDefault()
          setContextMenu({ x: event.clientX, y: event.clientY, group })
        },
      })),
    [selectedLibraryId, selectedLibraryIdentifying, showGroups]
  )

  const movieCards = useMemo(
    () =>
      selectedItems.map((item) => {
        const year = item.release_date?.split('-')[0] || item.title.match(/\((\d{4})\)$/)?.[1] || 'Unknown year'
        const status = item.match_status && item.match_status !== 'identified' ? ` • ${item.match_status}` : ''
        const rating = item.vote_average ? ` • ${item.vote_average.toFixed(1)}` : ''
        return {
          key: String(item.id),
          title: item.title,
          subtitle: `${year}${rating}${status}`,
          posterPath: item.poster_path,
          isIdentifying:
            selectedLibraryIdentifying && (item.match_status !== 'identified' || !item.poster_path),
          statusLabel: 'Identifying…',
          onClick: () => playMedia(item),
        }
      }),
    [playMedia, selectedItems, selectedLibraryIdentifying]
  )

  return (
    <>
      {loadingLibs ? (
        <p className="text-sm text-[var(--plum-muted)]">Loading libraries…</p>
      ) : loadLibsError ? (
        <p className="text-sm text-[var(--plum-muted)]">
          Failed to load libraries: {loadLibsError.message}{' '}
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
                  {selectedError.message}{' '}
                  <button
                    type="button"
                    className="text-[var(--plum-accent)] hover:underline"
                    onClick={() => void refetchLibraryMedia()}
                  >
                    Retry
                  </button>
                </p>
              ) : selectedItems.length === 0 ? (
                <p className="text-sm text-[var(--plum-muted)]">No media in this library yet.</p>
              ) : isTVOrAnime(selectedLib) ? (
                <LibraryPosterGrid items={showCards} />
              ) : selectedLib.type === 'movie' ? (
                <LibraryPosterGrid items={movieCards} />
              ) : (
                <MusicLibraryView
                  items={selectedItems}
                  onPlayCollection={playMusicCollection}
                />
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
                        { onSettled: closeContextMenu }
                      )
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
                        `/library/${selectedLibraryId}/show/${encodeURIComponent(contextMenu.group.showKey)}`
                      )
                      closeContextMenu()
                    }}
                  >
                    Edit show
                  </button>
                  <button
                    type="button"
                    className="w-full px-3 py-2 text-left text-sm hover:bg-[var(--plum-bg)]"
                    onClick={() => {
                      setIdentifyGroup(contextMenu.group)
                      closeContextMenu()
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
  )
}
