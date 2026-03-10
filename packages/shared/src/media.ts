export function tmdbPosterUrl(path: string | undefined, size: 'w200' | 'w500' | 'original' = 'w200'): string {
  if (!path) return ''
  return `https://image.tmdb.org/t/p/${size}${path}`
}

export function tmdbBackdropUrl(path: string | undefined, size: 'w300' | 'w500' | 'w780' | 'original' = 'w500'): string {
  if (!path) return ''
  return `https://image.tmdb.org/t/p/${size}${path}`
}

