import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseMutationResult,
  type UseQueryResult,
} from '@tanstack/react-query'
import {
  fetchLibraryMedia,
  fetchSeriesByTmdbId,
  identifyLibrary,
  listLibraries,
  refreshShow,
  scanLibraryById,
  type IdentifyResult,
  type Library,
  type MediaItem,
  type ScanLibraryResult,
  type SeriesDetails,
  type ShowActionResult,
} from './api'

export const queryKeys = {
  libraries: ['libraries'] as const,
  library: (id: number) => ['library', id] as const,
  series: (tmdbId: number) => ['series', tmdbId] as const,
}

const LIBRARIES_STALE_MS = 60 * 1000
const LIBRARY_MEDIA_STALE_MS = 60 * 1000

export function useLibraries(): UseQueryResult<Library[], Error> {
  return useQuery({
    queryKey: queryKeys.libraries,
    queryFn: listLibraries,
    staleTime: LIBRARIES_STALE_MS,
  })
}

export function useLibraryMedia(
  libraryId: number | null,
  options?: { enabled?: boolean },
): UseQueryResult<MediaItem[], Error> {
  return useQuery({
    queryKey: queryKeys.library(libraryId ?? 0),
    queryFn: () => fetchLibraryMedia(libraryId!),
    enabled: (options?.enabled ?? true) && libraryId != null,
    staleTime: LIBRARY_MEDIA_STALE_MS,
  })
}

export function useScanLibrary(): UseMutationResult<
  ScanLibraryResult,
  Error,
  { libraryId: number; identify?: boolean }
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ libraryId, identify }) =>
      scanLibraryById(libraryId, { identify }),
    onSuccess: (_, { libraryId }) => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.library(libraryId) })
      void queryClient.invalidateQueries({ queryKey: queryKeys.libraries })
    },
  })
}

export function useIdentifyLibrary(): UseMutationResult<
  IdentifyResult,
  Error,
  number
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: identifyLibrary,
    onSuccess: (_, libraryId) => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.library(libraryId) })
      void queryClient.invalidateQueries({ queryKey: queryKeys.libraries })
    },
  })
}

const SERIES_STALE_MS = 5 * 60 * 1000

export function useSeries(
  tmdbId: number | null,
  options?: { enabled?: boolean },
): UseQueryResult<SeriesDetails | null, Error> {
  return useQuery({
    queryKey: queryKeys.series(tmdbId ?? 0),
    queryFn: () => fetchSeriesByTmdbId(tmdbId!),
    enabled: (options?.enabled ?? true) && tmdbId != null && tmdbId > 0,
    staleTime: SERIES_STALE_MS,
  })
}

export function useRefreshShow(): UseMutationResult<
  ShowActionResult,
  Error,
  { libraryId: number; showKey: string }
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ libraryId, showKey }) => refreshShow(libraryId, showKey),
    onSuccess: (_, { libraryId }) => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.library(libraryId) })
    },
  })
}
