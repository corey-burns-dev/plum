export function ensureBaseUrl(raw: string | undefined | null): string {
  if (!raw) return ''
  return raw.endsWith('/') ? raw.slice(0, -1) : raw
}

export function buildBackendUrl(base: string, path: string): string {
  const normalizedBase = ensureBaseUrl(base)
  if (!normalizedBase) return path
  if (!path.startsWith('/')) return `${normalizedBase}/${path}`
  return `${normalizedBase}${path}`
}

export function mediaStreamUrl(base: string, mediaId: number): string {
  return buildBackendUrl(base, `/api/stream/${mediaId}`)
}

export function externalSubtitleUrl(base: string, subtitleId: number): string {
  return buildBackendUrl(base, `/api/subtitles/${subtitleId}`)
}

export function embeddedSubtitleUrl(base: string, mediaId: number, streamIndex: number): string {
  return buildBackendUrl(base, `/api/media/${mediaId}/subtitles/embedded/${streamIndex}`)
}

