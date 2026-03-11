import type {
  CreateLibraryPayload,
  EmbeddedSubtitle,
  Library,
  LibraryType,
  MatchStatus,
  MediaItem,
  Subtitle,
} from '@plum/contracts'

export type { CreateLibraryPayload, Library, LibraryType, MatchStatus, MediaItem, Subtitle, EmbeddedSubtitle }

export interface User {
  id: number
  email: string
  is_admin: boolean
}

export const BASE_URL: string =
  (import.meta.env.VITE_BACKEND_URL as string | undefined) || ''

const defaultFetchOpts: RequestInit = { credentials: 'include' }

export async function getSetupStatus(): Promise<{ hasAdmin: boolean }> {
  const res = await fetch(`${BASE_URL}/api/setup/status`, defaultFetchOpts)
  if (!res.ok) throw new Error(`Setup status: ${res.status}`)
  return res.json()
}

export async function createAdmin(payload: { email: string; password: string }): Promise<User> {
  const res = await fetch(`${BASE_URL}/api/auth/admin-setup`, {
    ...defaultFetchOpts,
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(res.status === 409 ? 'Admin already exists.' : text || `Failed: ${res.status}`)
  }
  return res.json()
}

export async function login(payload: { email: string; password: string }): Promise<User> {
  const res = await fetch(`${BASE_URL}/api/auth/login`, {
    ...defaultFetchOpts,
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || 'Invalid credentials.')
  }
  return res.json()
}

export async function logout(): Promise<void> {
  await fetch(`${BASE_URL}/api/auth/logout`, { ...defaultFetchOpts, method: 'POST' })
}

export async function getMe(): Promise<User | null> {
  const res = await fetch(`${BASE_URL}/api/auth/me`, defaultFetchOpts)
  if (res.status === 401) return null
  if (!res.ok) throw new Error(`Me: ${res.status}`)
  return res.json()
}

export async function createLibrary(payload: CreateLibraryPayload): Promise<Library> {
  const res = await fetch(`${BASE_URL}/api/libraries`, {
    ...defaultFetchOpts,
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || `Create library: ${res.status}`)
  }
  return res.json()
}

export async function listLibraries(): Promise<Library[]> {
  const url = `${BASE_URL}/api/libraries`
  console.log('[api] listLibraries request', { url, baseUrl: BASE_URL })
  const res = await fetch(url, defaultFetchOpts)
  if (!res.ok) {
    const text = await res.text()
    console.error('[api] listLibraries error', { status: res.status, statusText: res.statusText, body: text })
    throw new Error(`Libraries: ${res.status}${text ? ` ${text}` : ''}`)
  }
  const data = await res.json()
  console.log('[api] listLibraries response', { count: Array.isArray(data) ? data.length : 0 })
  return data
}

export async function scanLibraryById(
  id: number,
  options?: { identify?: boolean },
): Promise<ScanLibraryResult> {
  const identify = options?.identify !== false
  const url = `${BASE_URL}/api/libraries/${id}/scan${identify ? '' : '?identify=false'}`
  const res = await fetch(url, {
    ...defaultFetchOpts,
    method: 'POST',
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || `Scan: ${res.status}`)
  }
  return res.json()
}

export interface ScanLibraryResult {
  added: number
  updated: number
  removed: number
  unmatched: number
  skipped: number
}

export interface IdentifyResult {
  identified: number
  failed: number
}

export async function identifyLibrary(id: number): Promise<IdentifyResult> {
  const res = await fetch(`${BASE_URL}/api/libraries/${id}/identify`, {
    ...defaultFetchOpts,
    method: 'POST',
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || `Identify: ${res.status}`)
  }
  return res.json()
}

export interface SeriesDetails {
  name: string
  overview: string
  poster_path: string
  backdrop_path: string
  first_air_date: string
}

export async function fetchSeriesByTmdbId(tmdbId: number): Promise<SeriesDetails | null> {
  const res = await fetch(`${BASE_URL}/api/series/${tmdbId}`, defaultFetchOpts)
  if (res.status === 404) return null
  if (!res.ok) throw new Error(`Series: ${res.status}`)
  return res.json()
}

export async function fetchLibraryMedia(id: number): Promise<MediaItem[]> {
  const url = `${BASE_URL}/api/libraries/${id}/media`
  console.log('[api] fetchLibraryMedia request', { libraryId: id, url })
  const res = await fetch(url, defaultFetchOpts)
  if (!res.ok) {
    const text = await res.text()
    console.error('[api] fetchLibraryMedia error', { libraryId: id, status: res.status, statusText: res.statusText, body: text })
    throw new Error(`Library media: ${res.status}${text ? ` ${text}` : ''}`)
  }
  const data = await res.json()
  console.log('[api] fetchLibraryMedia response', { libraryId: id, count: Array.isArray(data) ? data.length : 0 })
  return data
}

export async function fetchMediaList(): Promise<MediaItem[]> {
  const res = await fetch(`${BASE_URL}/api/media`, defaultFetchOpts)
  if (!res.ok) throw new Error(`Failed to fetch media: ${res.status}`)
  return res.json()
}

export async function startTranscode(id: number): Promise<void> {
  const res = await fetch(`${BASE_URL}/api/transcode/${id}`, {
    ...defaultFetchOpts,
    method: 'POST',
  })
  if (!res.ok) throw new Error(`Failed to start transcode: ${res.status}`)
}

/** TV search result from GET /api/series/search (TMDB match). Matches Go MatchResult JSON. */
export interface SeriesSearchResult {
  Title: string
  Overview: string
  PosterURL: string
  BackdropURL: string
  ReleaseDate: string
  VoteAverage: number
  Provider: string
  ExternalID: string
}

export async function searchSeries(query: string): Promise<SeriesSearchResult[]> {
  if (!query.trim()) return []
  const res = await fetch(
    `${BASE_URL}/api/series/search?q=${encodeURIComponent(query.trim())}`,
    defaultFetchOpts,
  )
  if (!res.ok) throw new Error(`Search: ${res.status}`)
  const data = await res.json()
  return Array.isArray(data) ? data : []
}

export interface ShowActionResult {
  updated: number
}

export async function refreshShow(
  libraryId: number,
  showKey: string,
): Promise<ShowActionResult> {
  const res = await fetch(`${BASE_URL}/api/libraries/${libraryId}/shows/refresh`, {
    ...defaultFetchOpts,
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ showKey }),
  })
  if (!res.ok) throw new Error(`Refresh: ${res.status}`)
  return res.json()
}

export async function identifyShow(
  libraryId: number,
  showKey: string,
  tmdbId: number,
): Promise<ShowActionResult> {
  const res = await fetch(`${BASE_URL}/api/libraries/${libraryId}/shows/identify`, {
    ...defaultFetchOpts,
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ showKey, tmdbId }),
  })
  if (!res.ok) throw new Error(`Identify: ${res.status}`)
  return res.json()
}
