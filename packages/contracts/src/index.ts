/**
 * Library type determines identification/scan behavior and which category table is used.
 * TV and anime use TMDB for episodes; movie uses TMDB; music does not.
 */
export type LibraryType = 'tv' | 'movie' | 'music' | 'anime'

/**
 * Media item type stored per item; matches library type for identification.
 */
export type MediaType = 'tv' | 'movie' | 'music'

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
  type: MediaType | string
  subtitles?: Subtitle[]
  embeddedSubtitles?: EmbeddedSubtitle[]
  tmdb_id?: number
  overview?: string
  poster_path?: string
  backdrop_path?: string
  release_date?: string
  vote_average?: number
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
