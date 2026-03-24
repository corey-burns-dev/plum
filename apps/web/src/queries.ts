import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseMutationResult,
  type UseQueryResult,
} from "@tanstack/react-query";
import {
  confirmShow,
  getDiscover,
  getDiscoverTitleDetails,
  fetchLibraryMedia,
  fetchSeriesByTmdbId,
  getHomeDashboard,
  searchDiscover,
  type HomeDashboard,
  getTranscodingSettings,
  identifyLibrary,
  listLibraries,
  refreshShow,
  scanLibraryById,
  type DiscoverLibraryMatch,
  type DiscoverMediaType,
  type DiscoverResponse,
  type DiscoverSearchResponse,
  type DiscoverTitleDetails,
  type IdentifyResult,
  type Library,
  type MediaItem,
  type ScanLibraryResult,
  type SeriesDetails,
  type ShowActionResult,
  type TranscodingSettings,
  type TranscodingSettingsResponse,
  type UpdateLibraryPlaybackPreferencesPayload,
  updateLibraryPlaybackPreferences,
  updateTranscodingSettings,
} from "./api";

type LibrariesResult = Awaited<ReturnType<typeof listLibraries>>;
type DiscoverResult = Awaited<ReturnType<typeof getDiscover>>;
type DiscoverSearchResult = Awaited<ReturnType<typeof searchDiscover>>;
type DiscoverTitleDetailsResult = Awaited<ReturnType<typeof getDiscoverTitleDetails>>;
type LibraryMediaResult = Awaited<ReturnType<typeof fetchLibraryMedia>>;
type HomeDashboardResult = Awaited<ReturnType<typeof getHomeDashboard>>;
type TranscodingSettingsResult = Awaited<ReturnType<typeof getTranscodingSettings>>;

function cloneLibrary(library: LibrariesResult[number]): Library {
  return { ...library };
}

function cloneMediaItem(item: LibraryMediaResult[number]): MediaItem {
  return {
    ...item,
    subtitles: item.subtitles?.map((subtitle) => ({ ...subtitle })),
    embeddedSubtitles: item.embeddedSubtitles?.map((subtitle) => ({ ...subtitle })),
    embeddedAudioTracks: item.embeddedAudioTracks?.map((track) => ({ ...track })),
  };
}

function cloneDiscoverLibraryMatch(match: DiscoverLibraryMatch): DiscoverLibraryMatch {
  return { ...match };
}

function cloneDiscoverItem(item: DiscoverResult["shelves"][number]["items"][number]) {
  return {
    ...item,
    library_matches: item.library_matches?.map(cloneDiscoverLibraryMatch),
  };
}

function cloneDiscoverResponse(response: DiscoverResult): DiscoverResponse {
  return {
    shelves: response.shelves.map((shelf) => ({
      ...shelf,
      items: shelf.items.map(cloneDiscoverItem),
    })),
  };
}

function cloneDiscoverSearchResponse(response: DiscoverSearchResult): DiscoverSearchResponse {
  return {
    movies: response.movies.map(cloneDiscoverItem),
    tv: response.tv.map(cloneDiscoverItem),
  };
}

function cloneDiscoverTitleDetails(
  details: DiscoverTitleDetailsResult,
): DiscoverTitleDetails | null {
  if (details == null) {
    return null;
  }
  return {
    ...details,
    genres: [...details.genres],
    videos: details.videos.map((video) => ({ ...video })),
    library_matches: details.library_matches?.map(cloneDiscoverLibraryMatch),
  };
}

function cloneHomeDashboard(dashboard: HomeDashboardResult): HomeDashboard {
  return {
    ...dashboard,
    continueWatching: dashboard.continueWatching.map((entry) => ({
      ...entry,
      media: cloneMediaItem(entry.media),
    })),
    recentlyAdded: (dashboard.recentlyAdded ?? []).map((entry) => ({
      ...entry,
      media: cloneMediaItem(entry.media),
    })),
  };
}

function cloneTranscodingSettingsResponse(
  response: TranscodingSettingsResult,
): TranscodingSettingsResponse {
  return {
    settings: {
      ...response.settings,
      decodeCodecs: { ...response.settings.decodeCodecs },
      encodeFormats: { ...response.settings.encodeFormats },
    },
    warnings: response.warnings.map((warning) => ({ ...warning })),
  };
}

export const queryKeys = {
  discover: ["discover"] as const,
  discoverSearch: (query: string) => ["discover-search", query] as const,
  discoverTitle: (mediaType: DiscoverMediaType, tmdbId: number) =>
    ["discover-title", mediaType, tmdbId] as const,
  home: ["home"] as const,
  libraries: ["libraries"] as const,
  library: (id: number) => ["library", id] as const,
  series: (tmdbId: number) => ["series", tmdbId] as const,
  transcodingSettings: ["transcoding-settings"] as const,
};

const LIBRARIES_STALE_MS = 60 * 1000;
const LIBRARY_MEDIA_STALE_MS = 60 * 1000;
const DISCOVER_STALE_MS = 5 * 60 * 1000;

export function useLibraries(): UseQueryResult<Library[], Error> {
  return useQuery({
    queryKey: queryKeys.libraries,
    queryFn: async () => (await listLibraries()).map(cloneLibrary),
    staleTime: LIBRARIES_STALE_MS,
  });
}

export function useDiscover(options?: { enabled?: boolean }): UseQueryResult<DiscoverResponse, Error> {
  return useQuery({
    queryKey: queryKeys.discover,
    queryFn: async () => cloneDiscoverResponse(await getDiscover()),
    enabled: options?.enabled ?? true,
    staleTime: DISCOVER_STALE_MS,
  });
}

export function useDiscoverSearch(
  query: string,
  options?: { enabled?: boolean },
): UseQueryResult<DiscoverSearchResponse, Error> {
  const normalizedQuery = query.trim();
  return useQuery({
    queryKey: queryKeys.discoverSearch(normalizedQuery),
    queryFn: async () => cloneDiscoverSearchResponse(await searchDiscover(normalizedQuery)),
    enabled: (options?.enabled ?? true) && normalizedQuery.length >= 2,
    staleTime: DISCOVER_STALE_MS,
  });
}

export function useDiscoverTitleDetails(
  mediaType: DiscoverMediaType | null,
  tmdbId: number | null,
  options?: { enabled?: boolean },
): UseQueryResult<DiscoverTitleDetails | null, Error> {
  return useQuery({
    queryKey: queryKeys.discoverTitle(mediaType ?? "movie", tmdbId ?? 0),
    queryFn: async () => cloneDiscoverTitleDetails(await getDiscoverTitleDetails(mediaType!, tmdbId!)),
    enabled: (options?.enabled ?? true) && mediaType != null && tmdbId != null && tmdbId > 0,
    staleTime: DISCOVER_STALE_MS,
  });
}

export function useHomeDashboard(options?: {
  enabled?: boolean;
}): UseQueryResult<HomeDashboard, Error> {
  return useQuery({
    queryKey: queryKeys.home,
    queryFn: async () => cloneHomeDashboard(await getHomeDashboard()),
    enabled: options?.enabled ?? true,
    staleTime: LIBRARY_MEDIA_STALE_MS,
  });
}

export function useLibraryMedia(
  libraryId: number | null,
  options?: { enabled?: boolean; refetchInterval?: number | false },
): UseQueryResult<MediaItem[], Error> {
  return useQuery({
    queryKey: queryKeys.library(libraryId ?? 0),
    queryFn: async () => (await fetchLibraryMedia(libraryId!)).map(cloneMediaItem),
    enabled: (options?.enabled ?? true) && libraryId != null,
    refetchInterval: options?.refetchInterval,
    staleTime: LIBRARY_MEDIA_STALE_MS,
  });
}

export function useScanLibrary(): UseMutationResult<
  ScanLibraryResult,
  Error,
  { libraryId: number; identify?: boolean; subpath?: string }
> {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ libraryId, identify, subpath }) =>
      scanLibraryById(libraryId, { identify, subpath }),
    onSuccess: (_, { libraryId }) => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.library(libraryId) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.libraries });
    },
  });
}

export function useIdentifyLibrary(): UseMutationResult<
  IdentifyResult,
  Error,
  { libraryId: number; signal?: AbortSignal }
> {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ libraryId, signal }) => identifyLibrary(libraryId, { signal }),
    onSuccess: (_, { libraryId }) => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.library(libraryId) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.libraries });
    },
  });
}

export function useUpdateLibraryPlaybackPreferences(): UseMutationResult<
  Library,
  Error,
  { libraryId: number; payload: UpdateLibraryPlaybackPreferencesPayload }
> {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ libraryId, payload }) => updateLibraryPlaybackPreferences(libraryId, payload),
    onSuccess: (library) => {
      queryClient.setQueryData<Library[]>(
        queryKeys.libraries,
        (current) =>
          current?.map((item) => (item.id === library.id ? { ...item, ...library } : item)) ?? [
            cloneLibrary(library),
          ],
      );
    },
  });
}

const SERIES_STALE_MS = 5 * 60 * 1000;

export function useSeries(
  tmdbId: number | null,
  options?: { enabled?: boolean },
): UseQueryResult<SeriesDetails | null, Error> {
  return useQuery({
    queryKey: queryKeys.series(tmdbId ?? 0),
    queryFn: () => fetchSeriesByTmdbId(tmdbId!),
    enabled: (options?.enabled ?? true) && tmdbId != null && tmdbId > 0,
    staleTime: SERIES_STALE_MS,
  });
}

export function useRefreshShow(): UseMutationResult<
  ShowActionResult,
  Error,
  { libraryId: number; showKey: string }
> {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ libraryId, showKey }) => refreshShow(libraryId, showKey),
    onSuccess: (_, { libraryId }) => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.library(libraryId) });
    },
  });
}

export function useConfirmShow(): UseMutationResult<
  ShowActionResult,
  Error,
  { libraryId: number; showKey: string }
> {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ libraryId, showKey }) => confirmShow(libraryId, { showKey }),
    onSuccess: (_, { libraryId }) => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.library(libraryId) });
    },
  });
}

export function useTranscodingSettings(options?: {
  enabled?: boolean;
}): UseQueryResult<TranscodingSettingsResponse, Error> {
  return useQuery({
    queryKey: queryKeys.transcodingSettings,
    queryFn: async () => cloneTranscodingSettingsResponse(await getTranscodingSettings()),
    enabled: options?.enabled ?? true,
    staleTime: 30_000,
  });
}

export function useUpdateTranscodingSettings(): UseMutationResult<
  TranscodingSettingsResponse,
  Error,
  TranscodingSettings
> {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (settings) =>
      cloneTranscodingSettingsResponse(await updateTranscodingSettings(settings)),
    onSuccess: (data) => {
      queryClient.setQueryData(queryKeys.transcodingSettings, data);
    },
  });
}
