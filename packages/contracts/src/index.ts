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

export interface MediaItem {
  id: number;
  library_id?: number;
  title: string;
  path: string;
  duration: number;
  type: MediaType;
  match_status?: MatchStatus;
  subtitles?: Subtitle[];
  embeddedSubtitles?: EmbeddedSubtitle[];
  tmdb_id?: number;
  tvdb_id?: string;
  overview?: string;
  poster_path?: string;
  backdrop_path?: string;
  release_date?: string;
  vote_average?: number;
  artist?: string;
  album?: string;
  album_artist?: string;
  disc_number?: number;
  track_number?: number;
  release_year?: number;
  /** Set for TV/anime episodes; 0 when not applicable. */
  season?: number;
  episode?: number;
  /** Path to generated frame thumbnail (video episodes); served at /api/media/:id/thumbnail. */
  thumbnail_path?: string;
}

export const MediaItemSchema = Schema.Struct({
  id: Schema.Number,
  library_id: Schema.optional(Schema.Number),
  title: Schema.String,
  path: Schema.String,
  duration: Schema.Number,
  type: MediaTypeSchema,
  match_status: Schema.optional(MatchStatusSchema),
  subtitles: Schema.optional(Schema.Array(SubtitleSchema)),
  embeddedSubtitles: Schema.optional(Schema.Array(EmbeddedSubtitleSchema)),
  tmdb_id: Schema.optional(Schema.Number),
  tvdb_id: Schema.optional(Schema.String),
  overview: Schema.optional(Schema.String),
  poster_path: Schema.optional(Schema.String),
  backdrop_path: Schema.optional(Schema.String),
  release_date: Schema.optional(Schema.String),
  vote_average: Schema.optional(Schema.Number),
  artist: Schema.optional(Schema.String),
  album: Schema.optional(Schema.String),
  album_artist: Schema.optional(Schema.String),
  disc_number: Schema.optional(Schema.Number),
  track_number: Schema.optional(Schema.Number),
  release_year: Schema.optional(Schema.Number),
  season: Schema.optional(Schema.Number),
  episode: Schema.optional(Schema.Number),
  thumbnail_path: Schema.optional(Schema.String),
});

export interface Library {
  id: number;
  name: string;
  type: LibraryType;
  path: string;
  user_id: number;
}

export const LibrarySchema = Schema.Struct({
  id: Schema.Number,
  name: Schema.String,
  type: LibraryTypeSchema,
  path: Schema.String,
  user_id: Schema.Number,
});

export interface CreateLibraryPayload {
  name: string;
  type: LibraryType;
  path: string;
}

export const CreateLibraryPayloadSchema = Schema.Struct({
  name: Schema.String,
  type: LibraryTypeSchema,
  path: Schema.String,
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

export interface IdentifyResult {
  identified: number;
  failed: number;
}

export const IdentifyResultSchema = Schema.Struct({
  identified: Schema.Number,
  failed: Schema.Number,
});

export interface SeriesDetails {
  name: string;
  overview: string;
  poster_path: string;
  backdrop_path: string;
  first_air_date: string;
}

export const SeriesDetailsSchema = Schema.Struct({
  name: Schema.String,
  overview: Schema.String,
  poster_path: Schema.String,
  backdrop_path: Schema.String,
  first_air_date: Schema.String,
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

export interface TranscodeStartedEvent {
  type: "transcode_started";
  id: number;
  preferredMode: string;
}

export interface TranscodeWarningEvent {
  type: "transcode_warning";
  id: number;
  warning: string;
  error: string;
}

export interface TranscodeCompleteEvent {
  type: "transcode_complete";
  id: number;
  output: string;
  elapsed: number;
  mode: string;
  fallbackUsed: boolean;
  success: boolean;
  error: string;
}

export type PlumWebSocketEvent =
  | WelcomeEvent
  | PongEvent
  | TranscodeStartedEvent
  | TranscodeWarningEvent
  | TranscodeCompleteEvent;

export const WelcomeEventSchema = Schema.Struct({
  type: Schema.Literal("welcome"),
  message: Schema.String,
});

export const PongEventSchema = Schema.Struct({
  type: Schema.Literal("pong"),
});

export const TranscodeStartedEventSchema = Schema.Struct({
  type: Schema.Literal("transcode_started"),
  id: Schema.Number,
  preferredMode: Schema.String,
});

export const TranscodeWarningEventSchema = Schema.Struct({
  type: Schema.Literal("transcode_warning"),
  id: Schema.Number,
  warning: Schema.String,
  error: Schema.String,
});

export const TranscodeCompleteEventSchema = Schema.Struct({
  type: Schema.Literal("transcode_complete"),
  id: Schema.Number,
  output: Schema.String,
  elapsed: Schema.Number,
  mode: Schema.String,
  fallbackUsed: Schema.Boolean,
  success: Schema.Boolean,
  error: Schema.String,
});

export const PlumWebSocketEventSchema = Schema.Union([
  WelcomeEventSchema,
  PongEventSchema,
  TranscodeStartedEventSchema,
  TranscodeWarningEventSchema,
  TranscodeCompleteEventSchema,
]);
