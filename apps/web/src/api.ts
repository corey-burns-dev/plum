import { createPlumApiClient, ensureBaseUrl } from "@plum/shared";

export type {
  CreateLibraryPayload,
  CredentialsPayload,
  CreatePlaybackSessionPayload,
  EmbeddedAudioTrack,
  EmbeddedSubtitle,
  HardwareEncodeFormat,
  HomeDashboard,
  IdentifyResult,
  Library,
  LibraryScanStatus,
  LibraryType,
  MatchStatus,
  MediaItem,
  PlaybackSession,
  PlumWebSocketEvent,
  ScanLibraryResult,
  SeriesDetails,
  SeriesSearchResult,
  SetupStatus,
  ShowActionResult,
  Subtitle,
  TranscodingSettings,
  TranscodingSettingsResponse,
  TranscodingSettingsWarning,
  UpdatePlaybackSessionAudioPayload,
  UpdateLibraryPlaybackPreferencesPayload,
  UpdateMediaProgressPayload,
  User,
  VaapiDecodeCodec,
} from "@plum/shared";

export const BASE_URL = ensureBaseUrl(import.meta.env.VITE_BACKEND_URL as string | undefined);

const client = createPlumApiClient({
  baseUrl: BASE_URL,
  fetch: globalThis.fetch.bind(globalThis),
});

export const {
  getSetupStatus,
  createAdmin,
  login,
  logout,
  getMe,
  createLibrary,
  getLibraryScanStatus,
  listLibraries,
  updateLibraryPlaybackPreferences,
  startLibraryScan,
  scanLibraryById,
  identifyLibrary,
  fetchSeriesByTmdbId,
  fetchLibraryMedia,
  getHomeDashboard,
  fetchMediaList,
  createPlaybackSession,
  updatePlaybackSessionAudio,
  closePlaybackSession,
  startTranscode,
  cancelTranscode,
  getTranscodingSettings,
  updateTranscodingSettings,
  searchSeries,
  refreshShow,
  identifyShow,
  updateMediaProgress,
} = client;

export { client as plumApiClient };
