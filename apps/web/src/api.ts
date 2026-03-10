import type {
  CreateLibraryPayload,
  EmbeddedSubtitle,
  Library,
  LibraryType,
  MediaItem,
  Subtitle,
} from '@plum/contracts'

export type { CreateLibraryPayload, Library, LibraryType, MediaItem, Subtitle, EmbeddedSubtitle }

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

export async function scanLibraryById(id: number): Promise<{ added: number }> {
  const res = await fetch(`${BASE_URL}/api/libraries/${id}/scan`, {
    ...defaultFetchOpts,
    method: 'POST',
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || `Scan: ${res.status}`)
  }
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
