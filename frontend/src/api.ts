export interface MediaItem {
  id: number
  title: string
  path: string
  duration: number
  type: string
}

export const BASE_URL: string =
  (import.meta.env.VITE_BACKEND_URL as string | undefined) || ''

export async function fetchMediaList(): Promise<MediaItem[]> {
  const res = await fetch(`${BASE_URL}/api/media`)
  if (!res.ok) {
    throw new Error(`Failed to fetch media: ${res.status}`)
  }
  return res.json()
}

export async function startTranscode(id: number): Promise<void> {
  const res = await fetch(`${BASE_URL}/api/transcode/${id}`, {
    method: 'POST',
  })
  if (!res.ok) {
    throw new Error(`Failed to start transcode: ${res.status}`)
  }
}

export async function scanLibrary(path: string, type: string): Promise<{ added: number }> {
  const res = await fetch(`${BASE_URL}/api/scan`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ path, type }),
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(`Scan failed: ${res.status} ${text}`)
  }
  return res.json()
}

