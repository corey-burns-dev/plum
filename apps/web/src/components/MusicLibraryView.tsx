import { useMemo, type ReactNode } from "react";
import type { MediaItem } from "../api";
import {
  formatRuntime,
  groupMusicByAlbum,
  groupMusicByArtist,
  sortMusicTracks,
} from "../lib/musicGrouping";
import { LibraryPosterGrid } from "./LibraryPosterGrid";

interface Props {
  items: MediaItem[];
  onPlayCollection: (items: MediaItem[], startItem?: MediaItem) => void;
}

export function MusicLibraryView({ items, onPlayCollection }: Props) {
  const tracks = useMemo(() => sortMusicTracks(items), [items]);
  const albums = useMemo(() => groupMusicByAlbum(items), [items]);
  const artists = useMemo(() => groupMusicByArtist(items), [items]);

  return (
    <div className="music-library">
      <MusicSection
        title="Tracks"
        count={`${tracks.length} track${tracks.length === 1 ? "" : "s"}`}
      >
        <div className="music-track-list" role="list">
          {tracks.map((track) => (
            <button
              key={track.id}
              type="button"
              className="music-track-row"
              onClick={() => onPlayCollection(tracks, track)}
            >
              <span className="music-track-index">
                {(track.track_number ?? 0) > 0 ? String(track.track_number).padStart(2, "0") : "•"}
              </span>
              <span className="music-track-main">
                <span className="music-track-title">{track.title}</span>
                <span className="music-track-meta">
                  {track.artist || "Unknown Artist"}
                  {track.album ? ` • ${track.album}` : ""}
                </span>
              </span>
              <span className="music-track-duration">{formatRuntime(track.duration)}</span>
            </button>
          ))}
        </div>
      </MusicSection>

      <MusicSection
        title="Albums"
        count={`${albums.length} album${albums.length === 1 ? "" : "s"}`}
      >
        <LibraryPosterGrid
          compact
          items={albums.map((album) => ({
            key: album.key,
            title: album.title,
            subtitle: `${album.artist} • ${album.trackCount} tracks${album.year ? ` • ${album.year}` : ""}`,
            posterPath: album.posterPath,
            onClick: () => onPlayCollection(album.tracks, album.tracks[0]),
            onPlay: () => onPlayCollection(album.tracks, album.tracks[0]),
          }))}
        />
      </MusicSection>

      <MusicSection
        title="Artists"
        count={`${artists.length} artist${artists.length === 1 ? "" : "s"}`}
      >
        <LibraryPosterGrid
          compact
          items={artists.map((artist) => ({
            key: artist.key,
            title: artist.name,
            subtitle: `${artist.albumCount} albums • ${artist.trackCount} tracks`,
            posterPath: artist.posterPath,
            onClick: () => onPlayCollection(artist.tracks, artist.tracks[0]),
            onPlay: () => onPlayCollection(artist.tracks, artist.tracks[0]),
          }))}
        />
      </MusicSection>

      <div className="music-section-grid">
        <MusicPlaceholderSection
          title="Genres"
          description="Genres will appear here once Plum stores music genre metadata."
        />
        <MusicPlaceholderSection
          title="Playlists"
          description="Playlists are not persisted yet. This section is ready for future queue saving."
        />
      </div>
    </div>
  );
}

function MusicSection({
  title,
  count,
  children,
}: {
  title: string;
  count: string;
  children: ReactNode;
}) {
  return (
    <section className="music-section">
      <div className="music-section-header">
        <h2 className="music-section-title">{title}</h2>
        <span className="music-section-count">{count}</span>
      </div>
      {children}
    </section>
  );
}

function MusicPlaceholderSection({ title, description }: { title: string; description: string }) {
  return (
    <section className="music-placeholder">
      <div className="music-section-header">
        <h2 className="music-section-title">{title}</h2>
      </div>
      <p className="music-placeholder-copy">{description}</p>
    </section>
  );
}
