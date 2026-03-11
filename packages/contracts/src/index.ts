/**
 * Library type determines identification/scan behavior and which category table is used.
 * TV and anime use TMDB for episodes; movie uses TMDB; music does not.
 */
export type LibraryType = 'tv' | 'movie' | 'music' | 'anime'

/**
 * Media item type stored per item; matches library type for identification.
 */
export type MediaType = 'tv' | 'movie' | 'music' | 'anime'

export type MatchStatus = 'identified' | 'local' | 'unmatched'

export interface Subtitle {
  id: number
  title: string
  language: string
  format: string
}

export interface EmbeddedSubtitle {
  streamIndex: number
  language: string
  title: string
}

export interface MediaItem {
  id: number
  library_id?: number
  title: string
  path: string
  duration: number
  type: MediaType
  match_status?: MatchStatus
  subtitles?: Subtitle[]
  embeddedSubtitles?: EmbeddedSubtitle[]
  tmdb_id?: number
  tvdb_id?: string
  overview?: string
  poster_path?: string
  backdrop_path?: string
  release_date?: string
  vote_average?: number
  artist?: string
  album?: string
  album_artist?: string
  disc_number?: number
  track_number?: number
  release_year?: number
  /** Set for TV/anime episodes; 0 when not applicable. */
  season?: number
  episode?: number
  /** Path to generated frame thumbnail (video episodes); served at /api/media/:id/thumbnail. */
  thumbnail_path?: string
}

export interface Library {
  id: number
  name: string
  type: LibraryType
  path: string
  user_id: number
}

export interface CreateLibraryPayload {
  name: string
  type: LibraryType
  path: string
}
