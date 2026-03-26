import { Schema } from "effect";

/**
 * Library type determines identification/scan behavior and which category table is used.
 * TV and anime use TMDB for episodes; movie uses TMDB; music does not.
 */
export type LibraryType = "tv" | "movie" | "music" | "anime";

export const LibraryTypeSchema = Schema.Literals(["tv", "movie", "music", "anime"]);

/**
 * Media item type stored per item; matches library type for identification.
 */
export type MediaType = "tv" | "movie" | "music" | "anime";

export const MediaTypeSchema = Schema.Literals(["tv", "movie", "music", "anime"]);

export type MatchStatus = "identified" | "local" | "unmatched";

export const MatchStatusSchema = Schema.Literals(["identified", "local", "unmatched"]);

export type IdentifyState = "queued" | "identifying" | "failed";

export const IdentifyStateSchema = Schema.Literals(["queued", "identifying", "failed"]);

export interface Subtitle {
  id: number;
  title: string;
  language: string;
  format: string;
}

export const SubtitleSchema = Schema.Struct({
  id: Schema.Number,
  title: Schema.String,
  language: Schema.String,
  format: Schema.String,
});

export interface EmbeddedSubtitle {
  streamIndex: number;
  language: string;
  title: string;
}

export const EmbeddedSubtitleSchema = Schema.Struct({
  streamIndex: Schema.Number,
  language: Schema.String,
  title: Schema.String,
});

export interface EmbeddedAudioTrack {
  streamIndex: number;
  language: string;
  title: string;
}

export const EmbeddedAudioTrackSchema = Schema.Struct({
  streamIndex: Schema.Number,
  language: Schema.String,
  title: Schema.String,
});

export interface MediaItem {
  id: number;
  library_id?: number;
  title: string;
  path: string;
  duration: number;
  type: MediaType;
  match_status?: MatchStatus;
  identify_state?: IdentifyState;
  subtitles?: Subtitle[];
  embeddedSubtitles?: EmbeddedSubtitle[];
  embeddedAudioTracks?: EmbeddedAudioTrack[];
  tmdb_id?: number;
  tvdb_id?: string;
  overview?: string;
  poster_path?: string;
  backdrop_path?: string;
  poster_url?: string;
  backdrop_url?: string;
  release_date?: string;
  vote_average?: number;
  imdb_id?: string;
  imdb_rating?: number;
  artist?: string;
  album?: string;
  album_artist?: string;
  disc_number?: number;
  track_number?: number;
  release_year?: number;
  progress_seconds?: number;
  progress_percent?: number;
  remaining_seconds?: number;
  completed?: boolean;
  last_watched_at?: string;
  /** Set for TV/anime episodes; 0 when not applicable. */
  season?: number;
  episode?: number;
  metadata_review_needed?: boolean;
  metadata_confirmed?: boolean;
  /** Path to generated frame thumbnail (video episodes); served at /api/media/:id/thumbnail. */
  thumbnail_path?: string;
  /** Stable Plum-served thumbnail URL when available. */
  thumbnail_url?: string;
  missing?: boolean;
  missing_since?: string;
  duplicate?: boolean;
  duplicate_count?: number;
}

export const MediaItemSchema = Schema.Struct({
  id: Schema.Number,
  library_id: Schema.optional(Schema.Number),
  title: Schema.String,
  path: Schema.String,
  duration: Schema.Number,
  type: MediaTypeSchema,
  match_status: Schema.optional(MatchStatusSchema),
  identify_state: Schema.optional(IdentifyStateSchema),
  subtitles: Schema.optional(Schema.Array(SubtitleSchema)),
  embeddedSubtitles: Schema.optional(Schema.Array(EmbeddedSubtitleSchema)),
  embeddedAudioTracks: Schema.optional(Schema.Array(EmbeddedAudioTrackSchema)),
  tmdb_id: Schema.optional(Schema.Number),
  tvdb_id: Schema.optional(Schema.String),
  overview: Schema.optional(Schema.String),
  poster_path: Schema.optional(Schema.String),
  backdrop_path: Schema.optional(Schema.String),
  poster_url: Schema.optional(Schema.String),
  backdrop_url: Schema.optional(Schema.String),
  release_date: Schema.optional(Schema.String),
  vote_average: Schema.optional(Schema.Number),
  imdb_id: Schema.optional(Schema.String),
  imdb_rating: Schema.optional(Schema.Number),
  artist: Schema.optional(Schema.String),
  album: Schema.optional(Schema.String),
  album_artist: Schema.optional(Schema.String),
  disc_number: Schema.optional(Schema.Number),
  track_number: Schema.optional(Schema.Number),
  release_year: Schema.optional(Schema.Number),
  progress_seconds: Schema.optional(Schema.Number),
  progress_percent: Schema.optional(Schema.Number),
  remaining_seconds: Schema.optional(Schema.Number),
  completed: Schema.optional(Schema.Boolean),
  last_watched_at: Schema.optional(Schema.String),
  season: Schema.optional(Schema.Number),
  episode: Schema.optional(Schema.Number),
  metadata_review_needed: Schema.optional(Schema.Boolean),
  metadata_confirmed: Schema.optional(Schema.Boolean),
  thumbnail_path: Schema.optional(Schema.String),
  thumbnail_url: Schema.optional(Schema.String),
  missing: Schema.optional(Schema.Boolean),
  missing_since: Schema.optional(Schema.String),
  duplicate: Schema.optional(Schema.Boolean),
  duplicate_count: Schema.optional(Schema.Number),
});

export interface UpdateMediaProgressPayload {
  position_seconds: number;
  duration_seconds: number;
  completed?: boolean;
}

export const UpdateMediaProgressPayloadSchema = Schema.Struct({
  position_seconds: Schema.Number,
  duration_seconds: Schema.Number,
  completed: Schema.optional(Schema.Boolean),
});

export type PlaybackSessionStatus = "starting" | "ready" | "error" | "closed";

export const PlaybackSessionStatusSchema = Schema.Literals([
  "starting",
  "ready",
  "error",
  "closed",
]);

export type PlaybackDelivery = "direct" | "remux" | "transcode";

export const PlaybackDeliverySchema = Schema.Literals([
  "direct",
  "remux",
  "transcode",
]);

export interface ClientPlaybackCapabilities {
  supportsNativeHls: boolean;
  supportsMseHls: boolean;
  videoCodecs: string[];
  audioCodecs: string[];
  containers: string[];
}

export const ClientPlaybackCapabilitiesSchema = Schema.Struct({
  supportsNativeHls: Schema.Boolean,
  supportsMseHls: Schema.Boolean,
  videoCodecs: Schema.Array(Schema.String),
  audioCodecs: Schema.Array(Schema.String),
  containers: Schema.Array(Schema.String),
});

export interface CreatePlaybackSessionPayload {
  audioIndex?: number;
  clientCapabilities?: ClientPlaybackCapabilities;
}

export const CreatePlaybackSessionPayloadSchema = Schema.Struct({
  audioIndex: Schema.optional(Schema.Number),
  clientCapabilities: Schema.optional(ClientPlaybackCapabilitiesSchema),
});

export interface UpdatePlaybackSessionAudioPayload {
  audioIndex: number;
}

export const UpdatePlaybackSessionAudioPayloadSchema = Schema.Struct({
  audioIndex: Schema.Number,
});

export interface DirectPlaybackSession {
  delivery: "direct";
  mediaId: number;
  audioIndex?: number;
  status: PlaybackSessionStatus;
  streamUrl: string;
  error?: string;
}

export interface HlsPlaybackSession {
  sessionId: string;
  delivery: "remux" | "transcode";
  mediaId: number;
  revision: number;
  audioIndex: number;
  status: PlaybackSessionStatus;
  streamUrl: string;
  error?: string;
}

export type PlaybackSession = DirectPlaybackSession | HlsPlaybackSession;

export const DirectPlaybackSessionSchema = Schema.Struct({
  delivery: Schema.Literal("direct"),
  mediaId: Schema.Number,
  audioIndex: Schema.optional(Schema.Number),
  status: PlaybackSessionStatusSchema,
  streamUrl: Schema.String,
  error: Schema.optional(Schema.String),
});

export const HlsPlaybackSessionSchema = Schema.Struct({
  sessionId: Schema.String,
  delivery: Schema.Literals(["remux", "transcode"]),
  mediaId: Schema.Number,
  revision: Schema.Number,
  audioIndex: Schema.Number,
  status: PlaybackSessionStatusSchema,
  streamUrl: Schema.String,
  error: Schema.optional(Schema.String),
});

export const PlaybackSessionSchema = Schema.Union([
  DirectPlaybackSessionSchema,
  HlsPlaybackSessionSchema,
]);

export interface ContinueWatchingEntry {
  kind: "movie" | "show";
  media: MediaItem;
  show_key?: string;
  show_title?: string;
  episode_label?: string;
  remaining_seconds: number;
}

export const ContinueWatchingEntrySchema = Schema.Struct({
  kind: Schema.Literals(["movie", "show"]),
  media: MediaItemSchema,
  show_key: Schema.optional(Schema.String),
  show_title: Schema.optional(Schema.String),
  episode_label: Schema.optional(Schema.String),
  remaining_seconds: Schema.Number,
});

export interface RecentlyAddedEntry {
  kind: "movie" | "show";
  media: MediaItem;
  show_key?: string;
  show_title?: string;
  episode_label?: string;
}

export const RecentlyAddedEntrySchema = Schema.Struct({
  kind: Schema.Literals(["movie", "show"]),
  media: MediaItemSchema,
  show_key: Schema.optional(Schema.String),
  show_title: Schema.optional(Schema.String),
  episode_label: Schema.optional(Schema.String),
});

export interface HomeDashboard {
  continueWatching: ContinueWatchingEntry[];
  recentlyAdded?: RecentlyAddedEntry[];
}

export const HomeDashboardSchema = Schema.Struct({
  continueWatching: Schema.Array(ContinueWatchingEntrySchema),
  recentlyAdded: Schema.optional(Schema.Array(RecentlyAddedEntrySchema)),
});

export interface Library {
  id: number;
  name: string;
  type: LibraryType;
  path: string;
  user_id: number;
  preferred_audio_language?: string;
  preferred_subtitle_language?: string;
  subtitles_enabled_by_default?: boolean;
  watcher_enabled?: boolean;
  watcher_mode?: "auto" | "poll";
  scan_interval_minutes?: number;
}

export const LibrarySchema = Schema.Struct({
  id: Schema.Number,
  name: Schema.String,
  type: LibraryTypeSchema,
  path: Schema.String,
  user_id: Schema.Number,
  preferred_audio_language: Schema.optional(Schema.String),
  preferred_subtitle_language: Schema.optional(Schema.String),
  subtitles_enabled_by_default: Schema.optional(Schema.Boolean),
  watcher_enabled: Schema.optional(Schema.Boolean),
  watcher_mode: Schema.optional(Schema.Literals(["auto", "poll"])),
  scan_interval_minutes: Schema.optional(Schema.Number),
});

export interface UpdateLibraryPlaybackPreferencesPayload {
  preferred_audio_language: string;
  preferred_subtitle_language: string;
  subtitles_enabled_by_default: boolean;
  watcher_enabled?: boolean;
  watcher_mode?: "auto" | "poll";
  scan_interval_minutes?: number;
}

export const UpdateLibraryPlaybackPreferencesPayloadSchema = Schema.Struct({
  preferred_audio_language: Schema.String,
  preferred_subtitle_language: Schema.String,
  subtitles_enabled_by_default: Schema.Boolean,
  watcher_enabled: Schema.optional(Schema.Boolean),
  watcher_mode: Schema.optional(Schema.Literals(["auto", "poll"])),
  scan_interval_minutes: Schema.optional(Schema.Number),
});

export interface CreateLibraryPayload {
  name: string;
  type: LibraryType;
  path: string;
  watcher_enabled?: boolean;
  watcher_mode?: "auto" | "poll";
  scan_interval_minutes?: number;
}

export const CreateLibraryPayloadSchema = Schema.Struct({
  name: Schema.String,
  type: LibraryTypeSchema,
  path: Schema.String,
  watcher_enabled: Schema.optional(Schema.Boolean),
  watcher_mode: Schema.optional(Schema.Literals(["auto", "poll"])),
  scan_interval_minutes: Schema.optional(Schema.Number),
});

export interface CredentialsPayload {
  email: string;
  password: string;
}

export const CredentialsPayloadSchema = Schema.Struct({
  email: Schema.String,
  password: Schema.String,
});

export interface User {
  id: number;
  email: string;
  is_admin: boolean;
}

export const UserSchema = Schema.Struct({
  id: Schema.Number,
  email: Schema.String,
  is_admin: Schema.Boolean,
});

export interface SetupStatus {
  hasAdmin: boolean;
}

export const SetupStatusSchema = Schema.Struct({
  hasAdmin: Schema.Boolean,
});

export interface ScanLibraryResult {
  added: number;
  updated: number;
  removed: number;
  unmatched: number;
  skipped: number;
}

export const ScanLibraryResultSchema = Schema.Struct({
  added: Schema.Number,
  updated: Schema.Number,
  removed: Schema.Number,
  unmatched: Schema.Number,
  skipped: Schema.Number,
});

export type LibraryScanPhase = "idle" | "queued" | "scanning" | "completed" | "failed";

export const LibraryScanPhaseSchema = Schema.Literals([
  "idle",
  "queued",
  "scanning",
  "completed",
  "failed",
]);

export type LibraryIdentifyPhase = "idle" | "queued" | "identifying" | "completed" | "failed";

export const LibraryIdentifyPhaseSchema = Schema.Literals([
  "idle",
  "queued",
  "identifying",
  "completed",
  "failed",
]);

export interface LibraryScanStatus {
  libraryId: number;
  phase: LibraryScanPhase;
  enriching: boolean;
  identifyPhase: LibraryIdentifyPhase;
  identified: number;
  identifyFailed: number;
  processed: number;
  added: number;
  updated: number;
  removed: number;
  unmatched: number;
  skipped: number;
  identifyRequested: boolean;
  queuedAt?: string;
  estimatedItems: number;
  queuePosition: number;
  error?: string;
  retryCount?: number;
  maxRetries?: number;
  nextRetryAt?: string;
  lastError?: string;
  nextScheduledAt?: string;
  startedAt?: string;
  finishedAt?: string;
}

export const LibraryScanStatusSchema = Schema.Struct({
  libraryId: Schema.Number,
  phase: LibraryScanPhaseSchema,
  enriching: Schema.Boolean,
  identifyPhase: LibraryIdentifyPhaseSchema,
  identified: Schema.Number,
  identifyFailed: Schema.Number,
  processed: Schema.Number,
  added: Schema.Number,
  updated: Schema.Number,
  removed: Schema.Number,
  unmatched: Schema.Number,
  skipped: Schema.Number,
  identifyRequested: Schema.Boolean,
  queuedAt: Schema.optional(Schema.String),
  estimatedItems: Schema.Number,
  queuePosition: Schema.Number,
  error: Schema.optional(Schema.String),
  retryCount: Schema.optional(Schema.Number),
  maxRetries: Schema.optional(Schema.Number),
  nextRetryAt: Schema.optional(Schema.String),
  lastError: Schema.optional(Schema.String),
  nextScheduledAt: Schema.optional(Schema.String),
  startedAt: Schema.optional(Schema.String),
  finishedAt: Schema.optional(Schema.String),
});

export interface IdentifyResult {
  identified: number;
  failed: number;
}

export const IdentifyResultSchema = Schema.Struct({
  identified: Schema.Number,
  failed: Schema.Number,
});

export interface CastMember {
  name: string;
  character?: string;
  order?: number;
  profile_path?: string;
}

export const CastMemberSchema = Schema.Struct({
  name: Schema.String,
  character: Schema.optional(Schema.String),
  order: Schema.optional(Schema.Number),
  profile_path: Schema.optional(Schema.String),
});

export interface SeriesDetails {
  name: string;
  overview: string;
  poster_path: string;
  backdrop_path: string;
  poster_url?: string;
  backdrop_url?: string;
  first_air_date: string;
  imdb_id?: string;
  imdb_rating?: number;
  genres: string[];
  cast: CastMember[];
  runtime?: number;
  number_of_seasons?: number;
  number_of_episodes?: number;
}

export const SeriesDetailsSchema = Schema.Struct({
  name: Schema.String,
  overview: Schema.String,
  poster_path: Schema.String,
  backdrop_path: Schema.String,
  poster_url: Schema.optional(Schema.String),
  backdrop_url: Schema.optional(Schema.String),
  first_air_date: Schema.String,
  imdb_id: Schema.optional(Schema.String),
  imdb_rating: Schema.optional(Schema.Number),
  genres: Schema.Array(Schema.String),
  cast: Schema.Array(CastMemberSchema),
  runtime: Schema.optional(Schema.Number),
  number_of_seasons: Schema.optional(Schema.Number),
  number_of_episodes: Schema.optional(Schema.Number),
});

export interface MovieDetails {
  media_id: number;
  library_id: number;
  title: string;
  overview: string;
  poster_path?: string;
  poster_url?: string;
  backdrop_path?: string;
  backdrop_url?: string;
  release_date?: string;
  imdb_id?: string;
  imdb_rating?: number;
  runtime?: number;
  genres: string[];
  cast: CastMember[];
}

export const MovieDetailsSchema = Schema.Struct({
  media_id: Schema.Number,
  library_id: Schema.Number,
  title: Schema.String,
  overview: Schema.String,
  poster_path: Schema.optional(Schema.String),
  poster_url: Schema.optional(Schema.String),
  backdrop_path: Schema.optional(Schema.String),
  backdrop_url: Schema.optional(Schema.String),
  release_date: Schema.optional(Schema.String),
  imdb_id: Schema.optional(Schema.String),
  imdb_rating: Schema.optional(Schema.Number),
  runtime: Schema.optional(Schema.Number),
  genres: Schema.Array(Schema.String),
  cast: Schema.Array(CastMemberSchema),
});

export interface ShowDetails {
  library_id: number;
  show_key: string;
  name: string;
  overview: string;
  poster_path?: string;
  poster_url?: string;
  backdrop_path?: string;
  backdrop_url?: string;
  first_air_date?: string;
  imdb_id?: string;
  imdb_rating?: number;
  runtime?: number;
  number_of_seasons: number;
  number_of_episodes: number;
  genres: string[];
  cast: CastMember[];
}

export const ShowDetailsSchema = Schema.Struct({
  library_id: Schema.Number,
  show_key: Schema.String,
  name: Schema.String,
  overview: Schema.String,
  poster_path: Schema.optional(Schema.String),
  poster_url: Schema.optional(Schema.String),
  backdrop_path: Schema.optional(Schema.String),
  backdrop_url: Schema.optional(Schema.String),
  first_air_date: Schema.optional(Schema.String),
  imdb_id: Schema.optional(Schema.String),
  imdb_rating: Schema.optional(Schema.Number),
  runtime: Schema.optional(Schema.Number),
  number_of_seasons: Schema.Number,
  number_of_episodes: Schema.Number,
  genres: Schema.Array(Schema.String),
  cast: Schema.Array(CastMemberSchema),
});

export type SearchResultKind = "movie" | "show";

export const SearchResultKindSchema = Schema.Literals(["movie", "show"]);

export type SearchMatchReason = "title" | "actor";

export const SearchMatchReasonSchema = Schema.Literals(["title", "actor"]);

export interface SearchResult {
  kind: SearchResultKind;
  library_id: number;
  library_name: string;
  library_type: LibraryType;
  title: string;
  subtitle?: string;
  poster_path?: string;
  poster_url?: string;
  imdb_rating?: number;
  match_reason: SearchMatchReason;
  matched_actor?: string;
  href: string;
  genres?: string[];
}

export const SearchResultSchema = Schema.Struct({
  kind: SearchResultKindSchema,
  library_id: Schema.Number,
  library_name: Schema.String,
  library_type: LibraryTypeSchema,
  title: Schema.String,
  subtitle: Schema.optional(Schema.String),
  poster_path: Schema.optional(Schema.String),
  poster_url: Schema.optional(Schema.String),
  imdb_rating: Schema.optional(Schema.Number),
  match_reason: SearchMatchReasonSchema,
  matched_actor: Schema.optional(Schema.String),
  href: Schema.String,
  genres: Schema.optional(Schema.Array(Schema.String)),
});

export interface SearchFacetValue {
  value: string;
  label: string;
  count: number;
}

export const SearchFacetValueSchema = Schema.Struct({
  value: Schema.String,
  label: Schema.String,
  count: Schema.Number,
});

export interface SearchFacets {
  libraries: SearchFacetValue[];
  types: SearchFacetValue[];
  genres: SearchFacetValue[];
}

export const SearchFacetsSchema = Schema.Struct({
  libraries: Schema.Array(SearchFacetValueSchema),
  types: Schema.Array(SearchFacetValueSchema),
  genres: Schema.Array(SearchFacetValueSchema),
});

export interface SearchResponse {
  query: string;
  results: SearchResult[];
  total: number;
  facets: SearchFacets;
}

export const SearchResponseSchema = Schema.Struct({
  query: Schema.String,
  results: Schema.Array(SearchResultSchema),
  total: Schema.Number,
  facets: SearchFacetsSchema,
});

export interface SeriesSearchResult {
  Title: string;
  Overview: string;
  PosterURL: string;
  BackdropURL: string;
  ReleaseDate: string;
  VoteAverage: number;
  Provider: string;
  ExternalID: string;
}

export const SeriesSearchResultSchema = Schema.Struct({
  Title: Schema.String,
  Overview: Schema.String,
  PosterURL: Schema.String,
  BackdropURL: Schema.String,
  ReleaseDate: Schema.String,
  VoteAverage: Schema.Number,
  Provider: Schema.String,
  ExternalID: Schema.String,
});

export type DiscoverMediaType = "movie" | "tv";

export const DiscoverMediaTypeSchema = Schema.Literals(["movie", "tv"]);

export interface DiscoverLibraryMatch {
  library_id: number;
  library_name: string;
  library_type: LibraryType;
  kind: "movie" | "show";
  show_key?: string;
}

export const DiscoverLibraryMatchSchema = Schema.Struct({
  library_id: Schema.Number,
  library_name: Schema.String,
  library_type: LibraryTypeSchema,
  kind: Schema.Literals(["movie", "show"]),
  show_key: Schema.optional(Schema.String),
});

export interface DiscoverItem {
  media_type: DiscoverMediaType;
  tmdb_id: number;
  title: string;
  overview?: string;
  poster_path?: string;
  backdrop_path?: string;
  release_date?: string;
  first_air_date?: string;
  vote_average?: number;
  library_matches?: DiscoverLibraryMatch[];
}

export const DiscoverItemSchema = Schema.Struct({
  media_type: DiscoverMediaTypeSchema,
  tmdb_id: Schema.Number,
  title: Schema.String,
  overview: Schema.optional(Schema.String),
  poster_path: Schema.optional(Schema.String),
  backdrop_path: Schema.optional(Schema.String),
  release_date: Schema.optional(Schema.String),
  first_air_date: Schema.optional(Schema.String),
  vote_average: Schema.optional(Schema.Number),
  library_matches: Schema.optional(Schema.Array(DiscoverLibraryMatchSchema)),
});

export interface DiscoverShelf {
  id: string;
  title: string;
  items: DiscoverItem[];
}

export const DiscoverShelfSchema = Schema.Struct({
  id: Schema.String,
  title: Schema.String,
  items: Schema.Array(DiscoverItemSchema),
});

export interface DiscoverResponse {
  shelves: DiscoverShelf[];
}

export const DiscoverResponseSchema = Schema.Struct({
  shelves: Schema.Array(DiscoverShelfSchema),
});

export interface DiscoverSearchResponse {
  movies: DiscoverItem[];
  tv: DiscoverItem[];
}

export const DiscoverSearchResponseSchema = Schema.Struct({
  movies: Schema.Array(DiscoverItemSchema),
  tv: Schema.Array(DiscoverItemSchema),
});

export interface DiscoverTitleVideo {
  name: string;
  site: string;
  key: string;
  type: string;
  official?: boolean;
}

export const DiscoverTitleVideoSchema = Schema.Struct({
  name: Schema.String,
  site: Schema.String,
  key: Schema.String,
  type: Schema.String,
  official: Schema.optional(Schema.Boolean),
});

export interface DiscoverTitleDetails {
  media_type: DiscoverMediaType;
  tmdb_id: number;
  title: string;
  overview: string;
  poster_path?: string;
  backdrop_path?: string;
  release_date?: string;
  first_air_date?: string;
  vote_average?: number;
  imdb_id?: string;
  imdb_rating?: number;
  status?: string;
  genres: string[];
  runtime?: number;
  number_of_seasons?: number;
  number_of_episodes?: number;
  videos: DiscoverTitleVideo[];
  library_matches?: DiscoverLibraryMatch[];
}

export const DiscoverTitleDetailsSchema = Schema.Struct({
  media_type: DiscoverMediaTypeSchema,
  tmdb_id: Schema.Number,
  title: Schema.String,
  overview: Schema.String,
  poster_path: Schema.optional(Schema.String),
  backdrop_path: Schema.optional(Schema.String),
  release_date: Schema.optional(Schema.String),
  first_air_date: Schema.optional(Schema.String),
  vote_average: Schema.optional(Schema.Number),
  imdb_id: Schema.optional(Schema.String),
  imdb_rating: Schema.optional(Schema.Number),
  status: Schema.optional(Schema.String),
  genres: Schema.Array(Schema.String),
  runtime: Schema.optional(Schema.Number),
  number_of_seasons: Schema.optional(Schema.Number),
  number_of_episodes: Schema.optional(Schema.Number),
  videos: Schema.Array(DiscoverTitleVideoSchema),
  library_matches: Schema.optional(Schema.Array(DiscoverLibraryMatchSchema)),
});

export interface ShowActionResult {
  updated: number;
}

export const ShowActionResultSchema = Schema.Struct({
  updated: Schema.Number,
});

export interface ShowRefreshPayload {
  showKey: string;
}

export const ShowRefreshPayloadSchema = Schema.Struct({
  showKey: Schema.String,
});

export interface ShowConfirmPayload {
  showKey: string;
}

export const ShowConfirmPayloadSchema = Schema.Struct({
  showKey: Schema.String,
});

export interface ShowIdentifyPayload {
  showKey: string;
  tmdbId: number;
}

export const ShowIdentifyPayloadSchema = Schema.Struct({
  showKey: Schema.String,
  tmdbId: Schema.Number,
});

export type VaapiDecodeCodec =
  | "h264"
  | "hevc"
  | "mpeg2"
  | "vc1"
  | "vp8"
  | "vp9"
  | "av1"
  | "hevc10bit"
  | "vp910bit";

export const VaapiDecodeCodecSchema = Schema.Literals([
  "h264",
  "hevc",
  "mpeg2",
  "vc1",
  "vp8",
  "vp9",
  "av1",
  "hevc10bit",
  "vp910bit",
]);

export type HardwareEncodeFormat = "h264" | "hevc" | "av1";

export const HardwareEncodeFormatSchema = Schema.Literals(["h264", "hevc", "av1"]);

export interface TranscodingSettings {
  vaapiEnabled: boolean;
  vaapiDevicePath: string;
  decodeCodecs: Record<VaapiDecodeCodec, boolean>;
  hardwareEncodingEnabled: boolean;
  encodeFormats: Record<HardwareEncodeFormat, boolean>;
  preferredHardwareEncodeFormat: HardwareEncodeFormat;
  allowSoftwareFallback: boolean;
  crf: number;
  audioBitrate: string;
  audioChannels: number;
  threads: number;
  keyframeInterval: number;
  maxBitrate: string;
}

export const VaapiDecodeCodecFlagsSchema = Schema.Struct({
  h264: Schema.Boolean,
  hevc: Schema.Boolean,
  mpeg2: Schema.Boolean,
  vc1: Schema.Boolean,
  vp8: Schema.Boolean,
  vp9: Schema.Boolean,
  av1: Schema.Boolean,
  hevc10bit: Schema.Boolean,
  vp910bit: Schema.Boolean,
});

export const HardwareEncodeFormatFlagsSchema = Schema.Struct({
  h264: Schema.Boolean,
  hevc: Schema.Boolean,
  av1: Schema.Boolean,
});

export const TranscodingSettingsSchema = Schema.Struct({
  vaapiEnabled: Schema.Boolean,
  vaapiDevicePath: Schema.String,
  decodeCodecs: VaapiDecodeCodecFlagsSchema,
  hardwareEncodingEnabled: Schema.Boolean,
  encodeFormats: HardwareEncodeFormatFlagsSchema,
  preferredHardwareEncodeFormat: HardwareEncodeFormatSchema,
  allowSoftwareFallback: Schema.Boolean,
  crf: Schema.Number,
  audioBitrate: Schema.String,
  audioChannels: Schema.Number,
  threads: Schema.Number,
  keyframeInterval: Schema.Number,
  maxBitrate: Schema.String,
});

export interface TranscodingSettingsWarning {
  code: string;
  message: string;
}

export const TranscodingSettingsWarningSchema = Schema.Struct({
  code: Schema.String,
  message: Schema.String,
});

export interface TranscodingSettingsResponse {
  settings: TranscodingSettings;
  warnings: TranscodingSettingsWarning[];
}

export const TranscodingSettingsResponseSchema = Schema.Struct({
  settings: TranscodingSettingsSchema,
  warnings: Schema.Array(TranscodingSettingsWarningSchema),
});

export interface WelcomeEvent {
  type: "welcome";
  message: string;
}

export interface PongEvent {
  type: "pong";
}

export interface AttachPlaybackSessionCommand {
  action: "attach_playback_session";
  sessionId: string;
}

export interface DetachPlaybackSessionCommand {
  action: "detach_playback_session";
  sessionId: string;
}

export interface PlaybackSessionUpdateEvent {
  type: "playback_session_update";
  sessionId: string;
  delivery: "remux" | "transcode";
  mediaId: number;
  revision: number;
  audioIndex: number;
  status: PlaybackSessionStatus;
  streamUrl: string;
  error?: string;
}

export interface LibraryScanUpdateEvent {
  type: "library_scan_update";
  scan: LibraryScanStatus;
}

export type PlumWebSocketEvent =
  | WelcomeEvent
  | PongEvent
  | PlaybackSessionUpdateEvent
  | LibraryScanUpdateEvent;

export type PlumWebSocketCommand = AttachPlaybackSessionCommand | DetachPlaybackSessionCommand;

export const WelcomeEventSchema = Schema.Struct({
  type: Schema.Literal("welcome"),
  message: Schema.String,
});

export const PongEventSchema = Schema.Struct({
  type: Schema.Literal("pong"),
});

export const AttachPlaybackSessionCommandSchema = Schema.Struct({
  action: Schema.Literal("attach_playback_session"),
  sessionId: Schema.String,
});

export const DetachPlaybackSessionCommandSchema = Schema.Struct({
  action: Schema.Literal("detach_playback_session"),
  sessionId: Schema.String,
});

export const PlaybackSessionUpdateEventSchema = Schema.Struct({
  type: Schema.Literal("playback_session_update"),
  sessionId: Schema.String,
  delivery: Schema.Literals(["remux", "transcode"]),
  mediaId: Schema.Number,
  revision: Schema.Number,
  audioIndex: Schema.Number,
  status: PlaybackSessionStatusSchema,
  streamUrl: Schema.String,
  error: Schema.optional(Schema.String),
});

export const LibraryScanUpdateEventSchema = Schema.Struct({
  type: Schema.Literal("library_scan_update"),
  scan: LibraryScanStatusSchema,
});

export const PlumWebSocketEventSchema = Schema.Union([
  WelcomeEventSchema,
  PongEventSchema,
  PlaybackSessionUpdateEventSchema,
  LibraryScanUpdateEventSchema,
]);

export const PlumWebSocketCommandSchema = Schema.Union([
  AttachPlaybackSessionCommandSchema,
  DetachPlaybackSessionCommandSchema,
]);
