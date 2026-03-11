import type { MediaItem } from "../api";

export type MusicAlbumGroup = {
  key: string;
  title: string;
  artist: string;
  year: number;
  trackCount: number;
  duration: number;
  posterPath: string | undefined;
  tracks: MediaItem[];
};

export type MusicArtistGroup = {
  key: string;
  name: string;
  albumCount: number;
  trackCount: number;
  posterPath: string | undefined;
  tracks: MediaItem[];
};

export function sortMusicTracks(tracks: MediaItem[]): MediaItem[] {
  return [...tracks].toSorted((a, b) => {
    const discA = a.disc_number ?? 0;
    const discB = b.disc_number ?? 0;
    if (discA !== discB) return discA - discB;
    const trackA = a.track_number ?? 0;
    const trackB = b.track_number ?? 0;
    if (trackA !== trackB) return trackA - trackB;
    return a.title.localeCompare(b.title);
  });
}

export function groupMusicByAlbum(items: MediaItem[]): MusicAlbumGroup[] {
  const groups = new Map<string, MediaItem[]>();
  for (const item of items) {
    const artist = item.album_artist || item.artist || "Unknown Artist";
    const album = item.album || "Unsorted Tracks";
    const key = `${artist}::${album}`;
    const group = groups.get(key) ?? [];
    group.push(item);
    groups.set(key, group);
  }

  return Array.from(groups.entries())
    .map(([key, tracks]) => {
      const sortedTracks = sortMusicTracks(tracks);
      const first = sortedTracks[0];
      return {
        key,
        title: first.album || "Unsorted Tracks",
        artist: first.album_artist || first.artist || "Unknown Artist",
        year: first.release_year ?? 0,
        trackCount: sortedTracks.length,
        duration: sortedTracks.reduce((sum, track) => sum + (track.duration || 0), 0),
        posterPath: first.poster_path,
        tracks: sortedTracks,
      };
    })
    .toSorted((a, b) => a.title.localeCompare(b.title));
}

export function groupMusicByArtist(items: MediaItem[]): MusicArtistGroup[] {
  const groups = new Map<string, MediaItem[]>();
  for (const item of items) {
    const artist = item.album_artist || item.artist || "Unknown Artist";
    const group = groups.get(artist) ?? [];
    group.push(item);
    groups.set(artist, group);
  }

  return Array.from(groups.entries())
    .map(([name, tracks]) => {
      const sortedTracks = sortMusicTracks(tracks);
      const albums = new Set(sortedTracks.map((track) => track.album || "Unsorted Tracks"));
      return {
        key: name,
        name,
        albumCount: albums.size,
        trackCount: sortedTracks.length,
        posterPath: sortedTracks[0]?.poster_path,
        tracks: sortedTracks,
      };
    })
    .toSorted((a, b) => a.name.localeCompare(b.name));
}

export function formatRuntime(totalSeconds: number): string {
  if (!totalSeconds) return "0:00";
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  return `${minutes}:${String(seconds).padStart(2, "0")}`;
}
