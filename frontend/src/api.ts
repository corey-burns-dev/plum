export interface MediaItem {
  id: number
  title: string
  path: string
  duration: number
  type: string
}

const BASE_URL = import.meta.env.VITE_BACKEND_URL || ''

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

