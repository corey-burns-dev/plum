import type { Library } from '../api'
import type { MediaItem } from '../api'

/** Display label for library tab: Movies, TV, Anime, or Music based on type and name. */
export function getLibraryTabLabel(lib: Library): string {
  if (lib.type === 'movie') return 'Movies'
  if (lib.type === 'music') return 'Music'
  if (lib.type === 'anime' || (lib.type === 'tv' && /anime/i.test(lib.name))) return 'Anime'
  if (lib.type === 'tv') return 'TV'
  return lib.name
}

/** Extract display show name from episode title (e.g. "Show Name - S01E05 - Episode" -> "Show Name"). */
export function getShowName(title: string): string {
  const match = title.match(/^(.+?)\s*-\s*S\d+/i)
  return match ? match[1].trim() : title
}

/**
 * Stable key for grouping episodes into a show.
 * Uses tmdb_id when present so the same series is one show; otherwise falls back to show name from title.
 */
export function getShowKey(item: MediaItem): string {
  if (item.tmdb_id && (item.type === 'tv' || item.type === 'anime')) {
    return `tmdb-${item.tmdb_id}`
  }
  return getShowName(item.title)
}

export type ShowGroup = {
  showKey: string
  showTitle: string
  posterPath: string | undefined
  backdropPath: string | undefined
  unmatchedCount: number
  localCount: number
  episodes: MediaItem[]
}

/** Group TV/anime items by show key; each group has a stable key, title, first episode's poster, and sorted episodes. */
export function groupMediaByShow(items: MediaItem[]): ShowGroup[] {
  const map = new Map<string, MediaItem[]>()
  for (const m of items) {
    const key = getShowKey(m)
    const list = map.get(key) ?? []
    list.push(m)
    map.set(key, list)
  }
  const groups: ShowGroup[] = []
  for (const [showKey, episodes] of map.entries()) {
    sortEpisodes(episodes)
    const first = episodes[0]
    groups.push({
      showKey,
      showTitle: getShowName(first.title),
      posterPath: first.poster_path,
      backdropPath: first.backdrop_path,
      unmatchedCount: episodes.filter((episode) => episode.match_status === 'unmatched').length,
      localCount: episodes.filter((episode) => episode.match_status === 'local').length,
      episodes,
    })
  }
  return groups
}

/** Sort episodes by season then episode; fallback to title. */
export function sortEpisodes(episodes: MediaItem[]): void {
  episodes.sort((a, b) => {
    const sa = a.season ?? 0
    const sb = b.season ?? 0
    if (sa !== sb) return sa - sb
    const ea = a.episode ?? 0
    const eb = b.episode ?? 0
    if (ea !== eb) return ea - eb
    return (a.title ?? '').localeCompare(b.title ?? '')
  })
}
