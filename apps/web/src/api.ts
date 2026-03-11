import { createPlumApiClient, ensureBaseUrl } from "@plum/shared";

export type {
  CreateLibraryPayload,
  CredentialsPayload,
  EmbeddedSubtitle,
  HardwareEncodeFormat,
  IdentifyResult,
  Library,
  LibraryType,
  MatchStatus,
  MediaItem,
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
  listLibraries,
  scanLibraryById,
  identifyLibrary,
  fetchSeriesByTmdbId,
  fetchLibraryMedia,
  fetchMediaList,
  startTranscode,
  cancelTranscode,
  getTranscodingSettings,
  updateTranscodingSettings,
  searchSeries,
  refreshShow,
  identifyShow,
} = client;

export { client as plumApiClient };
